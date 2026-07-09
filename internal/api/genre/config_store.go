package genre

import (
	"context"
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
)

// ErrGenreConfigStoreNotConfigured is returned when no runtime/config backs
// the store (noop store writes, or RuntimeGenreConfigStore before runtime
// init). Handlers map it to HTTP 503 to distinguish "not configured" from a
// genuine internal error (500).
var ErrGenreConfigStoreNotConfigured = errors.New("genre config store is not configured")

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

	AddIgnoreGenre(ctx context.Context, genre string) (result []string, changed bool, err error)
	RemoveIgnoreGenre(ctx context.Context, genre string) (result []string, changed bool, err error)
	AddFavoriteGenre(ctx context.Context, genre string) (result []string, changed bool, err error)
	RemoveFavoriteGenre(ctx context.Context, genre string) (result []string, changed bool, err error)
}

// noopGenreConfigStore returns empty lists and no-ops writes. Used when no
// runtime/config is available so the genre endpoints degrade gracefully
// instead of panicking.
type noopGenreConfigStore struct{}

func (noopGenreConfigStore) GetIgnoreGenres(context.Context) ([]string, error) {
	return []string{}, nil
}
func (noopGenreConfigStore) SetIgnoreGenres(context.Context, []string) error {
	return ErrGenreConfigStoreNotConfigured
}
func (noopGenreConfigStore) GetFavoriteGenres(context.Context) ([]string, error) {
	return []string{}, nil
}
func (noopGenreConfigStore) SetFavoriteGenres(context.Context, []string) error {
	return ErrGenreConfigStoreNotConfigured
}
func (noopGenreConfigStore) AddIgnoreGenre(context.Context, string) ([]string, bool, error) {
	return nil, false, ErrGenreConfigStoreNotConfigured
}
func (noopGenreConfigStore) RemoveIgnoreGenre(context.Context, string) ([]string, bool, error) {
	return nil, false, ErrGenreConfigStoreNotConfigured
}
func (noopGenreConfigStore) AddFavoriteGenre(context.Context, string) ([]string, bool, error) {
	return nil, false, ErrGenreConfigStoreNotConfigured
}
func (noopGenreConfigStore) RemoveFavoriteGenre(context.Context, string) ([]string, bool, error) {
	return nil, false, ErrGenreConfigStoreNotConfigured
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
		return fmt.Errorf("%w: runtime is not initialized", ErrGenreConfigStoreNotConfigured)
	}
	if s.rt.GetRuntime() == nil {
		return fmt.Errorf("%w: runtime state is not initialized", ErrGenreConfigStoreNotConfigured)
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

// AddIgnoreGenre atomically appends genre to ignore_genres under ConfigUpdateMu
// so concurrent POST requests cannot lose updates. It returns the resulting
// list and changed=true when the genre was newly added; changed=false means the
// genre was already present (idempotent). The read-modify-write happens inside
// persist, which clones, mutates, saves, and publishes while holding the lock.
func (s *RuntimeGenreConfigStore) AddIgnoreGenre(_ context.Context, genre string) ([]string, bool, error) {
	var result []string
	var changed bool
	err := s.persist(func(cfg *config.Config) {
		current := cfg.Metadata.IgnoreGenres
		if containsString(current, genre) {
			result = cloneStrings(current)
			return
		}
		result = append(cloneStrings(current), genre)
		changed = true
		cfg.Metadata.IgnoreGenres = result
	})
	return result, changed, err
}

// RemoveIgnoreGenre atomically removes genre from ignore_genres under
// ConfigUpdateMu. changed=false means the genre was not present.
func (s *RuntimeGenreConfigStore) RemoveIgnoreGenre(_ context.Context, genre string) ([]string, bool, error) {
	var result []string
	var changed bool
	err := s.persist(func(cfg *config.Config) {
		current := cfg.Metadata.IgnoreGenres
		if !containsString(current, genre) {
			result = cloneStrings(current)
			return
		}
		result = removeString(current, genre)
		changed = true
		cfg.Metadata.IgnoreGenres = result
	})
	return result, changed, err
}

// AddFavoriteGenre atomically appends genre to the favorites list under
// ConfigUpdateMu. changed=false means the genre was already a favorite.
func (s *RuntimeGenreConfigStore) AddFavoriteGenre(_ context.Context, genre string) ([]string, bool, error) {
	var result []string
	var changed bool
	err := s.persist(func(cfg *config.Config) {
		current := cfg.WebUI.Favorites.Genre
		if containsString(current, genre) {
			result = cloneStrings(current)
			return
		}
		result = append(cloneStrings(current), genre)
		changed = true
		cfg.WebUI.Favorites.Genre = result
	})
	return result, changed, err
}

// RemoveFavoriteGenre atomically removes genre from the favorites list under
// ConfigUpdateMu. changed=false means the genre was not a favorite.
func (s *RuntimeGenreConfigStore) RemoveFavoriteGenre(_ context.Context, genre string) ([]string, bool, error) {
	var result []string
	var changed bool
	err := s.persist(func(cfg *config.Config) {
		current := cfg.WebUI.Favorites.Genre
		if !containsString(current, genre) {
			result = cloneStrings(current)
			return
		}
		result = removeString(current, genre)
		changed = true
		cfg.WebUI.Favorites.Genre = result
	})
	return result, changed, err
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
