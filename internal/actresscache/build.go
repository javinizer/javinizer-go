package actresscache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Build ...
func Build(ctx context.Context, options BuildOptions) (Cache, BuildReport, error) {
	if options.Registry == nil {
		return Cache{}, BuildReport{}, fmt.Errorf("actress cache registry is required")
	}
	sources, err := normalizeSources(options.Sources)
	if err != nil {
		return Cache{}, BuildReport{}, err
	}
	for _, name := range sources {
		if _, ok := options.Registry.Create(name); !ok {
			return Cache{}, BuildReport{}, fmt.Errorf("unknown actress cache source %q (available: %s)", name, strings.Join(options.Registry.Names(), ", "))
		}
	}
	state, err := openState(options.StatePath)
	if err != nil {
		return Cache{}, BuildReport{}, fmt.Errorf("open actress cache state: %w", err)
	}
	defer func() { _ = state.close() }()

	// Effective validation policy for this run; reused state must satisfy it.
	minDimension := options.MinThumbnailDimension
	if minDimension <= 0 {
		minDimension = 64
	}
	maxBytes := options.MaxThumbnailBytes
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	validator := options.ValidateThumbnail
	if validator == nil {
		if options.SourceOptions.Fetcher == nil {
			return Cache{}, BuildReport{}, fmt.Errorf("actress cache fetcher is required")
		}
		validator = func(ctx context.Context, candidate Candidate) (ThumbnailValidation, error) {
			return ValidateThumbnail(ctx, options.SourceOptions.Fetcher, candidate.ThumbURL, minDimension, maxBytes)
		}
		validator = newThumbnailValidatorCache(validator).Validate
	}

	selected := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		selected[source] = struct{}{}
	}
	candidates := make(map[string]ValidatedCandidate)
	for key, entry := range state.entries {
		if entry.Status != "ok" || entry.Candidate == nil || entry.Thumbnail == nil || !cachedCandidateReusable(entry.Candidate, entry.Thumbnail, minDimension, maxBytes, options.AllowPrivateHosts) {
			continue
		}
		candidate := cloneCandidate(*entry.Candidate)
		candidate.Source = strings.ToLower(strings.TrimSpace(candidate.Source))
		if _, ok := selected[candidate.Source]; !ok {
			continue
		}
		candidates[key] = ValidatedCandidate{Candidate: candidate, Thumbnail: *entry.Thumbnail}
	}

	report := BuildReport{Sources: append([]string(nil), sources...)}
	// initialKeys remembers which candidates were reused from state so the
	// Cached metric survives: candidates validated during this run must not
	// inflate it; only pruned previously-reused entries decrement it. Under
	// --refresh nothing is reused (every entry is validated again), so the
	// metric starts at zero and no entry counts as previously reused.
	initialKeys := make(map[string]struct{}, len(candidates))
	if !options.Refresh {
		report.Cached = len(candidates)
		for key := range candidates {
			initialKeys[key] = struct{}{}
		}
	}
	// mu ...
	var mu sync.Mutex
	seenBySource := make(map[string]map[string]struct{}, len(sources))
	completedSources := make(map[string]bool, len(sources))
	recordSourceFailure := func(candidate Candidate, failure error) error {
		err := recordFailure(state, candidate, failure, &mu, &report)
		if err == nil && options.Refresh {
			mu.Lock()
			delete(candidates, candidate.Key)
			mu.Unlock()
		}
		return err
	}
	buildCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sourceErrors := make(chan error, len(sources))
	// sourceWG ...
	var sourceWG sync.WaitGroup
	pruningAllowed := options.SourceOptions.Limit <= 0
	for _, sourceName := range sources {
		sourceName := sourceName
		source, _ := options.Registry.Create(sourceName)
		sourceOptions := options.SourceOptions
		sourceOptions.ShouldSkip = func(key string) bool {
			if options.Refresh {
				return false
			}
			entry, ok := state.get(key)
			if !ok {
				return false
			}
			if entry.Status == "rejected" {
				return true
			}
			return entry.Status == "ok" && entry.Candidate != nil && entry.Thumbnail != nil && cachedCandidateReusable(entry.Candidate, entry.Thumbnail, minDimension, maxBytes, options.AllowPrivateHosts)
		}
		sourceOptions.RecordFailure = recordSourceFailure
		sourceOptions.MarkSeen = func(key string) {
			if strings.TrimSpace(key) == "" {
				return
			}
			mu.Lock()
			if seenBySource[sourceName] == nil {
				seenBySource[sourceName] = make(map[string]struct{})
			}
			seenBySource[sourceName][key] = struct{}{}
			mu.Unlock()
		}
		sourceOptions.MarkComplete = func() {
			if !pruningAllowed {
				return
			}
			mu.Lock()
			completedSources[sourceName] = true
			mu.Unlock()
		}
		emit := func(candidate Candidate) error {
			candidate.Source = strings.ToLower(strings.TrimSpace(candidate.Source))
			if candidate.Source == "" {
				candidate.Source = sourceName
			}
			if strings.TrimSpace(candidate.Key) == "" {
				return fmt.Errorf("source %q emitted a candidate without a key", sourceName)
			}
			mu.Lock()
			report.Candidates++
			mu.Unlock()
			if !hasStableIdentity(candidate) {
				return recordSourceFailure(candidate, fmt.Errorf("candidate has no stable identity"))
			}
			thumbnail, err := validator(buildCtx, candidate)
			if err != nil {
				return recordSourceFailure(candidate, err)
			}
			entry := StateEntry{
				Key:       candidate.Key,
				Status:    "ok",
				CheckedAt: time.Now().UTC().Format(time.RFC3339),
				Candidate: candidatePtr(candidate),
				Thumbnail: thumbnailPtr(thumbnail),
			}
			if err := state.append(entry); err != nil {
				return fmt.Errorf("write state for %s: %w", candidate.Key, err)
			}
			mu.Lock()
			candidates[candidate.Key] = ValidatedCandidate{Candidate: cloneCandidate(candidate), Thumbnail: thumbnail}
			report.Validated++
			mu.Unlock()
			return nil
		}
		sourceWG.Add(1)
		go func(sourceName string, source Source, sourceOptions SourceOptions, emit func(Candidate) error) {
			defer sourceWG.Done()
			if err := source.Collect(buildCtx, sourceOptions, emit); err != nil {
				sourceErrors <- fmt.Errorf("collect %s: %w", sourceName, err)
				cancel()
			}
		}(sourceName, source, sourceOptions, emit)
	}
	sourceWG.Wait()
	close(sourceErrors)
	// sourceErr ...
	var sourceErr error
	for err := range sourceErrors {
		if sourceErr == nil || (errors.Is(sourceErr, context.Canceled) && !errors.Is(err, context.Canceled)) {
			sourceErr = err
		}
	}
	if sourceErr != nil {
		return Cache{}, report, sourceErr
	}
	if options.Refresh && report.Failed > 0 {
		return Cache{}, report, fmt.Errorf("refresh encountered %d transient failures; refusing to publish incomplete cache", report.Failed)
	}
	for _, sourceName := range sources {
		mu.Lock()
		complete := completedSources[sourceName]
		seen := make(map[string]struct{}, len(seenBySource[sourceName]))
		for key := range seenBySource[sourceName] {
			seen[key] = struct{}{}
		}
		staleKeys := make([]string, 0)
		if complete {
			for key, candidate := range candidates {
				if candidate.Candidate.Source != sourceName {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				delete(candidates, key)
				staleKeys = append(staleKeys, key)
				if _, reused := initialKeys[key]; reused {
					report.Cached--
				}
			}
		}
		mu.Unlock()
		for _, key := range staleKeys {
			entry, ok := state.get(key)
			if !ok || entry.Status != "ok" {
				continue
			}
			entry.Status = "stale"
			entry.CheckedAt = time.Now().UTC().Format(time.RFC3339)
			entry.Error = "candidate was not present in source enumeration"
			if err := state.append(entry); err != nil {
				return Cache{}, report, fmt.Errorf("write stale state for %s: %w", key, err)
			}
		}
	}

	validated := make([]rankedCandidate, 0, len(candidates))
	ranks := make(map[string]int, len(sources))
	for rank, source := range sources {
		ranks[source] = rank
	}
	for _, candidate := range candidates {
		rank, ok := ranks[candidate.Candidate.Source]
		if !ok {
			continue
		}
		validated = append(validated, rankedCandidate{candidate: candidate, rank: rank})
	}
	cache := Cache{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Sources:       append([]string(nil), sources...),
		Records:       mergeCandidates(validated),
	}
	report.Records = len(cache.Records)
	return cache, report, nil
}

