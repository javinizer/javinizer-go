package workflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/spf13/afero"
)

const (
	videoCollisionKindFile     = "file"
	videoCollisionKindSameFile = "same-file"
	videoCollisionStaleCode    = "VIDEO_COLLISION_STALE"
)

type videoReviewFile struct {
	Device   uint64
	Inode    uint64
	Strong   bool
	Size     int64
	Modified int64
	Digest   string
}

type videoReviewReceipt struct {
	Scope           string
	Source          string
	Destination     string
	SourceFile      videoReviewFile
	DestinationFile videoReviewFile
}

func videoReviewScope(scope string, generation int64, applyGeneration uint64, plan *organizer.OrganizePlan) string {
	contentID := ""
	if plan.Movie != nil {
		contentID = plan.Movie.ContentID
	}
	return strings.Join([]string{"video-collision-v2", scope, contentID, strconv.FormatInt(generation, 10), strconv.FormatUint(applyGeneration, 10), plan.FolderName, plan.BaseFileName, plan.SubfolderPath, plan.ReviewNamingSignature()}, "\x00")
}

func lstatReviewedVideo(fs afero.Fs, path string) (os.FileInfo, error) {
	lst, ok := fs.(afero.Lstater)
	if !ok {
		return nil, fmt.Errorf("video review requires no-follow filesystem inspection")
	}
	info, noFollow, err := lst.LstatIfPossible(path)
	if err == nil && !noFollow {
		return nil, fmt.Errorf("video review cannot prove a no-follow file identity")
	}
	return info, err
}

func captureVideoReviewFile(ctx context.Context, fs afero.Fs, path string) (videoReviewFile, error) {
	info, err := lstatReviewedVideo(fs, path)
	if err != nil {
		return videoReviewFile{}, err
	}
	if !info.Mode().IsRegular() {
		return videoReviewFile{}, fmt.Errorf("video review requires a regular file: %s", path)
	}
	dev, ino, strong := fsutil.BoundObjectIdentity(fs, path, info)
	result := videoReviewFile{Device: dev, Inode: ino, Strong: strong, Size: info.Size(), Modified: info.ModTime().UnixNano()}
	f, err := fs.Open(path)
	if err != nil {
		return result, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return result, err
	}
	if !captureArtifactSourceIdentity(fs, path, info).matches(fs, path, opened) {
		return result, errArtifactSourceChanged
	}
	hash := sha256.New()
	buffer := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, readErr
		}
	}
	current, err := lstatReviewedVideo(fs, path)
	if err != nil {
		return result, err
	}
	if !captureArtifactSourceIdentity(fs, path, info).matches(fs, path, current) {
		return result, errArtifactSourceChanged
	}
	result.Digest = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}

