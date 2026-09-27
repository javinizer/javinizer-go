package r18devdump

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeLogicalQuerySQL(t *testing.T) {
	path := importFixture(t)
	store, err := OpenContext(context.Background(), path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Sane row on the canonical fixture: no probe may count it.
	n, err := queryCount(context.Background(), store.db,
		"SELECT COUNT(*) FROM videos WHERE content_id IS NOT NULL AND ("+
			"UPPER("+buildNormExprOnly("content_id")+") != content_id)")
	_ = n
	_ = err
	t.Logf("noncanonical count current-predicate: %d err=%v, normExpr=%s", n, err, buildNormExprOnly("content_id"))
}

func buildNormExprOnly(col string) string {
	expr := col
	chain := "REPLACE(" + expr + ", '-', '')"
	for _, cExpr := range []string{"' '", "char(9)", "char(10)", "char(11)", "char(12)", "char(13)"} {
		chain = "REPLACE(" + chain + ", " + cExpr + ", '')"
	}
	return "UPPER(" + chain + ")"
}
