package catalogname

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOwnerUnionContract(t *testing.T) {
	base := trusted(0, "Mary Jane", "Van Dyke")
	other := trusted(2, "Other", "Person")
	other.verified = false
	for _, aliasCount := range []int{0, 1, 2, 17} {
		t.Run(fmt.Sprintf("aliases_%d", aliasCount), func(t *testing.T) {
			aliases := make([]alias, aliasCount)
			for i := range aliases {
				aliases[i] = alias{"Van Dyke Mary Jane", "Mary Jane Van Dyke"}
			}
			for _, reversed := range []bool{false, true} {
				t.Run(fmt.Sprintf("reversed_%t", reversed), func(t *testing.T) {
					for _, blocking := range []bool{false, true} {
						t.Run(fmt.Sprintf("blocking_%t", blocking), func(t *testing.T) {
							records := []record{base, other}
							current := append([]alias(nil), aliases...)
							if blocking {
								current = append(current, alias{"Mary Jane Van Dyke", "Other Person"})
							}
							if reversed {
								records[0], records[1] = records[1], records[0]
								for i, j := 0, len(current)-1; i < j; i, j = i+1, j-1 {
									current[i], current[j] = current[j], current[i]
								}
							}
							snapshot := newSnapshot(records, current)
							want := Decision{Resolved: true, FirstName: base.first, LastName: base.last, CatalogID: base.id, Confidence: "catalog_consistent", Reason: "matched"}
							if blocking {
								want = Decision{Confidence: "none", Reason: "ambiguous"}
							}
							for range 20 {
								require.Equal(t, want, snapshot.Resolve("Van Dyke Mary Jane"))
							}
						})
					}
				})
			}
		})
	}
}

func TestResolveSnapshotConcurrentReaders(t *testing.T) {
	base := trusted(0, "Yui", "Hatano")
	snapshot := newSnapshot([]record{base}, []alias{{"Yui Hatano", "Hatano Yui"}, {"Hatano Yui", "Yui Hatano"}})
	matched := Decision{Resolved: true, FirstName: "Yui", LastName: "Hatano", Confidence: "catalog_consistent", Reason: "matched"}
	unknown := Decision{Confidence: "none", Reason: "unknown"}
	var readers sync.WaitGroup
	for range 32 {
		readers.Go(func() {
			for range 20 {
				for _, display := range []string{"Yui Hatano", "Hatano Yui", "Missing Name", " "} {
					want := matched
					if display == "Missing Name" || display == " " {
						want = unknown
					}
					if got := snapshot.Resolve(display); got != want {
						t.Errorf("Resolve(%q) = %#v, want %#v", display, got, want)
					}
				}
			}
		})
	}
	readers.Wait()
}