func signVideoReview(receipt videoReviewReceipt) (string, error) {
	key, err := artifactSweepSecretKey()
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(append([]byte("video-review-v1\x00"), body...))
	return base64.RawURLEncoding.EncodeToString(body) + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func decodeVideoReview(token string) (videoReviewReceipt, error) {
	var receipt videoReviewReceipt
	if len(token) > 32768 {
		return receipt, fmt.Errorf("invalid video review token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return receipt, fmt.Errorf("video review token is required")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return receipt, err
	}
	if err := json.Unmarshal(body, &receipt); err != nil {
		return receipt, err
	}
	canonical, err := signVideoReview(receipt)
	if err != nil {
		return receipt, err
	}
	if !hmac.Equal([]byte(canonical), []byte(token)) {
		return receipt, fmt.Errorf("invalid video review signature")
	}
	return receipt, nil
}

func inspectVideoCollision(ctx context.Context, fs afero.Fs, source, destination, scope string) (*videocollision.Collision, error) {
	info, err := lstatArtifactSource(fs, destination)
	if os.IsNotExist(err) {
		return nil, nil
	}
	collision := &videocollision.Collision{Source: source, Destination: destination, Kind: "unknown"}
	if err != nil {
		return collision, err
	}
	collision.DestinationSize = info.Size()
	if sourceInfo, err := lstatArtifactSource(fs, source); err == nil {
		collision.SourceSize = sourceInfo.Size()
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		collision.Kind = "symlink"
	case info.IsDir():
		collision.Kind = "directory"
	case info.Mode().IsRegular():
		if filepath.Clean(source) == filepath.Clean(destination) || artifactSameRegularFile(fs, source, destination) {
			return nil, nil
		}
		collision.Kind = videoCollisionKindFile
		sourceInfo, sourceErr := lstatReviewedVideo(fs, source)
		destinationInfo, destinationErr := lstatReviewedVideo(fs, destination)
		if sourceErr != nil || destinationErr != nil {
			collision.Kind = "unknown"
			break
		}
		_, _, sourceStrong := fsutil.BoundObjectIdentity(fs, source, sourceInfo)
		_, _, destinationStrong := fsutil.BoundObjectIdentity(fs, destination, destinationInfo)
		if !sourceStrong || !destinationStrong {
			return collision, nil
		}
		src, err := captureVideoReviewFile(ctx, fs, source)
		if err != nil {
			return collision, err
		}
		dst, err := captureVideoReviewFile(ctx, fs, destination)
		if err != nil {
			return collision, err
		}
		if !src.Strong || !dst.Strong {
			return collision, nil
		}
		collision.ReviewToken, err = signVideoReview(videoReviewReceipt{Scope: scope, Source: filepath.Clean(source), Destination: filepath.Clean(destination), SourceFile: src, DestinationFile: dst})
		if err != nil {
			return collision, err
		}
	}
	return collision, nil
}

func verifyVideoReview(ctx context.Context, fs afero.Fs, decision videocollision.Decision, source, destination, scope, currentDestination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stale := &videocollision.ConflictError{Code: videoCollisionStaleCode, Collision: videocollision.Collision{Source: source, Destination: destination, Kind: videoCollisionKindFile}}
	receipt, err := decodeVideoReview(decision.ReviewToken)
	if err != nil || receipt.Scope != scope || receipt.Source != filepath.Clean(source) || receipt.Destination != filepath.Clean(destination) || filepath.Clean(decision.Source) != receipt.Source || filepath.Clean(decision.Destination) != receipt.Destination {
		return stale
	}
	src, err := captureVideoReviewFile(ctx, fs, source)
	if err != nil || src != receipt.SourceFile {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return stale
	}
	dst, err := captureVideoReviewFile(ctx, fs, currentDestination)
	if err != nil || dst != receipt.DestinationFile {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return stale
	}
	if !src.Strong || !dst.Strong {
		return fmt.Errorf("video replacement requires provable filesystem identities")
	}
	return nil
}

func (o *applyOrchImpl) preflightVideoCollision(ctx context.Context, cmd *ApplyCmd) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	policy, err := videocollision.Normalize(cmd.Organize.VideoCollisionPolicy)
	if err != nil {
		return false, err
	}
	if cmd.Organize.Skip || cmd.DryRun || (cmd.Organize.ForceUpdate && cmd.Organize.VideoCollisionPolicy == "" && len(cmd.Organize.VideoCollisionDecisions) == 0) {
		return false, nil
	}
	executor, ok := o.organizer.(artifactPlanExecutor)
	if !ok {
		if policy != videocollision.Fail || len(cmd.Organize.VideoCollisionDecisions) > 0 {
			return false, fmt.Errorf("video collision review requires planned organizer execution")
		}
		return false, nil
	}
	plan, err := executor.PlanOrganize(ctx, organizer.OrganizeCmd{Match: cmd.Match, Movie: cmd.Movie, DestDir: cmd.DestPath, MoveFiles: cmd.Organize.MoveFiles, LinkMode: cmd.Organize.LinkMode, OperationMode: cmd.OperationMode, ForceRenameFile: cmd.Organize.ForceRenameFile})
	if err != nil {
		return false, err
	}
	scope := videoReviewScope(cmd.Organize.VideoCollisionScope, cmd.Movie.RenderGeneration, cmd.Organize.VideoCollisionGeneration, plan)
	cmd.Organize.videoReviewScope, cmd.Organize.videoReviewTarget = scope, plan.TargetPath
	decision, explicit := cmd.Organize.VideoCollisionDecisions[filepath.Clean(plan.SourcePath)]
	verifiedReplacement := false
	if explicit {
		if filepath.Clean(decision.Source) != filepath.Clean(plan.SourcePath) || filepath.Clean(decision.Destination) != filepath.Clean(plan.TargetPath) {
			return false, &videocollision.ConflictError{Code: videoCollisionStaleCode, Collision: videocollision.Collision{Source: plan.SourcePath, Destination: plan.TargetPath, Kind: "stale"}}
		}
		policy, err = videocollision.Normalize(decision.Policy)
		if err != nil {
			return false, err
		}
		if policy == videocollision.Skip && decision.ReviewToken != "" {
			if err := verifyVideoReview(ctx, o.fs, decision, plan.SourcePath, plan.TargetPath, scope, plan.TargetPath); err != nil {
				return false, err
			}
		}
		if policy == videocollision.Skip && filepath.Clean(decision.Source) == filepath.Clean(plan.SourcePath) && filepath.Clean(decision.Destination) == filepath.Clean(plan.TargetPath) {
			cmd.Organize.DuplicateTracker.ReleaseClaim(plan.SourcePath, plan.TargetPath)
			return true, nil
		}
		if policy == videocollision.Replace {
			if err := verifyVideoReview(ctx, o.fs, decision, plan.SourcePath, plan.TargetPath, scope, plan.TargetPath); err != nil {
				return false, err
			}
			verifiedReplacement = true
		}
	}
	if collision := videoContainerCollision(o.fs, plan, plan.SourcePath, plan.TargetPath); collision != nil {
		if policy == videocollision.Skip {
			return true, nil
		}
		return false, &videocollision.ConflictError{Code: "VIDEO_COLLISION_UNRESOLVED", Collision: *collision}
	}
	var collision *videocollision.Collision
	if verifiedReplacement {
		collision = &videocollision.Collision{Source: plan.SourcePath, Destination: plan.TargetPath, Kind: videoCollisionKindFile}
	} else {
		collision, err = inspectVideoCollision(ctx, o.fs, plan.SourcePath, plan.TargetPath, scope)
		if err != nil {
			return false, err
		}
	}
	if policy == videocollision.Skip && collision != nil {
		cmd.Organize.DuplicateTracker.ReleaseClaim(plan.SourcePath, plan.TargetPath)
		return true, nil
	}
	if siblings, err := inspectMultipartVideoCollisions(ctx, o.fs, plan.SourcePath, plan.TargetPath, scope, cmd.Organize.VideoCollisionSelectedSources); err != nil {
		return false, err
	} else {
		for _, sibling := range siblings {
			if sibling.Kind != "new" && sibling.Kind != videoCollisionKindSameFile {
				if policy == videocollision.Skip {
					return true, nil
				}
				return false, &videocollision.ConflictError{Code: "VIDEO_COLLISION_UNRESOLVED", Collision: sibling}
			}
		}
	}
	if collision == nil {
		return false, nil
	}
	if policy == videocollision.Skip {
		cmd.Organize.DuplicateTracker.ReleaseClaim(plan.SourcePath, plan.TargetPath)
		return true, nil
	}
	if collision.Kind != videoCollisionKindFile || policy != videocollision.Replace || !explicit {
		return false, &videocollision.ConflictError{Code: "VIDEO_COLLISION_UNRESOLVED", Collision: *collision}
	}
	log, durable := o.revertLog.(*dbRevertLog)
	if artifactFencer(*cmd) == nil || strings.TrimSpace(cmd.Movie.ContentID) == "" || !durable || log.repo == nil || log.cfg == nil {
		return false, fmt.Errorf("video replacement requires durable staged recovery; enable operation history")
	}
	return false, nil
}

func videoContainerCollision(fs afero.Fs, plan *organizer.OrganizePlan, source, target string) *videocollision.Collision {
	for _, conflict := range plan.Conflicts {
		if filepath.Clean(conflict.Path) == filepath.Clean(target) || conflict.Kind == organizer.ConflictFile || conflict.Kind == organizer.ConflictDuplicate {
			continue
		}
		kind := "unknown"
		switch conflict.Kind {
		case organizer.ConflictDirectory:
			kind = "directory"
		case organizer.ConflictSymlink:
			kind = "symlink"
		}
		collision := &videocollision.Collision{Source: source, Destination: conflict.Path, Kind: kind, SelectedSource: source}
		if info, err := lstatArtifactSource(fs, source); err == nil {
			collision.SourceSize = info.Size()
		}
		if info, err := lstatArtifactSource(fs, conflict.Path); err == nil {
			collision.DestinationSize = info.Size()
		}
		return collision
	}
	return nil
}

func inspectMultipartVideoCollisions(ctx context.Context, fs afero.Fs, source, target, scope string, selected map[string]bool) ([]videocollision.Collision, error) {
	entries, err := afero.ReadDir(fs, filepath.Dir(source))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var collisions []videocollision.Collision
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == filepath.Base(source) || !isStagedArtifactVideoSibling(filepath.Base(source), entry.Name()) {
			continue
		}
		sibling := filepath.Join(filepath.Dir(source), entry.Name())
		if selected[filepath.Clean(sibling)] {
			continue
		}
		sourceInfo, sourceErr := lstatArtifactSource(fs, sibling)
		if sourceErr != nil {
			return nil, sourceErr
		}
		if !sourceInfo.Mode().IsRegular() {
			continue
		}
		destination := filepath.Join(filepath.Dir(target), stagedArtifactSiblingName(filepath.Base(source), filepath.Base(target), entry.Name()))
		collision, err := inspectVideoCollision(ctx, fs, sibling, destination, scope)
		if err != nil {
			return nil, err
		}
		if collision != nil {
			collision.Kind = "multipart"
			collision.ReviewToken = ""
		} else {
			collision = &videocollision.Collision{Source: sibling, Destination: destination, SourceSize: sourceInfo.Size(), Kind: "new"}
			if info, err := lstatArtifactSource(fs, destination); err == nil {
				collision.Kind = videoCollisionKindSameFile
				collision.DestinationSize = info.Size()
			}
		}
		collisions = append(collisions, *collision)
	}
	return collisions, nil
}

