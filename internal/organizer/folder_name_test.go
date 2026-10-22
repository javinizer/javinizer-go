package organizer

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestFolderNameBudget(t *testing.T) {
	for _, tc := range []struct {
		name, base, file        string
		limit, budget, overhead int
	}{
		{"unlimited", "/dest", "a.mp4", 0, 0, 12},
		{"negative", "/dest", "a.mp4", -5, 0, 12},
		{"exact exhaustion", "/dest", "a.mp4", 12, 0, 12},
		{"over exhaustion", "/dest", "a.mp4", 11, 0, 12},
		{"one byte left", "/dest", "a.mp4", 13, 1, 12},
		{"nested UTF8 bytes", "/映/画", "映.mp4", 30, 13, 17},
		{"cleaned nested path", "/dest/a/../b", "a.mp4", 20, 6, 14},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget, overhead := folderNameBudget(tc.base, tc.file, tc.limit)
			require.Equal(t, tc.budget, budget)
			require.Equal(t, tc.overhead, overhead)
		})
	}
}

type folderBudgetRecordingEngine struct {
	template.EngineInterface
	calls                 []string
	budgets               []int
	initialErr, budgetErr error
	budgetResult          *string
}

func (e *folderBudgetRecordingEngine) Execute(format string, ctx *template.Context) (string, error) {
	e.calls = append(e.calls, "execute:"+format)
	if e.initialErr != nil {
		return "", e.initialErr
	}
	return e.EngineInterface.Execute(format, ctx)
}
func (e *folderBudgetRecordingEngine) ExecuteWithMaxBytes(format string, ctx *template.Context, budget int) (string, error) {
	e.calls = append(e.calls, "budget:"+format)
	e.budgets = append(e.budgets, budget)
	if e.budgetErr != nil {
		return "", e.budgetErr
	}
	if e.budgetResult != nil {
		return *e.budgetResult, nil
	}
	return e.EngineInterface.ExecuteWithMaxBytes(format, ctx, budget)
}

func TestRenderBudgetedFolderName(t *testing.T) {
	sentinel := errors.New("budget failure")
	for _, tc := range []struct {
		name, result, id, want string
		budget                 int
		err                    error
	}{
		{name: "unlimited keeps initial", result: "other", want: "initial", budget: 0},
		{name: "negative keeps initial", result: "other", want: "initial", budget: -1},
		{name: "sanitizes rendered path", result: "one*/two?", want: "one_two", budget: 50},
		{name: "ID fallback", result: "***", id: "ABC?", want: "ABC", budget: 50},
		{name: "unknown fallback", result: "***", id: "???", want: "unknown", budget: 50},
		{name: "wrapped error", budget: 50, err: sentinel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := &folderBudgetRecordingEngine{budgetResult: &tc.result, budgetErr: tc.err}
			got, err := renderBudgetedFolderName(engine, "<TITLE>", planContext{FolderName: "initial"}, tc.id, tc.budget)
			if tc.err != nil {
				require.ErrorIs(t, err, sentinel)
				require.EqualError(t, err, "failed to generate folder name: budget failure")
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
			if tc.budget <= 0 {
				require.Empty(t, engine.calls)
			} else {
				require.Equal(t, []int{tc.budget}, engine.budgets)
			}
		})
	}
}

func TestPathLengthRegression_SharedFolderBudgetStrategyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		inplace, dedicated, organizeMode bool
		limit                            int
	}{
		{name: "organize nested UTF8", limit: 90},
		{name: "organize unlimited", limit: 0},
		{name: "organize negative", limit: -1},
		{name: "dedicated source parent", inplace: true, dedicated: true, limit: 90},
		{name: "mixed untouched", inplace: true, limit: 90},
		{name: "mixed organize destination", inplace: true, organizeMode: true, limit: 90},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			source := "/source/old/ABC-001.mp4"
			require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0755))
			require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0644))
			if !tc.dedicated {
				require.NoError(t, afero.WriteFile(fs, "/source/old/XYZ-002.mp4", []byte("video"), 0644))
			}
			m, err := matcher.NewMatcher(&matcher.Config{})
			require.NoError(t, err)
			cfg := &Config{FolderFormat: "<TITLE>", MaxPathLength: tc.limit, OperationMode: operationmode.OperationModeOrganize, SubfolderFormat: []string{"映", "画", "***"}}
			engine := &folderBudgetRecordingEngine{EngineInterface: template.NewEngine()}
			var strategy OperationStrategy = newOrganizeStrategy(fs, cfg, engine, nil)
			base := filepath.Join("/dest", "映/画")
			if tc.inplace {
				cfg.OperationMode = operationmode.OperationModeInPlace
				if tc.organizeMode {
					cfg.OperationMode = operationmode.OperationModeOrganize
				}
				strategy = newInPlaceStrategy(fs, cfg, m, engine)
				base = "/source"
				if tc.organizeMode && !tc.dedicated {
					base = "/dest"
				}
			}
			plan, err := strategy.Plan(models.FileMatchInfo{Path: source, Name: "ABC-001.mp4", MovieID: "ABC-001"}, &models.Movie{ID: "ABC-001", Title: strings.Repeat("日本語", 40)}, "/dest", false)
			require.NoError(t, err)
			require.Equal(t, "execute:<TITLE>", engine.calls[0])
			if tc.limit <= 0 || (tc.inplace && !tc.dedicated && !tc.organizeMode) {
				require.Empty(t, engine.budgets)
				if tc.inplace {
					require.Equal(t, source, plan.TargetPath)
				}
			} else {
				require.Equal(t, []int{tc.limit - (len(filepath.Join(base, "X", "ABC-001.mp4")) - 1)}, engine.budgets)
				require.LessOrEqual(t, len(plan.TargetPath), tc.limit)
				require.True(t, utf8.ValidString(plan.FolderName))
				require.Equal(t, base, filepath.Dir(plan.TargetDir))
			}
		})
	}
}

func TestPathLengthRegression_SharedBudgetErrorsKeepStrategyWording(t *testing.T) {
	for _, inplace := range []bool{false, true} {
		t.Run(fmt.Sprint(inplace), func(t *testing.T) {
			fs := afero.NewMemMapFs()
			cfg := &Config{FolderFormat: "<TITLE>", MaxPathLength: 12, OperationMode: operationmode.OperationModeOrganize}
			engine := &folderBudgetRecordingEngine{EngineInterface: template.NewEngine()}
			var strategy OperationStrategy = newOrganizeStrategy(fs, cfg, engine, nil)
			want := "path validation failed: destination directory and filename overhead (12 bytes) already exceeds max_path_length (12); reduce the destination path or increase max_path_length"
			if inplace {
				strategy = newInPlaceStrategy(fs, cfg, nil, engine)
				want = "path validation failed: directory and filename overhead (12 bytes) already exceeds max_path_length (12); reduce the path or increase max_path_length"
			}
			match := models.FileMatchInfo{Path: "/source/a.mp4", Name: "a.mp4"}
			_, err := strategy.Plan(match, &models.Movie{Title: "title"}, "/dest", false)
			require.EqualError(t, err, want)
			require.Equal(t, []string{"execute:<TITLE>"}, engine.calls)
			sentinel := errors.New("initial failure")
			engine.initialErr = sentinel
			_, err = strategy.Plan(match, &models.Movie{}, "/dest", false)
			require.ErrorIs(t, err, sentinel)
			require.EqualError(t, err, "failed to generate folder name: initial failure")
			require.Empty(t, engine.budgets)
		})
	}
}
