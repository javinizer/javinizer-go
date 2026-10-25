// Package offline defines the finite resource contract shared by offline cache
// provisioning and its runtime readers. It does not fetch or mutate a database.
package offline

import (
	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
)

// Limits bounds source conversion, immutable artifacts and publication storage.
// Source.Output and Store.Artifact deliberately use the same format budgets.
// Callers also need bounded concurrent work and retained-snapshot counts.
type Limits struct {
	Store  artifactstore.Limits
	Source r18dump.Limits
}

// DefaultLimits returns owned finite budgets, not mutable global configuration.
// These are capacity limits, not a claim that the supplied source is complete.
func DefaultLimits() Limits {
	artifact := actresscache.Limits{
		MaxBytes: 32 << 20, MaxRecordBytes: 16 << 10, MaxRecords: 100_000,
		MaxStringBytes: 4 << 10, MaxTotalStringBytes: 24 << 20,
		MaxAliases: 32, MaxExternalIDs: 16, MaxTokensPerRecord: 512,
	}
	const sourceBytes = 512 << 20
	const sourceRows = 5_000_000
	return Limits{
		Store: artifactstore.Limits{
			MaxDirectoryEntries: 4096, MaxStoredBytes: 1 << 30,
			MaxManifestBytes: 4096, MaxSourceBytes: sourceBytes,
			MaxSourceRows: sourceRows, Artifact: artifact,
		},
		Source: r18dump.Limits{
			MaxInputBytes: sourceBytes, MaxLineBytes: 1 << 20,
			MaxRows: sourceRows, Output: artifact,
		},
	}
}