// normalizeSources ...
func normalizeSources(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("at least one actress cache source is required")
	}
	result := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("actress cache source %q was selected more than once", name)
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("at least one actress cache source is required")
	}
	return result, nil
}

// cachedCandidateReusable reports whether a cached thumbnail still satisfies
// the ACTIVE validation policy: dimensions and byte ceilings tightened
// between runs must force revalidation instead of republishing thumbnails
// that only passed under older settings. Zero-valued measurements mean the
// entry predates tracking and cannot prove compliance, so it revalidates.
func cachedCandidateReusable(candidate *Candidate, thumbnail *ThumbnailValidation, minDimension int, maxBytes int64, allowPrivateHosts bool) bool {
	if candidate == nil || strings.TrimSpace(candidate.ThumbURL) == "" {
		return false
	}
	if runtimeThumbnailURL(candidate.ThumbURL) == "" {
		return false
	}
	if !allowPrivateHosts {
		// A thumbnail fetched under --allow-private-hosts may carry a private
		// URL; a default-safe run must not reuse (and later embed) it.
		if u, err := url.Parse(strings.TrimSpace(candidate.ThumbURL)); err == nil && isBlockedFetchHost(u.Hostname()) {
			return false
		}
	}
	if thumbnail.Width < minDimension || thumbnail.Height < minDimension {
		return false
	}
	if thumbnail.Bytes <= 0 || int64(thumbnail.Bytes) > maxBytes {
		return false
	}
	return true
}

