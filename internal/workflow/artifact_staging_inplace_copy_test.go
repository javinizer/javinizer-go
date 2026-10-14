package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestArtifactInPlaceCopyStaysInsideFence(t *testing.T) {
	for _, rejectAfterPublish := range []bool{false, true} {
		name := "before-publish"
		if rejectAfterPublish {
			name = "after-publish"
		}
		t.Run(name, func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			incoming := filepath.Join(root, "incoming")
			require.NoError(t, os.Mkdir(incoming, 0755))
			source := filepath.Join(incoming, "ABC-123 original.mkv")
			subtitle := filepath.Join(incoming, "ABC-123 original.en.srt")
			require.NoError(t, os.WriteFile(source, []byte("video"), 0644))
			require.NoError(t, os.WriteFile(subtitle, []byte("subtitle"), 0644))
			movie := models.Movie{ID: "ABC-123", ContentID: "inplace-copy-rejected", RenderGeneration: 1}
			m, err := matcher.NewMatcher(&matcher.Config{})
			require.NoError(t, err)
			org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), m)
			orch := &applyOrchImpl{fs: fs, organizer: org}
			cmd := ApplyCmd{Movie: &movie, PublicationFence: &pr260FiniteAuthorityFence{authoritative: &movie, result: os.ErrPermission}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: incoming, Organize: OrganizeOptions{MoveFiles: false}, OperationMode: operationmode.OperationModeInPlace}
			if rejectAfterPublish {
				cmd.PublicationFence = postPublishFence{movie: &movie, err: os.ErrPermission}
			}
			_, err = orch.Execute(t.Context(), cmd)
			require.ErrorIs(t, err, os.ErrPermission)
			for path, want := range map[string]string{source: "video", subtitle: "subtitle"} {
				body, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, want, string(body))
			}
			target := filepath.Join(root, "renamed")
			if rejectAfterPublish {
				// Generic publication may leave its empty mkdir after rollback;
				// it must never leave copied video or subtitle bytes behind.
				entries, err := os.ReadDir(target)
				if !os.IsNotExist(err) {
					require.NoError(t, err)
					require.Empty(t, entries)
				}
			} else {
				require.NoDirExists(t, target, "the stage must remain private before publication")
			}
		})
	}
}

type stagedInPlaceCopyPlanner struct {
	*organizer.Organizer
	plan    *organizer.OrganizePlan
	err     error
	command organizer.OrganizeCmd
}

func (p *stagedInPlaceCopyPlanner) PlanOrganize(_ context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	p.command = cmd
	return p.plan, p.err
}

func TestArtifactInPlaceCopyPlanningRefusals(t *testing.T) {
	for _, kind := range []string{"missing-seam", "plan-error", "escaping-root", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "input", "ABC-123.mkv")
			staged := filepath.Join(root, ".stage", "ABC-123.mkv")
			target := filepath.Join(root, "renamed")
			stage := &artifactStage{inPlace: true, sourcePath: source, original: ApplyCmd{Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source)}, DestPath: filepath.Dir(source)}}
			state := &applyPipelineState{artifact: stage}
			cmd := ApplyCmd{Match: models.FileMatchInfo{Path: staged}, DestPath: filepath.Dir(staged), OperationMode: operationmode.OperationModeInPlace}
			planner := &stagedInPlaceCopyPlanner{plan: &organizer.OrganizePlan{TargetDir: target}}
			orch := &applyOrchImpl{organizer: planner}
			want := ""
			switch kind {
			case "missing-seam":
				orch.organizer = &stubOrganizer{}
				want = "requires the organizer planning seam"
			case "plan-error":
				planner.err = errors.New("original plan refused")
				want = planner.err.Error()
			case "escaping-root":
				planner.plan.TargetDir = filepath.Join(root, "..", "outside")
				want = "escapes staging area"
			case "conflict":
				planner.plan.Conflicts = []organizer.PlanConflict{{Path: filepath.Join(target, "ABC-123.mkv")}}
				want = "destination conflict"
			}
			steps := &stepCompletion{}
			err := orch.stepOrganize(t.Context(), cmd, state, steps)
			require.ErrorContains(t, err, want)
			require.False(t, steps.Organized)
			require.Nil(t, state.organizeResult)
			if kind != "missing-seam" {
				require.Equal(t, source, planner.command.Match.Path)
				require.Equal(t, stage.original.DestPath, planner.command.DestDir)
				require.Equal(t, operationmode.OperationModeInPlace, planner.command.OperationMode)
			}
		})
	}
}

