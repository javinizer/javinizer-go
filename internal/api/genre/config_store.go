package genre

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
)

// GenreConfigStore provides read/write access to the config-backed genre lists
// managed from the Genres page: ignore_genres (excluded from scraping) and
// favorite genres (the quick-apply list). Implementations must persist changes
// to the config file and publish them to the running config so the aggregator
// and UI observe them without a server restart.
type GenreConfigStore interface {
	GetIgnoreGenres(ctx context.Context) ([]string, error)
	SetIgnoreGenres(ctx context.Context, genres []string) error
	GetFavoriteGenres(ctx context.Context) ([]string, error)
	SetFavoriteGenres(ctx context.Context, genres []string) error
}

// noopGenreConfigStore returns empty lists and no-ops writes. Used when no
// runtime/config is available so the genre endpoints degrade gracefully
// instead of panicking.
type noopGenreConfigStore struct{}

func (noopGenreConfigStore) GetIgnoreGenres(context.Context) ([]string, error) {
	return []string{}, nil
}
func (noopGenreConfigStore) SetIgnoreGenres(context.Context, []string) error {
	return fmt.Errorf("genre config store is not configured")
}
func (noopGenreConfigStore) GetFavoriteGenres(context.Context) ([]string, error) {
	return []string{}, nil
}
func (noopGenreConfigStore) SetFavoriteGenres(context.Context, []string) error {
	return fmt.Errorf("genre config store is not configured")
}

// RuntimeGenreConfigStore is the production GenreConfigStore. It persists
// changes by mutating a clone of the in-memory config under the runtime's
// ConfigUpdateMu (the same lock the full config endpoint uses, preventing
// read-modify-publish races with concurrent full-config saves), writing the
// YAML file, and publishing the updated config atomically via SetConfig so the
// next scrape picks up new ignore_genres.
type RuntimeGenreConfigStore struct {
	rt         *core.APIRuntime
	configFile string
}

// NewRuntimeGenreConfigStore binds a store to the given runtime and config file.
func NewRuntimeGenreConfigStore(rt *core.APIRuntime, configFile string) *RuntimeGenreConfigStore {
	return &RuntimeGenreConfigStore{rt: rt, configFile: configFile}
}

func (s *RuntimeGenreConfigStore) current() *config.Config {
	return s.rt.Deps().CoreDeps.GetConfig()
}

// requireRuntime guards getters against a nil runtime/runtime-state, mirroring
// persist() so failures surface as errors instead of panics.
func (s *RuntimeGenreConfigStore) requireRuntime() error {
	if s.rt == nil {
		return fmt.Errorf("genre config store: runtime is not initialized")
	}
	if s.rt.GetRuntime() == nil {
		return fmt.Errorf("genre config store: runtime state is not initialized")
	}
	return nil
}

// GetIgnoreGenres returns the current ignore_genres list from the live config.
func (s *RuntimeGenreConfigStore) GetIgnoreGenres(_ context.Context) ([]string, error) {
	if err := s.requireRuntime(); err != nil {
		return nil, err
	}
	return cloneStrings(s.current().Metadata.IgnoreGenres), nil
}

// SetIgnoreGenres persists a new ignore_genres list and publishes it to the live config.
func (s *RuntimeGenreConfigStore) SetIgnoreGenres(ctx context.Context, genres []string) error {
	return s.persist(func(cfg *config.Config) {
		cfg.Metadata.IgnoreGenres = genres
	})
}

// GetFavoriteGenres returns the current favorite genres list from the live config.
func (s *RuntimeGenreConfigStore) GetFavoriteGenres(_ context.Context) ([]string, error) {
	if err := s.requireRuntime(); err != nil {
		return nil, err
	}
	return cloneStrings(s.current().WebUI.Favorites.Genre), nil
}

// SetFavoriteGenres persists a new favorites list and publishes it to the live config.
func (s *RuntimeGenreConfigStore) SetFavoriteGenres(ctx context.Context, genres []string) error {
	return s.persist(func(cfg *config.Config) {
		cfg.WebUI.Favorites.Genre = genres
	})
}

// persist clones the live config, applies mutate, writes the YAML file, and
// publishes the result. The clone-and-mutate-then-publish flow keeps the
// in-memory config and on-disk config consistent: the published pointer is
// the same one written to disk, so concurrent GetConfig readers cannot observe
// a config that diverges from the file. ConfigUpdateMu serializes this against
// full-config PUT saves.
func (s *RuntimeGenreConfigStore) persist(mutate func(*config.Config)) error {
	if err := s.requireRuntime(); err != nil {
		return err
	}
	rs := s.rt.GetRuntime()
	rs.ConfigUpdateMu.Lock()
	defer rs.ConfigUpdateMu.Unlock()

	cfg := s.current().Clone()
	mutate(cfg)

	if err := config.Save(cfg, s.configFile); err != nil {
		return fmt.Errorf("failed to save genre config: %w", err)
	}
	s.rt.SetConfig(cfg)
	return nil
}

func cloneStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	cp := make([]string, len(s))
	copy(cp, s)
	return cp
}
