package config

import (
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func RegisterTestScraperConfigs() {
	scraperutil.ResetAllRegistries()

	scraperPriorities := []struct {
		name     string
		priority int
	}{
		{"r18dev", 100},
		{"libredmm", 95},
		{"dmm", 90},
		{"javlibrary", 80},
		{"javdb", 75},
		{"javbus", 70},
		{"jav321", 65},
		{"mgstage", 55},
		{"tokyohot", 50},
		{"aventertainment", 45},
		{"caribbeancom", 40},
		{"dlgetchu", 40},
		{"fc2", 35},
		{"javstash", 10},
	}
	for _, sp := range scraperPriorities {
		module := &testScraperModule{
			name:     sp.name,
			priority: sp.priority,
			defaults: &ScraperSettings{Enabled: true},
		}
		scraperutil.RegisterModule(module)
	}

	for _, name := range []string{
		"r18dev", "dmm", "libredmm", "mgstage", "javlibrary", "javdb",
		"javbus", "jav321", "tokyohot", "aventertainment", "dlgetchu",
		"caribbeancom", "fc2", "javstash",
	} {
		flattenFn := scraperutil.FlattenFunc(func(a any) any {
			cfg, ok := a.(scraperutil.ScraperConfigInterface)
			if !ok {
				return nil
			}
			sc := &ScraperSettings{}
			sc.Enabled = cfg.IsEnabled()
			sc.RateLimit = cfg.GetRequestDelay()
			sc.RetryCount = cfg.GetMaxRetries()
			sc.UserAgent = cfg.GetUserAgent()
			if p := cfg.GetProxy(); p != nil {
				sc.Proxy = p.(*ProxyConfig)
			}
			if dp := cfg.GetDownloadProxy(); dp != nil {
				sc.DownloadProxy = dp.(*ProxyConfig)
			}
			return sc
		})
		module := &testScraperModule{
			name:        name,
			flattenFunc: flattenFn,
		}
		scraperutil.RegisterModule(module)
	}

	for _, name := range []string{
		"r18dev", "dmm", "libredmm", "mgstage", "javlibrary", "javdb",
		"javbus", "jav321", "tokyohot", "aventertainment", "dlgetchu",
		"caribbeancom", "fc2", "javstash",
	} {
		module := &testScraperModule{
			name:      name,
			validator: scraperutil.ValidatorFunc(func(a any) error { return nil }),
		}
		scraperutil.RegisterModule(module)
	}
}

type testScraperModule struct {
	name        string
	priority    int
	defaults    any
	validator   scraperutil.ValidatorFunc
	flattenFunc scraperutil.FlattenFunc
}

func (m *testScraperModule) Name() string        { return m.name }
func (m *testScraperModule) Description() string { return "Test " + m.name }
func (m *testScraperModule) Constructor() any    { return nil }
func (m *testScraperModule) Validator() any      { return m.validator }
func (m *testScraperModule) ConfigFactory() any  { return nil }
func (m *testScraperModule) Options() any        { return nil }
func (m *testScraperModule) Defaults() any       { return m.defaults }
func (m *testScraperModule) Priority() int       { return m.priority }
func (m *testScraperModule) FlattenFunc() any    { return m.flattenFunc }
