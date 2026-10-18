package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"testing"
)

type castGateOrganizer struct {
	calls int
	fs    afero.Fs
}

func (o *castGateOrganizer) Organize(_ context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	o.calls++
	return nil, o.fs.Rename(cmd.Match.Path, "/mutated.mp4")
}

func TestNFOCastAmbiguityStopsApplyBeforePublicationOrUnfencedMove(t *testing.T) {
	for _, fenced := range []bool{false, true} {
		t.Run(map[bool]string{false: "unfenced", true: "fenced"}[fenced], func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			a := models.Actress{FirstName: "Canonical", Verified: true}
			require.NoError(t, db.Create(&a).Error)
			movie := models.Movie{ID: "CAST-001", ContentID: "cast001", Title: "Title", RenderGeneration: 7, RenderDirty: true}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: a.ID, Origin: "user", OverrideName: "Current Alias", UserOverride: true}).Error)
			require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{a}))
			snapshot, err := db.Repositories().MovieRepo.FindByID(t.Context(), movie.ID)
			require.NoError(t, err)
			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll("/incoming", 0755))
			files := map[string]string{"/incoming/CAST-001.mp4": "synthetic video", "/incoming/CAST-001.nfo": `<movie><id>CAST-001</id><title>Keep</title><actor><name>Prior Alias</name></actor></movie>`, "/incoming/poster.jpg": "existing artwork"}
			for path, body := range files {
				require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0644))
			}
			cfg := &nfo.Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true}
			engine := template.NewEngine()
			org := &castGateOrganizer{fs: fs}
			log := NewDBRevertLog(database.NewBatchFileOperationRepository(db), NewRevertLogConfig(true, nil), "ambiguity", fs, engine, nil, nil)
			orch := newApplyOrchestrator(fs, org, nil, nfo.NewGenerator(fs, cfg), nfo.NewNFOImplementor(fs, cfg, engine), ApplyConfig{}, engine, log, nil, nil)
			cmd := ApplyCmd{Movie: snapshot, PersistedMovie: true, Match: models.FileMatchInfo{Path: "/incoming/CAST-001.mp4", Name: "CAST-001.mp4", MovieID: movie.ID}, DestPath: "/incoming", Organize: OrganizeOptions{Skip: fenced, MoveFiles: true}, Merge: MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true}, GenerateNFO: true, OperationMode: operationmode.OperationModeInPlace}
			if fenced {
				cmd.PublicationFence = db.Repositories().MovieRepo.(database.ApplyPublicationFencer)
			}
			result, err := orch.Execute(t.Context(), cmd)
			var ambiguity *nfo.CastAmbiguityError
			require.ErrorAs(t, err, &ambiguity)
			require.True(t, result.PrePublication)
			require.Equal(t, "merge", result.FailedStep)
			require.False(t, result.Steps.Merged)
			require.Zero(t, org.calls)
			for path, body := range files {
				got, readErr := afero.ReadFile(fs, path)
				require.NoError(t, readErr)
				require.Equal(t, body, string(got))
			}
			exists, err := afero.Exists(fs, "/mutated.mp4")
			require.NoError(t, err)
			require.False(t, exists)
			var rows []models.BatchFileOperation
			require.NoError(t, db.Find(&rows).Error)
			if fenced {
				require.Len(t, rows, 1)
				require.Empty(t, rows[0].NewPath)
				require.NotEqual(t, models.RevertStatusFailed, rows[0].RevertStatus)
			} else {
				require.Empty(t, rows)
			}
			stored, err := db.Repositories().MovieRepo.FindByID(t.Context(), movie.ID)
			require.NoError(t, err)
			require.Equal(t, snapshot.Credits, stored.Credits)
		})
	}
}

