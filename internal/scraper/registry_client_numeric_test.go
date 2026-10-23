package scraper

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestNumericPresenceAllRegisteredClients(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	RegisterAll(reg)
	for name, module := range reg.GetAll() {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				input   string
				retries int
			}{
				{`{}`, 3}, {`{"retry_count":null}`, 3},
				{`{"retry_count":0,"rate_limit":0,"timeout":0}`, 0},
				{`{"max_retries":0,"request_delay":0}`, 0}, {`{"retry_count":2}`, 2},
			} {
				t.Run(tc.input, func(t *testing.T) {
					var settings models.ScraperSettings
					require.NoError(t, json.Unmarshal([]byte(tc.input), &settings))
					settings.MergeDefaultsFrom(module.Defaults)
					instance, err := module.Constructor(scraperutil.ScraperDeps{Settings: settings, TimeoutSeconds: 47})
					require.NoError(t, err)
					if closer, ok := instance.(interface{ Close() error }); ok {
						t.Cleanup(func() { require.NoError(t, closer.Close()) })
					}
					// Inspect the actual module-owned Resty client without adding production
					// accessors or unsafe access just for tests. Int reads need no Interface conversion.
					client := reflect.ValueOf(instance).Elem().FieldByName("client")
					require.True(t, client.IsValid(), "every registered HTTP scraper must expose its client to this contract")
					require.False(t, client.IsNil())
					require.EqualValues(t, tc.retries, client.Elem().FieldByName("RetryCount").Int())
				})
			}
		})
	}
}
