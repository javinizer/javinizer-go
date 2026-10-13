package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// codex P2 (PRRT_kwDORn9KaM6rLI28): every leg of the deferred multipart
// sibling install fails closed — an indeterminate destination probe, a failed
// inverse journal, or a refused move must stop the publication instead of
// quietly skipping the sibling or clobbering what occupies the name.

type deferredSiblingDenyFS struct {
	afero.Fs
	armed bool
}

func (fs *deferredSiblingDenyFS) Stat(name string) (os.FileInfo, error) {
	if fs.armed && strings.HasSuffix(name, "-cd2.mp4") {
		dir := filepath.Dir(filepath.Dir(name))
		if filepath.Base(dir) == "library" || strings.Contains(name, string(filepath.Separator)+"library"+string(filepath.Separator)) {
			return nil, errors.New("destination probe indeterminate")
		}
	}
	return fs.Fs.Stat(name)
}

type armAfterExecOrganizer struct {
	deferredOrganizerBase
	onExec func()
}

func (w armAfterExecOrganizer) ExecuteOrganizePlan(plan *organizer.OrganizePlan, hardlinkFailure bool, link organizer.LinkMode) (*organizer.OrganizeResult, error) {
	res, err := w.deferredOrganizerBase.ExecuteOrganizePlan(plan, hardlinkFailure, link)
	if w.onExec != nil {
		w.onExec()
	}
	return res, err
}

func TestDeferredSiblingInstallRefusesIndeterminateTarget(t *testing.T) {
	var probe *deferredSiblingDenyFS
	env := buildDeferredArmEnv(t, "def-sib-statdeny",
		func(base afero.Fs) afero.Fs { probe = &deferredSiblingDenyFS{Fs: base}; return probe },
		func(_ afero.Fs, o organizer.OrganizerInterface) organizer.OrganizerInterface {
			return armAfterExecOrganizer{deferredOrganizerBase{o}, func() { probe.armed = true }}
		})
	_, err := env.orch.Execute(t.Context(), pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false))
	require.Error(t, err)
	assert.ErrorContains(t, err, "inspect deferred publication sibling")
}

type moveIntentDenyLog struct {
	RevertLog
}

func (l moveIntentDenyLog) RecordMoveIntent(ctx context.Context, opID, source, target string) error {
	if strings.HasSuffix(source, "-cd2.mp4") || strings.HasSuffix(target, "-cd2.mp4") {
		return errors.New("inverse journal unavailable")
	}
	return l.RevertLog.RecordMoveIntent(ctx, opID, source, target)
}

func TestDeferredSiblingInstallRefusesWhenIntentJournalingFails(t *testing.T) {
	env := buildDeferredArmEnv(t, "def-sib-intentdeny", nil, nil)
	env.orch.revertLog = moveIntentDenyLog{env.orch.revertLog}
	_, err := env.orch.Execute(t.Context(), pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false))
	require.Error(t, err)
	assert.ErrorContains(t, err, "journal deferred sibling move intent")
}

type denySourceRenameFS struct {
	afero.Fs
	armed bool
}

func (fs *denySourceRenameFS) Rename(oldname, newname string) error {
	if fs.armed && strings.HasSuffix(oldname, "-cd2.mp4") {
		return errors.New("move refused: kernel rename unavailable")
	}
	return fs.Fs.Rename(oldname, newname)
}

func TestDeferredSiblingInstallRefusesWhenMoveFails(t *testing.T) {
	var probe *denySourceRenameFS
	env := buildDeferredArmEnv(t, "def-sib-movedeny",
		func(base afero.Fs) afero.Fs { probe = &denySourceRenameFS{Fs: base}; return probe },
		func(_ afero.Fs, o organizer.OrganizerInterface) organizer.OrganizerInterface {
			return armAfterExecOrganizer{deferredOrganizerBase{o}, func() { probe.armed = true }}
		})
	_, err := env.orch.Execute(t.Context(), pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false))
	require.Error(t, err)
	assert.ErrorContains(t, err, "publish deferred sibling from source")
}