// recordFailure ...
func recordFailure(state *stateStore, candidate Candidate, failure error, mu *sync.Mutex, report *BuildReport) error {
	status := "failed"
	// rejected ...
	var rejected *ThumbnailRejectedError
	if errors.As(failure, &rejected) || strings.Contains(failure.Error(), "no stable identity") {
		status = "rejected"
	}
	entry := StateEntry{
		Key:       candidate.Key,
		Status:    status,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Candidate: candidatePtr(candidate),
		Error:     failure.Error(),
	}
	if err := state.append(entry); err != nil {
		return fmt.Errorf("write %s state for %s: %w", status, candidate.Key, err)
	}
	mu.Lock()
	if status == "rejected" {
		report.Rejected++
	} else {
		report.Failed++
	}
	mu.Unlock()
	return nil
}

// hasCJK ...
func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x3040 && r <= 0x30FF || r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// hasStableIdentity ...
func hasStableIdentity(candidate Candidate) bool {
	return candidate.DMMID > 0 || normalizeIdentity(candidate.JapaneseName) != "" || normalizeIdentity(candidate.FirstName+" "+candidate.LastName) != "" || strings.TrimSpace(candidate.SourceID) != ""
}

// cloneCandidate ...
func cloneCandidate(candidate Candidate) Candidate {
	candidate.Aliases = append([]string(nil), candidate.Aliases...)
	return candidate
}

// candidatePtr ...
func candidatePtr(candidate Candidate) *Candidate {
	candidate = cloneCandidate(candidate)
	return &candidate
}

// thumbnailPtr ...
func thumbnailPtr(thumbnail ThumbnailValidation) *ThumbnailValidation {
	return &thumbnail
}

// rankedCandidate ...
type rankedCandidate struct {
	candidate ValidatedCandidate
	rank      int
}

// candidateGroup ...
type candidateGroup struct {
	items      []rankedCandidate
	identities []string
	dmmIDs     map[int]struct{}
}

// mergeCandidates ...
func mergeCandidates(candidates []rankedCandidate) []Record {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		iDMM := candidates[i].candidate.Candidate.DMMID > 0
		jDMM := candidates[j].candidate.Candidate.DMMID > 0
		if iDMM != jDMM {
			return iDMM
		}
		return candidates[i].candidate.Candidate.Key < candidates[j].candidate.Candidate.Key
	})
	groups := make([]candidateGroup, 0)
	identityGroups := make(map[string]map[int]struct{})
	for _, candidate := range candidates {
		identities := candidateIdentities(candidate.candidate.Candidate)
		matches := compatibleGroups(groups, identityGroups, identities, candidate.candidate.Candidate)
		if len(matches) == 0 {
			groups = append(groups, newCandidateGroup(candidate, identities))
			registerGroup(identityGroups, len(groups)-1, identities)
			continue
		}
		base := matches[0]
		groups[base].items = append(groups[base].items, candidate)
		addGroupIdentities(&groups[base], identities)
		registerGroup(identityGroups, base, identities)
		if candidate.candidate.Candidate.DMMID > 0 {
			groups[base].dmmIDs[candidate.candidate.Candidate.DMMID] = struct{}{}
		}
		for _, group := range matches[1:] {
			mergeCandidateGroup(groups, identityGroups, base, group)
		}
	}
	records := make([]Record, 0, len(groups))
	for _, group := range groups {
		if len(group.items) == 0 {
			continue
		}
		sort.Slice(group.items, func(i, j int) bool {
			if group.items[i].rank != group.items[j].rank {
				return group.items[i].rank < group.items[j].rank
			}
			return group.items[i].candidate.Candidate.Key < group.items[j].candidate.Candidate.Key
		})
		records = append(records, recordFromGroup(group.items))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].BuiltinKey < records[j].BuiltinKey })
	return records
}