func (s *artifactStage) reviewedPrimaryTarget(target string) bool {
	decision, ok := s.original.Organize.VideoCollisionDecisions[filepath.Clean(s.sourcePath)]
	return ok && decision.Policy == videocollision.Replace && filepath.Clean(decision.Destination) == filepath.Clean(target) && filepath.Clean(s.original.Organize.videoReviewTarget) == filepath.Clean(target)
}

func (s *artifactStage) reviewedResidentError(target string, primary bool) error {
	decision, ok := s.original.Organize.VideoCollisionDecisions[filepath.Clean(s.sourcePath)]
	if primary && ok && decision.Policy == videocollision.Replace {
		return &videocollision.ConflictError{Code: videoCollisionStaleCode, Collision: videocollision.Collision{Source: s.sourcePath, Destination: target, Kind: videoCollisionKindSameFile}}
	}
	return nil
}

func (s *artifactStage) verifyReviewedPayload(path string, info os.FileInfo) error {
	decision, ok := s.original.Organize.VideoCollisionDecisions[filepath.Clean(s.sourcePath)]
	if !ok || decision.Policy != videocollision.Replace {
		return nil
	}
	stale := &videocollision.ConflictError{Code: videoCollisionStaleCode, Collision: videocollision.Collision{Source: s.sourcePath, Destination: decision.Destination, Kind: "source-changed"}}
	receipt, err := decodeVideoReview(decision.ReviewToken)
	if err != nil || receipt.Source != filepath.Clean(s.sourcePath) || receipt.Scope != s.original.Organize.videoReviewScope {
		return stale
	}
	ctx := s.publishCtx
	if ctx == nil {
		ctx = context.Background()
	}
	current, err := captureVideoReviewFile(ctx, s.fs, path)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return stale
	}
	device, inode, strong := fsutil.BoundObjectIdentity(s.fs, path, info)
	if !strong || !current.Strong || current.Device != device || current.Inode != inode || current.Size != receipt.SourceFile.Size || current.Digest != receipt.SourceFile.Digest {
		return stale
	}
	return nil
}

