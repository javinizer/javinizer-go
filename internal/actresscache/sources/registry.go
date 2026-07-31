package sources

import (
	"github.com/javinizer/javinizer-go/internal/actresscache"
	legacycsvsource "github.com/javinizer/javinizer-go/internal/actresscache/sources/legacycsv"
	minnanoavsource "github.com/javinizer/javinizer-go/internal/actresscache/sources/minnanoav"
	r18devsource "github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dev"
)

// RegisterAll ...
func RegisterAll(registry *actresscache.Registry) {
	registry.Register("legacy-jvthumbs", legacycsvsource.New)
	registry.Register("minnanoav", minnanoavsource.New)
	registry.Register("r18dev", r18devsource.New)
}
