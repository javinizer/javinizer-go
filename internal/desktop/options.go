package desktop

// Options configures the desktop app launcher.
type Options struct {
	// ConfigFile is the config path. When empty or equal to the CLI default
	// ("configs/config.yaml"), the desktop launcher falls back to a portable
	// user-data path so the app works when launched from Finder/Explorer
	// (where CWD is not the repo).
	ConfigFile string
}
