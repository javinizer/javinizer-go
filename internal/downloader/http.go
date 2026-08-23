package downloader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
)

const imageAcceptHeader = "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8"

func (d *Downloader) download(ctx context.Context, url, destPath string, mediaType MediaType, options ...any) (finalResult *DownloadResult, finalErr error) {
	startTime := time.Now()
	overwriteExisting, dedup := resolveDownloadOptions(options)
	owner := resolveDownloadOwnerOptions(options)

	result := &DownloadResult{
		URL:        url,
		LocalPath:  destPath,
		Type:       mediaType,
		Downloaded: false,
	}

	var reservation *downloadReservation
	if overwriteExisting {
		var skipped bool
		var reservationErr error
		reservation, skipped, reservationErr = acquireDownloadReservation(ctx, dedup, destPath, owner.logicalKey, owner.ownerKey)
		if reservationErr != nil {
			result.Error = reservationErr
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
		if skipped {
			result.Skipped = true
			result.Duration = time.Since(startTime)
			return result, nil
		}
		defer func() {
			finishDownloadReservation(dedup, destPath, reservation, finalErr == nil)
		}()
	}

	if err := validateURLScheme(url); err != nil {
		result.Error = err
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	select {
	case <-ctx.Done():
		result.Error = ctx.Err()
		result.Duration = time.Since(startTime)
		return result, result.Error
	default:
	}

	// Existence is classified TWICE on overwrite: here, fail-fast before any
	// network fetch (stat wedges abort the download); then AGAIN inside the
	// install-time destination lock (the bounded section that must be racing
	// correct — two ops can never both classify "create" for one slot).
	if overwriteExisting {
		info, err := d.fs.Stat(destPath)
		switch {
		case err == nil:
			result.Size = info.Size()
		case os.IsNotExist(err):
		default:
			result.Error = fmt.Errorf("failed to stat destination: %w", err)
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
	} else if info, err := d.fs.Stat(destPath); err == nil {
		result.Size = info.Size()
		result.Duration = time.Since(startTime)
		return result, nil
	}

	destDir := filepath.Dir(destPath)
	if err := d.fs.MkdirAll(destDir, config.DirPerm); err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	if d.config.UserAgent != "" {
		req.Header.Set("User-Agent", d.config.UserAgent)
	}
	switch mediaType {
	case MediaTypeCover, MediaTypePoster, MediaTypeExtrafanart, MediaTypeActress:
		// Keep apply-time image representation negotiation aligned with the
		// review poster fetch. A content-negotiating host must return the same
		// bytes for the later fingerprint comparison to be meaningful.
		req.Header.Set("Accept", imageAcceptHeader)
	}
	if referer := resolveDownloadReferer(url); referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("failed to download: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}
	idleTimeout := time.Duration(d.config.DownloadTimeout) * time.Second
	var stallBody *StallReader
	if idleTimeout > 0 {
		stallBody = NewStallReader(resp.Body, idleTimeout, ctx)
		resp.Body = stallBody
	}
	defer func() {
		_ = httpclient.DrainAndClose(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		result.Error = &statusError{statusCode: resp.StatusCode}
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	tempPath, outFile, err := createDownloadTempFile(d.fs, destPath)
	if err != nil {
		result.Error = fmt.Errorf("failed to create file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	written, err := io.Copy(outFile, resp.Body)
	closeErr := outFile.Close()
	if err == nil && closeErr != nil {
		err = closeErr
	}

	if stallBody != nil && err == nil {
		stallBody.Disarm()
	}

	if err != nil {
		_ = d.fs.Remove(tempPath)
		result.Error = fmt.Errorf("failed to write file: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// A 200 with an empty body (transient CDN/proxy hiccup) yields (0, nil)
	// from io.Copy — without this guard, replaceFile would swap valid
	// artwork for a zero-byte file and report success. Fatal under
	// --overwrite-existing-media, so refuse before any replacement.
	if written == 0 {
		_ = d.fs.Remove(tempPath)
		result.Error = fmt.Errorf("%w: downloaded 0 bytes for %s", errDownloadEmpty, url)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Never swap good media for garbage: refuse before replaceFile when the
	// payload is provably wrong — a declared truncation (Content-Length) or
	// content provably not media (see validateDownloadedMedia: declared
	// text/JSON/XML types or a body that IS HTML/XML/JSON markup). Unknown
	// binary payloads pass through deliberately; this guard only fires on
	// positive evidence of corruption, never on uncertainty.
	// Only a DECLARED positive length can prove truncation; 0 also means
	// "unspecified" for close-delimited responses, and -1 is chunked.
	if resp.ContentLength > 0 && written != resp.ContentLength {
		_ = d.fs.Remove(tempPath)
		result.Error = fmt.Errorf("%w: downloaded %d of %d bytes for %s (truncated)", errDownloadTruncated, written, resp.ContentLength, url)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	validatedInfo, validatedHandle, err := validateDownloadedMedia(d.fs, tempPath, resp.Header.Get("Content-Type"), destPath)
	if err != nil {
		_ = d.fs.Remove(tempPath)
		result.Error = err
		result.Duration = time.Since(startTime)
		return result, result.Error
	}
	// Wave-45 (codex P2, PR#215 finding F1): freeze the VALIDATED object's
	// identity (from the open handle the sniffer just read) into the install
	// provenance snapshot — installOverwriting re-proves the staged name
	// against it before every publish, so a substitute planted between
	// validation and install is refused instead of landing at destPath.
	// Wave-48 (codex P2, PR#215 finding 6): the validated handle itself stays
	// OPEN and rides with the record — installOverwriting owns it end to end
	// (the bound publishes consume it; every other exit closes it), so the
	// publish never mutates the destination with a staging object merely
	// re-derived by PATH.
	provenance := stagedInstallProvenance{
		identity: installedIdentityFromFileInfo(validatedInfo),
		handle:   validatedHandle,
	}

	// P3: the byte install runs through the overwrite discipline — per-dest
	// lock, in-lock existence classification, ledger-armed skip+warn for
	// unrecorded replacements, backup-aside + restore-on-failure.
	// Wave-67 (codex P2, PR#215 — producer-side provenance binding): the
	// install hands back its own post-publish-VERIFIED destination identity
	// (the record it already proved at publish time — no extra filesystem
	// work), and the completed legs file it on the result as the producer
	// record. Wave-68 (codex P2, PR#215 F2): the completed-with-error
	// (wave-41) leg now files the verified identity too when the bound publish
	// handed one back (waves 61/62 — the ENOSYS-times-skipped leg); when the
	// identity is genuinely unavailable (virtual-fs posture, or
	// ErrPublishCompleted without a dest info binding) the leg refuses to
	// certify an unproven publish instead of filing an unknown record
	// (consumers keep the wave-53 fail-closed posture).
	ledger := resolveDownloadLedger(options)
	var installedID installedDestIdentity
	skipped, replaced, instErr := d.installOverwritingIdentity(ctx, tempPath, destPath, ledger, &installedID, provenance)
	if instErr != nil {
		// Wave-45 (codex P2, PR#215 finding F1): the staged name provably
		// stopped naming the validated download object — a directory writer
		// rotated a substitute onto it inside the validation→install window.
		// The substitute is FOREIGN bytes: never unlink it here (the create
		// path published nothing; the replace path already restored its
		// set-aside and retracted the journal through the publish-failure
		// compensation). Dest is untouched on the create path; the retained
		// staged name is warn-logged for manual cleanup.
		if errors.Is(instErr, errStagedInputSubstituted) {
			logging.Warnf("downloader: install of %s refused — staged name %s no longer names the validated download object (foreign substitution after validation); substitute preserved, destination untouched, manual cleanup advised", destPath, tempPath)
			result.Error = instErr
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
		// Wave-41 (codex P2, PR#215): an install error carrying
		// fsutil.ErrPublishCompleted proves the destination WAS published with
		// the staged bytes — the POSIX hard-link fallback's staged cleanup could
		// not re-prove tempPath (fsutil.ErrPublishNoReplaceStagedUnverified: it
		// may now address a FOREIGN occupant fsutil deliberately left
		// byte-intact) or its unlink failed with the destination rollback
		// failing too (wave-20). NEVER remove tempPath — unlinking there could
		// destroy foreign bytes; the retained staged name is warn-logged for
		// manual cleanup, matching copyBackupToDestPublish's wave-34 posture.
		// Wave-68 (codex P2, PR#215 F2): the completed-with-identity leg (waves
		// 61/62 — the ENOSYS-times-skipped publish hands back the post-publish-
		// verified destination identity) files THAT record on producerIdentity
		// and records exactly the success leg's accounting (dest enters
		// CreatedPaths through Downloaded && !Replaced, so a later revert
		// leaves the new media behind). If the identity is genuinely
		// unavailable (virtual-fs posture, or ErrPublishCompleted without a
		// dest info binding) the publish completed but its provenance CANNOT
		// be certified — a foreign temp replacement would then ride
		// publish-as-poster against an unknown record (downstream skips the
		// producer gates). Refuse instead of continuing, matching wave-53's
		// fail-closed posture: nothing certified, tempPath preserved
		// byte-intact (possibly foreign), the completed error surfaces.
		if fsutil.PublishCompleted(instErr) {
			if installedID.known {
				logging.Warnf("downloader: install of %s completed despite the returned error (%v) — staged name %s could not be re-proven (possibly foreign) and is left in place; manual cleanup advised", destPath, instErr, tempPath)
				result.producerIdentity = installedID
				result.Size = written
				result.Downloaded = true
				result.Replaced = replaced
				result.Duration = time.Since(startTime)
				return result, nil
			}
			logging.Warnf("downloader: install of %s completed despite the returned error (%v) but the published identity is unavailable (virtual-fs posture or no dest info binding) — refusing to certify an unproven publish; staged name %s left in place (possibly foreign), manual cleanup advised", destPath, instErr, tempPath)
			result.Error = instErr
			result.Duration = time.Since(startTime)
			return result, result.Error
		}
		// Codex P2 (PR#215 finding, wave-62): install failed WITHOUT any
		// publish — tempPath still names the validated object, or a foreign
		// substitute swapped in after validation while our handle was open.
		// Bind the cleanup to the identity snapshot exactly like the wave-59
		// skipped leg; the closed provenance handle's identity is immutable.
		if destStillHoldsInstalledObject(d.fs, tempPath, provenance.identity) {
			_ = d.fs.Remove(tempPath)
		} else if _, lerr := lstatBackupCandidate(d.fs, tempPath); !os.IsNotExist(lerr) {
			logging.Warnf("downloader: failed install of %s left staged name %s in place — it no longer provably names the validated download (foreign substitution or indeterminate); preserved byte-intact for manual cleanup", destPath, tempPath)
		}
		result.Error = instErr
		result.Duration = time.Since(startTime)
		return result, result.Error
	}
	if skipped {
		// Wave-59 (codex P2, PR#215 finding F2): bind the skipped-download
		// cleanup to the staged object — installOverwriting published nothing
		// on skip, so tempPath still holds the downloaded bytes OR a foreign
		// substitute swapped in after validation (the provenance handle is
		// already closed by installOverwriting's bound-publish ownership, so
		// the validation-time identity snapshot — the handle's never-mutable
		// fstat — binds the cleanup). Remove ONLY when tempPath still provably
		// names the validated object; a foreign occupant is preserved
		// byte-intact for manual cleanup, never destroyed by a pathname Remove.
		if destStillHoldsInstalledObject(d.fs, tempPath, provenance.identity) {
			_ = d.fs.Remove(tempPath)
		} else if _, lerr := lstatBackupCandidate(d.fs, tempPath); !os.IsNotExist(lerr) {
			logging.Warnf("downloader: skipped install of %s left staged name %s in place — it no longer provably names the validated download (foreign substitution or indeterminate); preserved byte-intact for manual cleanup", destPath, tempPath)
		}
		result.Skipped = true
		result.Downloaded = false
		result.LocalPath = destPath // the preserved existing artwork
		result.Duration = time.Since(startTime)
		return result, nil
	}

	result.Size = written
	result.Downloaded = true
	result.Replaced = replaced
	result.producerIdentity = installedID
	result.Duration = time.Since(startTime)

	return result, nil
}

// validateDownloadedMedia refuses to let obviously-not-media payloads reach
// replaceFile: a 200-OK HTML challenge page, a JSON error body, or an XML
// error document would otherwise atomically overwrite valid artwork. The
// guard is positive-evidence-only — HTML/XML/JSON are NEVER a valid media
// payload, while unknown binary bytes pass through untouched — so unusual
// but real image/video encodings and fixture bytes are never rejected by
// mistake.
//
// Wave-45 (codex P2, PR#215 finding F1): on acceptance the validated object's
// FileInfo is handed back, captured FROM THE OPEN HANDLE the sniffer read —
// the caller freezes it into the install provenance snapshot
// (installedIdentityFromFileInfo) so a substitute rotated onto tempPath
// between validation and install can never publish in the validated object's
// place. A failed identity capture fails the validation closed.
//
// Wave-48 (codex P2, PR#215 finding 6): on acceptance the sniffer's read
// handle itself is handed back OPEN alongside the identity record (returns
// info, handle, nil) — the identity-bearing object rides into install through
// the bound publish end to end (on filesystems where rename cannot span an
// open descriptor, fsutil's bound publish closes it at publish adjacency and
// re-proves the landed destination against the captured snapshot). Every
// refusal closes the handle and returns none.
func validateDownloadedMedia(fs afero.Fs, tempPath, contentType, destPath string) (os.FileInfo, afero.File, error) {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	// Any declared text/* type is provably not image/video payload —
	// "text/plain" prose like "rate limit exceeded" must not reach
	// replaceFile. Media arrives as image/*, video/*, octet-stream, or an
	// undeclared type (checked by content below).
	if strings.HasPrefix(ct, "text/") || strings.HasPrefix(ct, "application/json") ||
		strings.HasPrefix(ct, "application/xml") ||
		strings.HasSuffix(ct, "+xml") {
		return nil, nil, fmt.Errorf("downloaded %q instead of media for %s (likely an auth challenge or proxy error response)", ct, destPath)
	}

	f, err := fs.Open(tempPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read downloaded file: %w", err)
	}

	// Capture the validated object's identity THROUGH THE HANDLE before the
	// sniff read: on OsFs this is fstat — dev/inode, size, and mtime of
	// exactly the object being sniffed. InstallOverwriting's provenance gate
	// compares the staged name against this snapshot, never against a later
	// path re-lookup of the mutable temp name.
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("failed to stat downloaded file: %w", err)
	}

	head := make([]byte, 256)
	n, err := f.Read(head)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = f.Close()
		return nil, nil, fmt.Errorf("failed to read downloaded file: %w", err)
	}
	trimmed := strings.TrimSpace(strings.ToLower(string(head[:n])))
	if strings.HasPrefix(trimmed, "<!doctype") || strings.HasPrefix(trimmed, "<html") ||
		strings.HasPrefix(trimmed, "<head") || strings.HasPrefix(trimmed, "<?xml") ||
		strings.HasPrefix(trimmed, "<error") || strings.HasPrefix(trimmed, "<response") ||
		strings.HasPrefix(trimmed, "{") {
		_ = f.Close()
		return nil, nil, fmt.Errorf("downloaded an HTML/JSON document instead of media for %s (likely an auth challenge or proxy error)", destPath)
	}
	return info, f, nil
}

func resolveDownloadOptions(options []any) (bool, *sync.Map) {
	var overwriteExisting bool
	var dedup *sync.Map
	for _, option := range options {
		switch value := option.(type) {
		case bool:
			overwriteExisting = value
		case *sync.Map:
			dedup = value
		}
	}
	return overwriteExisting, dedup
}

// downloadOwnerOptions carries the apply phase's deterministic owner claim
// into the poster reservation. Empty values retain the legacy first-arrival
// reservation behavior for direct downloader callers.
type downloadOwnerOptions struct {
	logicalKey string
	ownerKey   string
}

func resolveDownloadOwnerOptions(options []any) downloadOwnerOptions {
	for _, option := range options {
		if owner, ok := option.(downloadOwnerOptions); ok {
			return owner
		}
	}
	return downloadOwnerOptions{}
}

const downloadOwnerClaimPrefix = "\x00poster-owner:"

// downloadOwnerClaim is primed before apply fan-out. The owner binds the
// concrete destination when it starts; workers for a different destination
// under the same logical movie key do not get incorrectly skipped.
type downloadOwnerClaim struct {
	logicalKey string
	ownerKey   string
	done       chan struct{}
	mu         sync.Mutex
	destPath   string
	success    bool
}

func ownerClaimKey(logicalKey string) string {
	return downloadOwnerClaimPrefix + strings.ToLower(strings.TrimSpace(logicalKey))
}

// PrimeDownloadOwners registers one deterministic owner per logical key. It
// is safe to call with an existing map and never overwrites a prior claim.
func PrimeDownloadOwners(dedup *sync.Map, owners map[string]string) {
	if dedup == nil {
		return
	}
	for logicalKey, ownerKey := range owners {
		logicalKey = strings.ToLower(strings.TrimSpace(logicalKey))
		ownerKey = strings.TrimSpace(ownerKey)
		if logicalKey == "" || ownerKey == "" {
			continue
		}
		dedup.LoadOrStore(ownerClaimKey(logicalKey), &downloadOwnerClaim{
			logicalKey: logicalKey,
			ownerKey:   ownerKey,
			done:       make(chan struct{}),
		})
	}
}

func (c *downloadOwnerClaim) bindDestination(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.destPath == "" {
		c.destPath = path
		return true
	}
	return c.destPath == path
}

func (c *downloadOwnerClaim) outcome() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.destPath, c.success
}

func (c *downloadOwnerClaim) complete(success bool) {
	c.mu.Lock()
	c.success = success
	c.mu.Unlock()
	close(c.done)
}

type downloadReservation struct {
	done    chan struct{}
	success bool
	claim   *downloadOwnerClaim
}

// acquireDownloadReservation accepts optional logicalKey, ownerKey arguments
// so the pre-registered owner gets the first reservation even when worker
// scheduling is arbitrary. Existing direct callers may omit them.
func acquireDownloadReservation(ctx context.Context, dedup *sync.Map, destPath string, ownerArgs ...string) (*downloadReservation, bool, error) {
	if dedup == nil {
		return nil, false, nil
	}
	logicalKey, ownerKey := "", ""
	if len(ownerArgs) > 0 {
		logicalKey = strings.ToLower(strings.TrimSpace(ownerArgs[0]))
	}
	if len(ownerArgs) > 1 {
		ownerKey = strings.TrimSpace(ownerArgs[1])
	}
	claimKey := ownerClaimKey(logicalKey)
	for {
		var claim *downloadOwnerClaim
		if logicalKey != "" && ownerKey != "" {
			if value, ok := dedup.Load(claimKey); ok {
				claim, _ = value.(*downloadOwnerClaim)
				if claim != nil && claim.ownerKey != ownerKey {
					select {
					case <-claim.done:
						dest, success := claim.outcome()
						if success && dest == destPath {
							return nil, true, nil
						}
						continue
					case <-ctx.Done():
						return nil, false, ctx.Err()
					}
				}
				if claim != nil && !claim.bindDestination(destPath) {
					claim = nil
				}
			}
		}
		value, loaded := dedup.LoadOrStore(destPath, &downloadReservation{done: make(chan struct{}), claim: claim})
		if !loaded {
			return value.(*downloadReservation), false, nil
		}
		reservation, ok := value.(*downloadReservation)
		if !ok {
			return nil, true, nil
		}
		select {
		case <-reservation.done:
			if reservation.success {
				return nil, true, nil
			}
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
}

func finishDownloadReservation(dedup *sync.Map, destPath string, reservation *downloadReservation, success bool) {
	if reservation == nil {
		return
	}
	reservation.success = success
	if !success {
		dedup.Delete(destPath)
	}
	if reservation.claim != nil {
		reservation.claim.complete(success)
		claimKey := ownerClaimKey(reservation.claim.logicalKey)
		if current, ok := dedup.Load(claimKey); ok && current == reservation.claim {
			dedup.Delete(claimKey)
		}
	}
	close(reservation.done)
}

// releaseDownloadOwnerClaim unblocks the next sibling when the primed owner
// has no poster work (for example, no source URL or overwrite disabled).
func releaseDownloadOwnerClaim(dedup *sync.Map, logicalKey, ownerKey string) {
	if dedup == nil || strings.TrimSpace(logicalKey) == "" || strings.TrimSpace(ownerKey) == "" {
		return
	}
	key := ownerClaimKey(logicalKey)
	value, ok := dedup.Load(key)
	claim, claimOK := value.(*downloadOwnerClaim)
	if !ok || !claimOK || claim.ownerKey != strings.TrimSpace(ownerKey) {
		return
	}
	claim.complete(false)
	if current, exists := dedup.Load(key); exists && current == claim {
		dedup.Delete(key)
	}
}

// ReleaseDownloadOwnerClaim releases a primed poster owner when the complete
// apply item exits before the poster reservation can do so (for example after
// a pre-apply skip, organize failure, or recovered panic).
func ReleaseDownloadOwnerClaim(dedup *sync.Map, logicalKey, ownerKey string) {
	releaseDownloadOwnerClaim(dedup, logicalKey, ownerKey)
}

func uniqueTempPath(destPath, suffix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return destPath + "." + hex.EncodeToString(buf) + "." + suffix
}

// downloadTempClaimTries bounds the exclusive temp-name claim loop; every
// collision (or racing claimant) costs one draw.
const downloadTempClaimTries = 8

// createDownloadTempFile claims the download's staging temp name with
// O_CREATE|O_EXCL|O_WRONLY, redrawing on collision (POSTER-WRITE-HARDENING
// wave-51): the pre-shape d.fs.Create opened the drawn name with O_TRUNC —
// anything sitting at the fresh name (a stale temp shard reused by another
// tool, a watcher pre-planting the namespace) was silently truncated before
// any bytes were even fetched. The claim loop never truncates an occupant:
// either the draw wins a provably-fresh name or the download fails and the
// occupant keeps its bytes byte-intact.
func createDownloadTempFile(fs afero.Fs, destPath string) (string, afero.File, error) {
	for attempt := 0; attempt < downloadTempClaimTries; attempt++ {
		tempPath := uniqueTempPath(destPath, "tmp")
		outFile, err := fs.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
		switch {
		case err == nil:
			return tempPath, outFile, nil
		case os.IsExist(err):
			continue // a racer (or a stale shard) owns this draw — draw again
		default:
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("download temp names exhausted for %s after %d attempts", destPath, downloadTempClaimTries)
}

// retryableOperation wraps an attempt function with retry logic for transient errors.
type retryableOperation struct {
	initialDelay time.Duration
	maxDelay     time.Duration
}

// ExecuteWithRetry runs attemptFn with exponential backoff for retryable errors.
// It retries on errors classified as retryable by isRetryableError, and fails
// immediately on non-retryable errors.
// Exponential backoff formula: delay = min(initialDelay * 2^(retryAttempt-1), maxDelay)
// Context cancellation is respected during backoff delays and attempts.
func (ro *retryableOperation) ExecuteWithRetry(ctx context.Context, attemptFn func() error, maxRetries int, url string) error {
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	totalAttempts := maxRetries + 1 // Initial attempt + retries

	for attempt := 0; attempt < totalAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := attemptFn(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if !isRetryableError(lastErr) {
			return fmt.Errorf("download failed after %d attempt(s): %s returned %w", attempt+1, url, lastErr)
		}

		if attempt == totalAttempts-1 {
			break
		}

		retryAttempt := attempt + 1
		delay := ro.initialDelay * time.Duration(1<<uint(retryAttempt-1))
		if delay > ro.maxDelay {
			delay = ro.maxDelay
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return fmt.Errorf("download failed after %d attempt(s): %s returned %w", totalAttempts, url, lastErr)
}

// DownloadWithRetry downloads a file with exponential backoff retry logic for transient errors
// It retries on HTTP 503, 500, 429 and network errors, but fails immediately on 404, 403, 401, 400
// Exponential backoff formula: delay = min(100ms * 2^(retryAttempt-1), 10s) where retryAttempt starts at 1
// Context cancellation is respected during backoff delays and HTTP requests
func (d *Downloader) DownloadWithRetry(ctx context.Context, url, destPath string, maxRetries int) error {
	op := &retryableOperation{
		initialDelay: 100 * time.Millisecond,
		maxDelay:     10 * time.Second,
	}

	return op.ExecuteWithRetry(ctx, func() error {
		_, err := d.download(ctx, url, destPath, "")
		return err
	}, maxRetries, url)
}

// statusError represents an HTTP status code error
type statusError struct {
	statusCode int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("HTTP %d", e.statusCode)
}

// isRetryableError determines if an error is retryable (503, 500, 429, network errors)
// Returns false for non-retryable errors (404, 403, 401, 400)
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var sErr *statusError
	if errors.As(err, &sErr) {
		switch sErr.statusCode {
		case http.StatusServiceUnavailable, // 503
			http.StatusInternalServerError, // 500
			http.StatusTooManyRequests:     // 429
			return true
		case http.StatusNotFound, // 404
			http.StatusForbidden,    // 403
			http.StatusUnauthorized, // 401
			http.StatusBadRequest:   // 400
			return false
		default:
			return false
		}
	}

	if errors.Is(err, errDownloadStalled) ||
		errors.Is(err, errDownloadTruncated) ||
		errors.Is(err, errDownloadEmpty) {
		return true
	}

	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// validateURLScheme checks if the URL uses http or https scheme
func validateURLScheme(urlStr string) error {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported URL scheme '%s': only http and https are allowed", scheme)
	}

	return nil
}

// ResolveMediaReferer selects a compatible Referer header for media requests.
// Delegates to httpclient.ResolveMediaReferer.
func resolveMediaReferer(downloadURL, configuredReferer string) string {
	return httpclient.ResolveMediaReferer(downloadURL, configuredReferer)
}

// resolveDownloadReferer selects a compatible Referer header for media downloads.
func resolveDownloadReferer(downloadURL string) string {
	return resolveMediaReferer(downloadURL, "")
}
