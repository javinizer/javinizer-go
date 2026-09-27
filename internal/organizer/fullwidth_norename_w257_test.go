package organizer

// codex P2 (PR #257): rename_file=false must preserve the original filename
// byte-for-byte, including fullwidth-extension spellings. The scanner admits
// RCT-156-HD．ｍｋｖ with a folded Name ("RCT-156-HD.mkv") per the round-30
// folded-extension contract, but the organizer derived the no-rename target
// from that folded Name — so organize and in-place modes renamed the file's
// extension despite rename_file=false. The no-rename target now comes from
// match.Path (the on-disk source of truth per the models.FileMatchInfo
// contract); the folded Name keeps serving matching only.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
)

// fullwidthMatch mirrors scanner-style admission (internal/scanner
// fullwidth.go): Path keeps the raw on-disk spelling — the source of truth
// for file I/O — while Name/Extension carry the extension-folded spelling.
func fullwidthMatch(path string) models.FileMatchInfo {
	return models.FileMatchInfo{
		MovieID:   "RCT-156",
		Path:      path,
		Name:      "RCT-156-HD.mkv",
		Extension: ".mkv",
	}
}

func TestNoRenameFileName(t *testing.T) {
	assert.Equal(t, "RCT-156-HD．ｍｋｖ", noRenameFileName(models.FileMatchInfo{
		Path: "/source/old-name/RCT-156-HD．ｍｋｖ",
		Name: "RCT-156-HD.mkv",
	}), "no-rename target comes from the raw Path basename, never the folded Name")
	assert.Equal(t, "fallback.mp4", noRenameFileName(models.FileMatchInfo{
		Name: "fallback.mp4",
	}), "degenerate match without a Path falls back to Name")
}

func TestSplitRawExtension(t *testing.T) {
	stem, ext := splitRawExtension("RCT-156-HD．ｍｋｖ")
	assert.Equal(t, "RCT-156-HD", stem, "fullwidth extension is stripped from the raw spelling")
	assert.Equal(t, "．ｍｋｖ", ext, "the raw extension spelling is returned")

	stem, ext = splitRawExtension("RCT-156-HD.mkv")
	assert.Equal(t, "RCT-156-HD", stem, "ASCII input reduces to TrimSuffix/Ext semantics")
	assert.Equal(t, ".mkv", ext)

	stem, ext = splitRawExtension("no-extension")
	assert.Equal(t, "no-extension", stem, "no extension stays whole")
	assert.Equal(t, "", ext)

	stem, ext = splitRawExtension("a..mkv")
	assert.Equal(t, "a.", stem, "last-dot parity with filepath.Ext")
	assert.Equal(t, ".mkv", ext)
}

func TestOrganize_RenameFileFalse_FullwidthExtensionKeepsRawName(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/RCT-156-HD．ｍｋｖ", []byte("video"), 0o644))

	org := NewOrganizer(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: false,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:   fullwidthMatch("/in/RCT-156-HD．ｍｋｖ"),
		Movie:   testutil.NewMovieBuilder().WithID("RCT-156").Build(),
		DestDir: "/dest", MoveFiles: true,
		OperationMode: operationmode.OperationModeOrganize,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	exists, err := afero.Exists(fs, "/dest/RCT-156/RCT-156-HD．ｍｋｖ")
	require.NoError(t, err)
	assert.True(t, exists, "rename_file=false must keep the raw fullwidth filename byte-for-byte")

	exists, err = afero.Exists(fs, "/dest/RCT-156/RCT-156-HD.mkv")
	require.NoError(t, err)
	assert.False(t, exists, "the folded Name spelling must not be used as the target name")

	assert.Equal(t, filepath.ToSlash("/dest/RCT-156/RCT-156-HD．ｍｋｖ"), filepath.ToSlash(result.NewPath))
	assert.Equal(t, "RCT-156-HD．ｍｋｖ", result.FileName)
}

func TestInPlace_RenameFileFalse_FullwidthExtensionKeepsRawName(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source/old-name", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/old-name/RCT-156-HD．ｍｋｖ", []byte("video"), 0o644))

	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := NewOrganizer(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: false,
		OperationMode: operationmode.OperationModeInPlace,
	}, nil, m)

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:         fullwidthMatch("/source/old-name/RCT-156-HD．ｍｋｖ"),
		Movie:         testutil.NewMovieBuilder().WithID("RCT-156").Build(),
		MoveFiles:     true,
		OperationMode: operationmode.OperationModeInPlace,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	exists, err := afero.Exists(fs, "/source/RCT-156/RCT-156-HD．ｍｋｖ")
	require.NoError(t, err)
	assert.True(t, exists, "in-place no-rename must keep the raw fullwidth filename byte-for-byte")

	exists, err = afero.Exists(fs, "/source/RCT-156/RCT-156-HD.mkv")
	require.NoError(t, err)
	assert.False(t, exists, "the inner rename must not fire when rename_file=false")

	exists, err = afero.Exists(fs, "/source/old-name")
	require.NoError(t, err)
	assert.False(t, exists, "the folder rename itself still happens")

	assert.True(t, result.InPlaceRenamed, "folder rename still happens with rename_file=false")
	assert.Equal(t, filepath.ToSlash("/source/RCT-156/RCT-156-HD．ｍｋｖ"), filepath.ToSlash(result.NewPath))
	assert.Equal(t, "RCT-156-HD．ｍｋｖ", result.FileName)
}

