package workflow

import (
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

type nfoSnapshotReadProbe struct {
	afero.Fs
	expected, correct, foreign string
	reads                      []string
}

func (f *nfoSnapshotReadProbe) Open(p string) (afero.File, error) {
	f.reads = append(f.reads, p)
	if p == f.expected {
		return f.Fs.Open(f.correct)
	}
	return f.Fs.Open(f.foreign)
}
func checkNFONativeSnapshotPath(t *testing.T, input, expected string) {
	t.Helper()
	base := afero.NewMemMapFs()
	correct := filepath.Join(string(filepath.Separator), "correct.nfo")
	foreign := filepath.Join(string(filepath.Separator), "foreign.nfo")
	require.NoError(t, afero.WriteFile(base, correct, []byte("intended snapshot"), 0644))
	require.NoError(t, afero.WriteFile(base, foreign, []byte("wrong volume"), 0644))
	fs := &nfoSnapshotReadProbe{Fs: base, expected: expected, correct: correct, foreign: foreign}
	got := readNFOSnapshot(logging.GlobalLogger(), fs, input)
	require.Equal(t, []string{expected}, fs.reads)
	require.Equal(t, expected, got.FoundPath)
	require.Equal(t, "intended snapshot", got.Content)
	data, err := afero.ReadFile(base, foreign)
	require.NoError(t, err)
	require.Equal(t, "wrong volume", string(data))
}
func TestNFONativeSnapshotPathCompatibility(t *testing.T) {
	for _, p := range []string{filepath.Join(t.TempDir(), "movie.nfo"), filepath.Join("relative", "movie.nfo"), filepath.Join(string(filepath.Separator), "virtual", "movie.nfo")} {
		t.Run(p, func(t *testing.T) {
			expected, err := fsutil.CanonicalizePath(p)
			require.NoError(t, err)
			if filepath.IsAbs(p) {
				expected = filepath.Clean(p)
			}
			checkNFONativeSnapshotPath(t, p, expected)
		})
	}
}

type nativeSnapshotResolver struct {
	nfo.NFOFieldMerger
	path string
}

func (f nativeSnapshotResolver) ResolveNFOPath(string, *models.Movie, nfo.NFONameConfig, string) (string, []string) {
	return f.path, nil
}
func checkNFONativeCapturePersistence(t *testing.T, source, path string) {
	t.Helper()
	base := afero.NewMemMapFs()
	correct := filepath.Join(string(filepath.Separator), "correct.nfo")
	foreign := filepath.Join(string(filepath.Separator), "foreign.nfo")
	require.NoError(t, afero.WriteFile(base, correct, []byte("intended snapshot"), 0644))
	require.NoError(t, afero.WriteFile(base, foreign, []byte("wrong volume"), 0644))
	fs := &nfoSnapshotReadProbe{Fs: base, expected: filepath.Clean(path), correct: correct, foreign: foreign}
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	record := &models.BatchFileOperation{ID: 55, OriginalPath: source, RevertStatus: models.RevertStatusApplied}
	repo.On("FindByID", mock.Anything, uint(55)).Return(record, nil).Once()
	repo.On("UpdateNonJournalFields", mock.Anything, mock.MatchedBy(func(got *models.BatchFileOperation) bool {
		return got.NFOPath == filepath.Clean(path) && got.NFOSnapshot == "intended snapshot" && got.OriginalPath == source
	})).Return(nil).Once()
	log := NewDBRevertLog(repo, nil, "native-snapshot", fs, nil, nativeSnapshotResolver{path: path}, nil)
	log.CaptureSnapshot(t.Context(), "55", ApplyCmd{Movie: &models.Movie{ID: "MOVIE"}, Match: models.FileMatchInfo{Path: source}})
	require.Equal(t, filepath.Clean(path), record.NFOPath)
	require.Equal(t, "intended snapshot", record.NFOSnapshot)
}
func TestNFONativeSnapshotCapturePersistsPath(t *testing.T) {
	root := t.TempDir()
	checkNFONativeCapturePersistence(t, filepath.Join(root, "movie.mp4"), filepath.Join(root, "movie.nfo"))
}