func TestArtifactInPlaceCopyForeignPrimary(t *testing.T) {
	env := setupCopyIntentE2E(t, "adversarial-inplace-primary")
	incoming := filepath.Dir(env.source)
	target := filepath.Join(incoming, env.movie.ID+".mp4")
	require.NoError(t, os.WriteFile(target, []byte("foreign primary must survive"), 0644))
	env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
	cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	_, applyErr := env.orch.Execute(t.Context(), cmd)
	require.ErrorContains(t, applyErr, "destination conflict")
	b, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "foreign primary must survive", string(b))
	require.FileExists(t, env.source)
}

func TestArtifactInPlaceCopyPathBudgetWithSubtitles(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		t.Run(string(mode), func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			incoming := filepath.Join(root, "incoming")
			require.NoError(t, os.Mkdir(incoming, 0755))
			source := filepath.Join(incoming, "ABC-123 original.mkv")
			require.NoError(t, os.WriteFile(source, []byte("video"), 0644))
			require.NoError(t, os.WriteFile(filepath.Join(incoming, "ABC-123 original.en.srt"), []byte("subtitle"), 0644))
			movie := models.Movie{ID: "ABC-123", ContentID: "path-budget", RenderGeneration: 1}
			finalDir := incoming
			if mode == operationmode.OperationModeInPlace {
				finalDir = filepath.Join(root, "renamed")
			}
			expected := filepath.Join(finalDir, "ABC-123.mkv")
			budget := len(expected) + 8
			m, e := matcher.NewMatcher(&matcher.Config{})
			require.NoError(t, e)
			org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, MaxPathLength: budget, OperationMode: mode}, template.NewEngine(), m)
			match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}
			plan, e := org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: incoming, MoveFiles: false, OperationMode: mode})
			require.NoError(t, e)
			require.Equal(t, expected, plan.TargetPath)
			orch := &applyOrchImpl{fs: fs, organizer: org}
			result, e := orch.Execute(t.Context(), ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: match, DestPath: incoming, Organize: OrganizeOptions{MoveFiles: false}, OperationMode: mode})
			require.NoError(t, e)
			require.Equal(t, expected, result.OrganizeResult.NewPath)
			require.FileExists(t, filepath.Join(finalDir, "ABC-123.eng.srt"))
			require.FileExists(t, source)
		})
	}
}

func TestArtifactInPlaceCopyPreservesTruncatedFinalLeaf(t *testing.T) {
	for _, inputName := range []string{"input", strings.Repeat("long", 16)} {
		t.Run(inputName, func(t *testing.T) {
			fs := afero.NewOsFs()
			incoming := filepath.Join(t.TempDir(), inputName)
			require.NoError(t, fs.MkdirAll(incoming, 0755))
			source := filepath.Join(incoming, "ABC-123 original.mkv")
			subtitle := filepath.Join(incoming, "ABC-123 original.en.srt")
			require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0644))
			require.NoError(t, afero.WriteFile(fs, subtitle, []byte("subtitle"), 0644))
			movie := models.Movie{ID: "ABC-123", Title: strings.Repeat("long title ", 10), ContentID: "truncated-path-budget", RenderGeneration: 1}
			cfg := &organizer.Config{FileFormat: "<ID> <TITLE>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, MaxPathLength: len(incoming) + 25, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}
			org := organizer.NewOrganizer(fs, cfg, template.NewEngine(), nil)
			match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}
			original, err := org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: incoming, OperationMode: cfg.OperationMode})
			require.NoError(t, err)
			require.LessOrEqual(t, len(original.TargetPath), cfg.MaxPathLength)
			require.Less(t, len(original.TargetFile), len(movie.ID+" "+movie.Title+".mkv"))
			subs := org.PlanSubtitleMoves(original)
			require.Len(t, subs, 1)
			orch := &applyOrchImpl{fs: fs, organizer: org}
			result, err := orch.Execute(t.Context(), ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: match, DestPath: incoming, OperationMode: cfg.OperationMode})
			require.NoError(t, err)
			require.Equal(t, original.TargetPath, result.OrganizeResult.NewPath)
			require.Equal(t, original.TargetFile, result.OrganizeResult.FileName)
			require.FileExists(t, original.TargetPath)
			require.FileExists(t, subs[0].NewPath)
			require.FileExists(t, source)
			require.FileExists(t, subtitle)
			require.Equal(t, len(incoming)+25, cfg.MaxPathLength)
		})
	}
}
