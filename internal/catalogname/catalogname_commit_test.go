package catalogname

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFinishSnapshotRequiresSuccessfulCommit(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "committed"
		if canceled {
			name = "canceled after consuming rows"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			db := fixture(t, path)
			db.SetMaxOpenConns(1)
			_, err := db.Exec(`INSERT INTO actress_aliases VALUES ('Stage Name','Hatano Yui')`)
			require.NoError(t, err)
			healthy, err := Load(t.Context(), path)
			require.NoError(t, err)
			want := Decision{Resolved: true, FirstName: "Yui", LastName: "Hatano", CatalogID: 1, Confidence: "catalog_consistent", Reason: "matched"}
			wantAlias := Decision{Confidence: "none", Reason: "alias_name_mismatch"}
			require.Equal(t, want, healthy.Resolve("Hatano Yui"))
			before, err := os.ReadFile(path)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
			require.NoError(t, err)
			var snapshot *Snapshot
			func() {
				// Match Load's transaction ownership: finalization commits, and
				// the caller always defers rollback for any failure.
				defer func() { _ = tx.Rollback() }()
				var r record
				rows, readErr := tx.QueryContext(ctx, `SELECT id, first_name, last_name, japanese_name, origin, verified, ambiguity_quarantined FROM actresses`)
				require.NoError(t, readErr)
				require.True(t, rows.Next())
				require.NoError(t, rows.Scan(&r.id, &r.first, &r.last, &r.japanese, &r.origin, &r.verified, &r.quarantined))
				require.False(t, rows.Next())
				require.NoError(t, rows.Err())
				require.NoError(t, rows.Close())
				var a alias
				rows, readErr = tx.QueryContext(ctx, `SELECT alias_name, canonical_name FROM actress_aliases`)
				require.NoError(t, readErr)
				require.True(t, rows.Next())
				require.NoError(t, rows.Scan(&a.name, &a.canonical))
				require.False(t, rows.Next())
				require.NoError(t, rows.Err())
				require.NoError(t, rows.Close())
				require.NoError(t, ctx.Err(), "both reads must complete before cancellation")
				require.Equal(t, 1, db.Stats().InUse, "the read transaction still owns the sole connection")

				if canceled {
					cancel()
					require.ErrorIs(t, ctx.Err(), context.Canceled)
				}
				snapshot, err = finishSnapshot(tx, []record{r}, []alias{a})
				if !canceled {
					require.NoError(t, err)
					// A helper that returned a snapshot without committing would
					// leave this transaction open until the deferred rollback.
					require.ErrorIs(t, tx.Rollback(), sql.ErrTxDone)
				}
			}()

			if canceled {
				require.Nil(t, snapshot, "fully read data cannot escape a failed commit")
				require.ErrorContains(t, err, "finish catalog snapshot: ")
				cause := errors.Unwrap(err)
				require.NotNil(t, cause, "the commit error must remain available to callers")
				// database/sql may either observe the canceled context first or
				// finish its automatic rollback first. Both are real Commit errors.
				require.True(t, errors.Is(cause, context.Canceled) || errors.Is(cause, sql.ErrTxDone), "unexpected commit error: %v", err)
				require.Equal(t, "finish catalog snapshot: "+cause.Error(), err.Error())
			} else {
				require.NoError(t, err)
				require.NotNil(t, snapshot)
				require.Equal(t, want, snapshot.Resolve("Hatano Yui"))
				require.Equal(t, wantAlias, snapshot.Resolve("Stage Name"), "curated aliases must reach the snapshot")
			}
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after, "snapshot finalization must not change the catalog")

			// A bounded write through the single-connection pool proves that
			// finalization/caller cleanup released the transaction and locks.
			recoveryCtx, recoveryCancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer recoveryCancel()
			_, err = db.ExecContext(recoveryCtx, `UPDATE actresses SET first_name='Changed'`)
			require.NoError(t, err)
			recovered, err := Load(recoveryCtx, path)
			require.NoError(t, err)
			require.True(t, recovered.Resolve("Changed Hatano").Resolved)
			require.Equal(t, want, healthy.Resolve("Hatano Yui"), "earlier snapshots remain detached")
			if snapshot != nil {
				require.Equal(t, want, snapshot.Resolve("Hatano Yui"), "successful finalization also detaches its data")
			}
		})
	}
}