func mergeCandidateGroup(groups []candidateGroup, identityGroups map[string]map[int]struct{}, base, group int) {
	groups[base].items = append(groups[base].items, groups[group].items...)
	for _, identity := range groups[group].identities {
		addGroupIdentities(&groups[base], []string{identity})
		registerGroup(identityGroups, base, []string{identity})
	}
	for dmmID := range groups[group].dmmIDs {
		groups[base].dmmIDs[dmmID] = struct{}{}
	}
	groups[group] = candidateGroup{}
}

// newCandidateGroup ...
func newCandidateGroup(candidate rankedCandidate, identities []string) candidateGroup {
	group := candidateGroup{
		items:      []rankedCandidate{candidate},
		identities: append([]string(nil), identities...),
		dmmIDs:     make(map[int]struct{}),
	}
	if candidate.candidate.Candidate.DMMID > 0 {
		group.dmmIDs[candidate.candidate.Candidate.DMMID] = struct{}{}
	}
	return group
}

// compatibleGroups ...
func compatibleGroups(groups []candidateGroup, identityGroups map[string]map[int]struct{}, identities []string, candidate Candidate) []int {
	matched := make(map[int]struct{})
	for _, identity := range identities {
		for group := range identityGroups[identity] {
			if group < len(groups) && len(groups[group].items) > 0 && compatibleGroup(groups[group], candidate) {
				matched[group] = struct{}{}
			}
		}
	}
	if candidate.DMMID <= 0 && len(matched) > 1 {
		withoutDMM := make([]int, 0)
		for group := range matched {
			if len(groups[group].dmmIDs) == 0 {
				withoutDMM = append(withoutDMM, group)
			}
		}
		switch len(withoutDMM) {
		case 0:
			return nil
		case 1:
			matched = map[int]struct{}{withoutDMM[0]: {}}
		default:
			return nil
		}
	}
	if normalizeIdentity(candidate.JapaneseName) == "" && len(matched) > 1 {
		// A candidate with no Japanese name is not authoritative enough to
		// bridge multiple groups: when they carry conflicting Japanese
		// identities (distinct actresses sharing a romanized name), merging
		// would collapse them onto this one record.
		names := make(map[string]struct{})
		for group := range matched {
			for _, item := range groups[group].items {
				if name := normalizeIdentity(item.candidate.Candidate.JapaneseName); name != "" {
					names[name] = struct{}{}
				}
			}
		}
		if len(names) > 1 {
			return nil
		}
	}
	result := make([]int, 0, len(matched))
	for group := range matched {
		result = append(result, group)
	}
	sort.Ints(result)
	return result
}

// compatibleGroup ...
func compatibleGroup(group candidateGroup, candidate Candidate) bool {
	if candidate.DMMID > 0 {
		for dmmID := range group.dmmIDs {
			if dmmID != candidate.DMMID {
				return false
			}
		}
		if len(group.dmmIDs) > 0 {
			return true
		}
		// No DMM anchor in the group: fall through to the name-conflict checks
		// so a romanized-name-only match cannot collapse actresses with
		// conflicting Japanese names onto this DMM identity.
	}
	candidateJapaneseName := normalizeIdentity(candidate.JapaneseName)
	if candidateJapaneseName == "" {
		return true
	}
	if groupHasAlias(group, candidateJapaneseName) {
		return true
	}
	for _, item := range group.items {
		groupJapaneseName := normalizeIdentity(item.candidate.Candidate.JapaneseName)
		if groupJapaneseName != "" && groupJapaneseName != candidateJapaneseName {
			return false
		}
	}
	return true
}

// groupHasAlias ...
func groupHasAlias(group candidateGroup, name string) bool {
	for _, item := range group.items {
		for _, alias := range item.candidate.Candidate.Aliases {
			if normalizeIdentity(alias) == name {
				return true
			}
		}
	}
	return false
}

// addGroupIdentities ...
func addGroupIdentities(group *candidateGroup, identities []string) {
	for _, identity := range identities {
		group.identities = appendUnique(group.identities, identity)
	}
}

// registerGroup ...
func registerGroup(identityGroups map[string]map[int]struct{}, group int, identities []string) {
	for _, identity := range identities {
		groups := identityGroups[identity]
		if groups == nil {
			groups = make(map[int]struct{})
			identityGroups[identity] = groups
		}
		groups[group] = struct{}{}
	}
}