func TestUnfencedStaleLinkWithoutSourceCreditsCannotMoveFirst(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/incoming", 0755))
	require.NoError(t, afero.WriteFile(fs, "/incoming/CAST-001.mp4", []byte("video"), 0644))
	original := []byte(`<movie><id>CAST-001</id><title>Keep</title><actor><name>Prior Alias</name><javinizer-credit version="1" selection="override" movie="cast001" credit="1"/></actor></movie>`)
	require.NoError(t, afero.WriteFile(fs, "/incoming/CAST-001.nfo", original, 0644))
	cfg := &nfo.Config{FilenameTemplate: "<ID>.nfo"}
	org := &castGateOrganizer{fs: fs}
	orch := newApplyOrchestrator(fs, org, nil, nil, nfo.NewNFOImplementor(fs, cfg, nil), ApplyConfig{}, nil, nil, nil, nil)
	result, err := orch.Execute(t.Context(), ApplyCmd{Movie: &models.Movie{ID: "CAST-001", ContentID: "cast001", Title: "Title"}, Match: models.FileMatchInfo{Path: "/incoming/CAST-001.mp4", Name: "CAST-001.mp4"}, DestPath: "/output", Organize: OrganizeOptions{MoveFiles: true}, Merge: MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true}})
	var blocked *nfo.CastAmbiguityError
	require.ErrorAs(t, err, &blocked)
	require.True(t, result.PrePublication)
	require.Zero(t, org.calls)
	body, err := afero.ReadFile(fs, "/incoming/CAST-001.nfo")
	require.NoError(t, err)
	require.Equal(t, original, body)
}

func TestDuplicatePreflightBlocksOnlyAmbiguousFileAndKeepsNoopControl(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/in", 0755))
	nfoBody := []byte(`<movie><id>CAST-001</id><title>Keep</title><actor><name>Unclassified Alias</name></actor></movie>`)
	require.NoError(t, afero.WriteFile(fs, "/in/CAST-001.nfo", nfoBody, 0644))
	require.NoError(t, afero.WriteFile(fs, "/in/CAST-001.mp4", []byte("video"), 0644))
	cfg := &nfo.Config{FilenameTemplate: "<ID>.nfo"}
	gen := &dupGateCountingNFOGen{}
	dl := &dupGateCountingDownloader{}
	ledger := &dupGateCapturingRevertLog{}
	orch := newApplyOrchestrator(fs, &stubOrganizer{result: dupGateSkippedResult()}, dl, gen, nfo.NewNFOImplementor(fs, cfg, nil), ApplyConfig{}, nil, ledger, nil, nil)
	movie := &models.Movie{ID: "CAST-001", ContentID: "cast001", Title: "Title", Credits: []models.MovieCredit{{Origin: "user", CreditedName: "Current"}}}
	cmd := ApplyCmd{Movie: movie, Match: models.FileMatchInfo{Path: "/in/CAST-001.mp4", Name: "CAST-001.mp4"}, DestPath: "/dest", Organize: OrganizeOptions{ForceUpdate: true}, Merge: MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true}, GenerateNFO: true, Download: true}
	blocked, err := orch.Execute(t.Context(), cmd)
	var ambiguity *nfo.CastAmbiguityError
	require.ErrorAs(t, err, &ambiguity)
	require.True(t, blocked.PrePublication)
	require.Empty(t, ledger.completed)
	require.Empty(t, ledger.failed)
	// A following ordinary duplicate remains a successful no-op; the failed
	// preflight has not poisoned shared dependencies or generated output state.
	cmd.Movie = movie.Clone()
	cmd.Movie.Credits = nil
	control, err := orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.True(t, control.OrganizeResult.DuplicateSkipped)
	require.Empty(t, control.NFOPath)
	require.Zero(t, gen.calls.Load())
	require.Zero(t, dl.calls.Load())
	require.Len(t, ledger.completed, 1)
	require.Empty(t, ledger.failed)
	body, err := afero.ReadFile(fs, "/in/CAST-001.nfo")
	require.NoError(t, err)
	require.Equal(t, nfoBody, body)
}
