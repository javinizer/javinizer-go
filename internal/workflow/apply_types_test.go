package workflow

import (
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/assert"
)

func TestApplyCmd_HasOrganizeField(t *testing.T) {
	cmd := ApplyCmd{}
	fieldType := reflect.TypeOf(cmd).Field(4) // Organize is the 5th field
	assert.Equal(t, "Organize", fieldType.Name)
	assert.Equal(t, "OrganizeOptions", fieldType.Type.Name())
}

func TestApplyCmd_HasMergeField(t *testing.T) {
	cmd := ApplyCmd{}
	fieldType := reflect.TypeOf(cmd).Field(5) // Merge is the 6th field
	assert.Equal(t, "Merge", fieldType.Name)
	assert.Equal(t, "MergeOptions", fieldType.Type.Name())
}

func TestOrganizeOptions_HasExpectedFields(t *testing.T) {
	opts := OrganizeOptions{
		Skip:        true,
		MoveFiles:   true,
		LinkMode:    organizer.LinkModeHard,
		ForceUpdate: true,
	}
	assert.True(t, opts.Skip)
	assert.True(t, opts.MoveFiles)
	assert.Equal(t, organizer.LinkModeHard, opts.LinkMode)
	assert.True(t, opts.ForceUpdate)
}

func TestMergeOptions_HasExpectedFields(t *testing.T) {
	opts := MergeOptions{
		ForceOverwrite: true,
		PreserveNFO:    true,
		ScalarStrategy: nfo.PreferNFO,
		ArrayStrategy:  true,
	}
	assert.True(t, opts.ForceOverwrite)
	assert.True(t, opts.PreserveNFO)
	assert.Equal(t, nfo.PreferNFO, opts.ScalarStrategy)
	assert.True(t, opts.ArrayStrategy)
}

func TestApplyResult_HasOperationID(t *testing.T) {
	result := ApplyResult{OperationID: "42"}
	assert.Equal(t, "42", result.OperationID)
}

func TestWorkflowInterface_HasApplyMethod(t *testing.T) {
	iface := reflect.TypeOf((*WorkflowInterface)(nil)).Elem()
	_, ok := iface.MethodByName("Apply")
	assert.True(t, ok, "WorkflowInterface must have Apply method")
}

func TestApply_Signature(t *testing.T) {
	iface := reflect.TypeOf((*WorkflowInterface)(nil)).Elem()
	method, ok := iface.MethodByName("Apply")
	if !ok {
		t.Fatal("Apply method not found on WorkflowInterface")
	}
	// Interface method: no receiver, params are: ctx, cmd, progress
	assert.Equal(t, 3, method.Type.NumIn(), "Apply should have 3 params (ctx, cmd, progress)")
	assert.Equal(t, 2, method.Type.NumOut(), "Apply should return 2 values (*ApplyResult, error)")
}

func TestApplyCmd_Fields(t *testing.T) {
	cmd := ApplyCmd{
		Movie:           &models.Movie{ID: "TEST-001"},
		Match:           models.FileMatchInfo{MovieID: "TEST-001"},
		DestPath:        "/dest",
		DryRun:          true,
		Organize:        OrganizeOptions{Skip: true},
		Merge:           MergeOptions{ForceOverwrite: true},
		Download:        true,
		GenerateNFO:     true,
		DisplayTitleSrc: &models.Movie{ID: "TEST-001"},
	}
	assert.NotNil(t, cmd.Movie)
	assert.Equal(t, "TEST-001", cmd.Match.MovieID)
	assert.Equal(t, "/dest", cmd.DestPath)
	assert.True(t, cmd.DryRun)
	assert.True(t, cmd.Organize.Skip)
	assert.True(t, cmd.Merge.ForceOverwrite)
	assert.True(t, cmd.Download)
	assert.True(t, cmd.GenerateNFO)
	assert.NotNil(t, cmd.DisplayTitleSrc)
}

func TestStepCompletion_Fields(t *testing.T) {
	sc := stepCompletion{
		Organized:    true,
		Merged:       true,
		DisplayTitle: true,
		Downloaded:   true,
		NFOGenerated: true,
	}
	assert.True(t, sc.Organized)
	assert.True(t, sc.Merged)
	assert.True(t, sc.DisplayTitle)
	assert.True(t, sc.Downloaded)
	assert.True(t, sc.NFOGenerated)
}

func TestStepCompletion_ZeroValue(t *testing.T) {
	sc := stepCompletion{}
	assert.False(t, sc.Organized)
	assert.False(t, sc.Merged)
	assert.False(t, sc.DisplayTitle)
	assert.False(t, sc.Downloaded)
	assert.False(t, sc.NFOGenerated)
}

func TestApplyResult_HasStepCompletion(t *testing.T) {
	result := ApplyResult{
		OperationID: "42",
		Steps:       stepCompletion{Organized: true},
	}
	assert.Equal(t, "42", result.OperationID)
	assert.True(t, result.Steps.Organized)
	assert.False(t, result.Steps.Downloaded)
}