// candidateIdentities ...
func candidateIdentities(candidate Candidate) []string {
	identities := make([]string, 0, 8)
	if candidate.DMMID > 0 {
		identities = append(identities, fmt.Sprintf("dmm:%d", candidate.DMMID))
	}
	if normalized := normalizeIdentity(candidate.JapaneseName); normalized != "" {
		identities = append(identities, "jp:"+normalized)
	}
	for _, alias := range candidate.Aliases {
		if normalized := normalizeIdentity(alias); normalized != "" {
			identities = append(identities, "jp:"+normalized)
		}
	}
	if normalized := normalizeIdentity(candidate.FirstName + " " + candidate.LastName); normalized != "" {
		identities = append(identities, "name:"+normalized)
	}
	if strings.TrimSpace(candidate.SourceID) != "" && strings.TrimSpace(candidate.Source) != "" {
		identities = append(identities, "source:"+strings.ToLower(strings.TrimSpace(candidate.Source))+":"+normalizeIdentity(candidate.SourceID))
	}
	return identities
}

// normalizeIdentity ...
func normalizeIdentity(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(raw))), " ")
}

// appendUnique ...
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// recordFromGroup ...
func recordFromGroup(items []rankedCandidate) Record {
	record := Record{Sources: make([]SourceRecord, 0, len(items))}
	for _, item := range items {
		candidate := item.candidate.Candidate
		if record.DMMID == 0 && candidate.DMMID > 0 {
			record.DMMID = candidate.DMMID
		}
		if record.FirstName == "" {
			record.FirstName = candidate.FirstName
		}
		if record.LastName == "" {
			record.LastName = candidate.LastName
		}
		if record.JapaneseName == "" {
			record.JapaneseName = candidate.JapaneseName
		} else if !hasCJK(record.JapaneseName) && hasCJK(candidate.JapaneseName) {
			record.JapaneseName = candidate.JapaneseName
		}
		if record.ThumbURL == "" && candidate.ThumbURL != "" {
			record.ThumbURL = candidate.ThumbURL
			record.Thumbnail = item.candidate.Thumbnail
		}
		if record.PrimarySource == "" {
			record.PrimarySource = candidate.Source
		}
		record.Sources = append(record.Sources, SourceRecord{
			Source:       candidate.Source,
			SourceID:     candidate.SourceID,
			SourceURL:    candidate.SourceURL,
			DMMID:        candidate.DMMID,
			FirstName:    candidate.FirstName,
			LastName:     candidate.LastName,
			JapaneseName: candidate.JapaneseName,
			Aliases:      append([]string(nil), candidate.Aliases...),
			ThumbURL:     candidate.ThumbURL,
			Thumbnail:    item.candidate.Thumbnail,
		})
	}
	aliases := make([]string, 0)
	for _, item := range items {
		candidate := item.candidate.Candidate
		if candidate.JapaneseName != "" && normalizeIdentity(candidate.JapaneseName) != normalizeIdentity(record.JapaneseName) {
			aliases = appendUnique(aliases, strings.TrimSpace(candidate.JapaneseName))
		}
		for _, alias := range candidate.Aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" || normalizeIdentity(alias) == normalizeIdentity(record.JapaneseName) {
				continue
			}
			aliases = appendUnique(aliases, alias)
		}
	}
	record.Aliases = aliases
	record.BuiltinKey = builtinKey(record, items[0].candidate.Candidate)
	return record
}

// builtinKey ...
func builtinKey(record Record, fallback Candidate) string {
	if record.DMMID > 0 {
		return fmt.Sprintf("actress:dmm:%d", record.DMMID)
	}
	if name := normalizeIdentity(record.JapaneseName); name != "" {
		return "actress:jp:" + name
	}
	if name := normalizeIdentity(record.FirstName + " " + record.LastName); name != "" {
		return "actress:name:" + name
	}
	return "actress:" + strings.ToLower(strings.TrimSpace(fallback.Source)) + ":" + normalizeIdentity(fallback.SourceID)
}

type cacheTempFile interface {
	io.Writer
	Name() string
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

var createCacheTemp = func(dir, pattern string) (cacheTempFile, error) {
	return os.CreateTemp(dir, pattern)
}

// WriteFile ...
func WriteFile(path string, cache Cache) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("actress cache output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := createCacheTemp(filepath.Dir(path), ".actress-cache-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cache); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	// fsync before rename: a crash between them must not leave a zeroed cache.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
