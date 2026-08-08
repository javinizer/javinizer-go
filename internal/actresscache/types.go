package actresscache

import (
	"context"
	"sort"
)

// Candidate ...
type Candidate struct {
	Key          string   `json:"key"`
	Source       string   `json:"source"`
	SourceID     string   `json:"source_id"`
	SourceURL    string   `json:"source_url"`
	DMMID        int      `json:"dmm_id"`
	FirstName    string   `json:"first_name"`
	LastName     string   `json:"last_name"`
	JapaneseName string   `json:"japanese_name"`
	Aliases      []string `json:"aliases,omitempty"`
	ThumbURL     string   `json:"thumb_url"`
}

// ThumbnailValidation ...
type ThumbnailValidation struct {
	CheckedAt string `json:"checked_at"`
	SHA256    string `json:"sha256"`
	Bytes     int    `json:"bytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Format    string `json:"format"`
}

// ValidatedCandidate ...
type ValidatedCandidate struct {
	Candidate Candidate           `json:"candidate"`
	Thumbnail ThumbnailValidation `json:"thumbnail"`
}

// SourceRecord ...
type SourceRecord struct {
	Source       string              `json:"source"`
	SourceID     string              `json:"source_id"`
	SourceURL    string              `json:"source_url"`
	DMMID        int                 `json:"dmm_id"`
	FirstName    string              `json:"first_name"`
	LastName     string              `json:"last_name"`
	JapaneseName string              `json:"japanese_name"`
	Aliases      []string            `json:"aliases,omitempty"`
	ThumbURL     string              `json:"thumb_url"`
	Thumbnail    ThumbnailValidation `json:"thumbnail"`
}

// Record ...
type Record struct {
	BuiltinKey    string              `json:"builtin_key"`
	DMMID         int                 `json:"dmm_id"`
	FirstName     string              `json:"first_name"`
	LastName      string              `json:"last_name"`
	JapaneseName  string              `json:"japanese_name"`
	Aliases       []string            `json:"aliases,omitempty"`
	ThumbURL      string              `json:"thumb_url"`
	Thumbnail     ThumbnailValidation `json:"thumbnail"`
	PrimarySource string              `json:"primary_source"`
	Sources       []SourceRecord      `json:"sources"`
}

// Cache ...
type Cache struct {
	SchemaVersion int      `json:"schema_version"`
	GeneratedAt   string   `json:"generated_at"`
	Sources       []string `json:"sources"`
	Records       []Record `json:"records"`
}

// StateEntry ...
type StateEntry struct {
	Key       string               `json:"key"`
	Status    string               `json:"status"`
	CheckedAt string               `json:"checked_at"`
	Candidate *Candidate           `json:"candidate,omitempty"`
	Thumbnail *ThumbnailValidation `json:"thumbnail,omitempty"`
	// ValidatedWithPrivateHosts records that this entry's thumbnail passed
	// under --allow-private-hosts. The host may be an internal DNS name, so a
	// later default-safe run cannot lexically prove it safe and must
	// revalidate instead of reusing.
	ValidatedWithPrivateHosts bool `json:"private_hosts,omitempty"`
	// Policy fingerprints the validation policy in force when this entry was
	// journaled (min-dimension, max-bytes, private-hosts). Entries rejected
	// under a DIFFERENT policy re-evaluate on the next run instead of being
	// skipped forever.
	Policy string `json:"policy,omitempty"`
	Error  string `json:"error,omitempty"`
}

// SourceOptions ...
type SourceOptions struct {
	Fetcher       *Fetcher
	Limit         int
	Workers       int
	SitemapURL    string
	ShouldSkip    func(key string) bool
	RecordFailure func(Candidate, error) error
	MarkSeen      func(key string)
	MarkComplete  func()
	Parameters    map[string]string
}

// ThumbnailValidator ...
type ThumbnailValidator func(context.Context, Candidate) (ThumbnailValidation, error)

// BuildOptions ...
type BuildOptions struct {
	Registry              *Registry
	Sources               []string
	SourceOptions         SourceOptions
	StatePath             string
	Refresh               bool
	MinThumbnailDimension int
	MaxThumbnailBytes     int64
	// AllowPrivateHosts widens the egress guard for trusted local mirrors.
	// Cached thumbnails fetched under it may carry private URLs, so a
	// default-safe run must not reuse them (see cachedCandidateReusable).
	AllowPrivateHosts bool
	ValidateThumbnail ThumbnailValidator
}

// BuildReport ...
type BuildReport struct {
	Sources    []string `json:"sources"`
	Cached     int      `json:"cached"`
	Candidates int      `json:"candidates"`
	Validated  int      `json:"validated"`
	Rejected   int      `json:"rejected"`
	Failed     int      `json:"failed"`
	Records    int      `json:"records"`
	// StaleKeys carries prune decisions for the caller to commit AFTER the
	// publish step succeeds (see JournalStale); never serialized.
	StaleKeys []string `json:"-"`
}

// SourceFactory ...
type SourceFactory func() Source

// Registry ...
type Registry struct {
	factories map[string]SourceFactory
}

// NewRegistry ...
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]SourceFactory)}
}

// Register ...
func (r *Registry) Register(name string, factory SourceFactory) {
	if r == nil || factory == nil || name == "" {
		return
	}
	r.factories[name] = factory
}

// Create ...
func (r *Registry) Create(name string) (Source, bool) {
	if r == nil {
		return nil, false
	}
	factory, ok := r.factories[name]
	if !ok {
		return nil, false
	}
	return factory(), true
}

// Names ...
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Source ...
type Source interface {
	Name() string
	Collect(context.Context, SourceOptions, func(Candidate) error) error
}