func TestInPlace_RenameFileTrue_FullwidthExtension_CanonicalRenameUnchanged(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source/old-name", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/old-name/RCT-156-HD．ｍｋｖ", []byte("video"), 0o644))

	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := NewOrganizer(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
		OperationMode: operationmode.OperationModeInPlace,
	}, nil, m)

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:         fullwidthMatch("/source/old-name/RCT-156-HD．ｍｋｖ"),
		Movie:         testutil.NewMovieBuilder().WithID("RCT-156").Build(),
		MoveFiles:     true,
		OperationMode: operationmode.OperationModeInPlace,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	exists, err := afero.Exists(fs, "/source/RCT-156/RCT-156.mkv")
	require.NoError(t, err)
	assert.True(t, exists, "rename_file=true still renames to the canonical template name (matched id), not the folded Name")

	assert.Equal(t, filepath.ToSlash("/source/RCT-156/RCT-156.mkv"), filepath.ToSlash(result.NewPath))
	assert.Equal(t, "RCT-156.mkv", result.FileName)
}

func TestInPlaceNoRenameFolder_RenameFileFalse_FullwidthExtension_NoOp(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source/folder", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/folder/RCT-156-HD．ｍｋｖ", []byte("video"), 0o644))

	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := NewOrganizer(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: false,
		OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
	}, nil, m)

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:         fullwidthMatch("/source/folder/RCT-156-HD．ｍｋｖ"),
		Movie:         testutil.NewMovieBuilder().WithID("RCT-156").Build(),
		MoveFiles:     true,
		OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	exists, err := afero.Exists(fs, "/source/folder/RCT-156-HD．ｍｋｖ")
	require.NoError(t, err)
	assert.True(t, exists, "no-rename in-place-norenamefolder keeps the raw spelling exactly where it is")

	exists, err = afero.Exists(fs, "/source/folder/RCT-156-HD.mkv")
	require.NoError(t, err)
	assert.False(t, exists, "no folded-spelling target may appear")

	assert.Equal(t, filepath.ToSlash("/source/folder/RCT-156-HD．ｍｋｖ"), filepath.ToSlash(result.NewPath))
	assert.Equal(t, "RCT-156-HD．ｍｋｖ", result.FileName)
}

func TestInPlaceNoRenameFolderStrategy_Plan_MaxPathLength_FullwidthNoRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FileFormat:    "<ID>",
		RenameFile:    false,
		MaxPathLength: 35,
	}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)

	require.NoError(t, fs.MkdirAll("/source/some-folder", 0o777))
	require.NoError(t, afero.WriteFile(fs, "/source/some-folder/RCT-156-HD．ｍｋｖ", []byte("video"), 0o644))

	plan, err := strategy.Plan(fullwidthMatch("/source/some-folder/RCT-156-HD．ｍｋｖ"),
		testutil.NewMovieBuilder().WithID("RCT-156").Build(), "/dest", false)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(plan.TargetPath), 35, "truncation still fits MaxPathLength")
	assert.True(t, strings.HasSuffix(plan.TargetFile, "．ｍｋｖ"),
		"truncation must cut the raw stem and keep the raw fullwidth extension spelling, got %q", plan.TargetFile)
	assert.False(t, strings.HasSuffix(plan.TargetFile, ".mkv"),
		"truncation must not swap the extension for the folded spelling, got %q", plan.TargetFile)
}

func TestInPlace_FullwidthExtension_SubtitleSidecar(t *testing.T) {
	for _, tt := range []struct {
		name       string
		renameFile bool
		// wantVideo is the video filename inside the renamed folder.
		wantVideo string
		// wantSub is the subtitle filename inside the renamed folder.
		wantSub string
	}{
		{
			name:       "rename disabled keeps folded sidecar stem",
			renameFile: false,
			wantVideo:  "RCT-156-HD．ｍｋｖ",
			wantSub:    "RCT-156-HD.eng.srt",
		},
		{
			name:       "rename enabled renames sidecar to canonical stem",
			renameFile: true,
			wantVideo:  "RCT-156.mkv",
			wantSub:    "RCT-156.eng.srt",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll("/source/old-name", 0o755))
			require.NoError(t, afero.WriteFile(fs, "/source/old-name/RCT-156-HD．ｍｋｖ", []byte("video"), 0o644))
			require.NoError(t, afero.WriteFile(fs, "/source/old-name/RCT-156-HD.eng.srt", []byte("sub"), 0o644))

			m, err := matcher.NewMatcher(&matcher.Config{})
			require.NoError(t, err)
			org := NewOrganizer(fs, &Config{
				FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: tt.renameFile,
				OperationMode: operationmode.OperationModeInPlace,
				MoveSubtitles: true, SubtitleExtensions: []string{".srt"},
			}, nil, m)

			result, err := org.Organize(context.Background(), OrganizeCmd{
				Match:         fullwidthMatch("/source/old-name/RCT-156-HD．ｍｋｖ"),
				Movie:         testutil.NewMovieBuilder().WithID("RCT-156").Build(),
				MoveFiles:     true,
				OperationMode: operationmode.OperationModeInPlace,
			})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, result.Subtitles, 1)
			require.NoError(t, result.Subtitles[0].Error)

			exists, err := afero.Exists(fs, "/source/RCT-156/"+tt.wantVideo)
			require.NoError(t, err)
			assert.True(t, exists, "video at expected name")

			exists, err = afero.Exists(fs, "/source/RCT-156/"+tt.wantSub)
			require.NoError(t, err)
			assert.True(t, exists, "subtitle sidecar at expected name")

			exists, err = afero.Exists(fs, "/source/RCT-156/RCT-156-HD．ｍｋｖ.eng.srt")
			require.NoError(t, err)
			assert.False(t, exists, "the fullwidth video extension must not leak into the sidecar stem")
		})
	}
}