func (s *artifactStage) refreshedVideoCollision(ctx context.Context, publicationErr error) error {
	if ctx.Err() != nil || s.original.Organize.videoReviewTarget == "" {
		return publicationErr
	}
	var conflict *videocollision.ConflictError
	if errors.As(publicationErr, &conflict) {
		return publicationErr
	}
	target := s.original.Organize.videoReviewTarget
	decision, explicit := s.original.Organize.VideoCollisionDecisions[filepath.Clean(s.sourcePath)]
	collision, err := inspectVideoCollision(ctx, s.fs, s.sourcePath, target, s.original.Organize.videoReviewScope)
	if err != nil {
		if explicit && decision.Policy == videocollision.Replace {
			return errors.Join(publicationErr, &videocollision.ConflictError{Code: videoCollisionStaleCode, Collision: videocollision.Collision{Source: s.sourcePath, Destination: target, Kind: "stale"}})
		}
		return publicationErr
	}
	code := "VIDEO_COLLISION_REFRESHED"
	if explicit && decision.Policy == videocollision.Replace {
		if collision != nil && collision.ReviewToken == decision.ReviewToken {
			return publicationErr
		}
		code = videoCollisionStaleCode
	}
	if collision == nil {
		if code != videoCollisionStaleCode {
			return publicationErr
		}
		collision = &videocollision.Collision{Source: s.sourcePath, Destination: target, Kind: "missing"}
	}
	return errors.Join(publicationErr, &videocollision.ConflictError{Code: code, Collision: *collision})
}

func (s *artifactStage) armReviewedVideo(ctx context.Context, batch *downloader.ReplacementBatch, plan *organizer.OrganizePlan) (bool, error) {
	decision, ok := s.original.Organize.VideoCollisionDecisions[filepath.Clean(s.sourcePath)]
	if !ok || decision.Policy != videocollision.Replace {
		return batch.BeforePublish(ctx, plan.TargetPath, s.original.Organize.ForceUpdate)
	}
	scope := videoReviewScope(s.original.Organize.VideoCollisionScope, s.original.Movie.RenderGeneration, s.original.Organize.VideoCollisionGeneration, plan)
	return batch.BeforePublishChecked(ctx, plan.TargetPath, true, func(path string) error {
		return verifyVideoReview(ctx, s.fs, decision, s.sourcePath, plan.TargetPath, scope, path)
	})
}
