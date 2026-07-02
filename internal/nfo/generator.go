package nfo

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

const defaultNFOFileName = "metadata"

// Generator creates NFO files from movie metadata
type Generator struct {
	fs             afero.Fs
	templateEngine template.EngineInterface
	config         *Config
	mediaAnalyzer  mediaAnalyzer
	encodeFunc     func(io.Writer, *Movie) error
}

// NewGenerator returns an NFO generator that writes to fs using the given config.
func NewGenerator(fs afero.Fs, cfg *Config) *Generator {
	return newGeneratorWithAnalyzer(fs, cfg, nil)
}

// newGeneratorWithAnalyzer creates a new NFO generator with an optional media analyzer override.
func newGeneratorWithAnalyzer(fs afero.Fs, cfg *Config, ma mediaAnalyzer) *Generator {
	if cfg == nil {
		cfg = defaultConfig()
	}

	// Copy config to prevent caller mutation
	cfgCopy := *cfg

	// Ensure defaults
	if cfgCopy.UnknownActressText == "" {
		cfgCopy.UnknownActressText = "Unknown"
	}
	if cfgCopy.UnknownActressMode == "" {
		cfgCopy.UnknownActressMode = models.UnknownActressModeSkip
	}
	if cfgCopy.FilenameTemplate == "" {
		cfgCopy.FilenameTemplate = "<ID>.nfo"
	}

	// Use injected template engine with nil-guard fallback
	engine := cfgCopy.TemplateEngine
	if engine == nil {
		engine = template.NewEngine()
	}

	if ma == nil {
		ma = defaultMediaAnalyzer{}
	}

	return &Generator{
		fs:             fs,
		templateEngine: engine,
		config:         &cfgCopy,
		mediaAnalyzer:  ma,
		encodeFunc:     writeNFOXML,
	}
}

// defaultConfig returns default NFO generation settings
func defaultConfig() *Config {
	return &Config{
		FirstNameOrder:     true,
		UnknownActressText: "Unknown",
		UnknownActressMode: models.UnknownActressModeSkip,
		FilenameTemplate:   "<ID>.nfo",
		IncludeFanart:      true,
		IncludeTrailer:     true,
		RatingSource:       "themoviedb",
	}
}

// NFOFieldMerger is the focused interface for NFO filename and path resolution.
// Compare orchestrator and revert log use this to resolve NFO paths through
// the seam instead of reaching into the nfo package directly.
// Per the nfoImplementor refactoring: ParseNFO and MergeMovieMetadataWithOptions
// are now package-level pure functions — no longer on any interface.
type NFOFieldMerger interface {
	ResolveNFOFilename(movie *models.Movie, cfg NFONameConfig) string

	// ResolveNFOPath builds the expected NFO file path and a list of legacy
	// paths to check for backward compatibility. this method
	// exists on NFOFieldMerger so that revert callers resolve NFO paths
	// through the seam instead of reaching into the nfo package directly.
	ResolveNFOPath(baseDir string, movie *models.Movie, cfg NFONameConfig, videoFilePath string) (nfoPath string, legacyPaths []string)
}

// NFOFileMerger is the focused interface for filesystem-aware NFO merge.
// Apply orchestrator uses this to merge with an existing NFO file on disk.
type NFOFileMerger interface {
	MergeWithExistingNFO(movie *models.Movie, opts MergeWithExistingOptions) MergeWithExistingResult
}

// NFOInterface combines NFO filename/path resolution and filesystem-aware
// merge into a single seam. Per the nfoImplementor refactoring: ParseNFO and
// MergeMovieMetadataWithOptions are now package-level pure functions — callers
// invoke them directly instead of through an interface.
// New callers should depend on the narrowest sub-interface they need
// (NFOFieldMerger or NFOFileMerger).
type NFOInterface interface {
	NFOFieldMerger
	NFOFileMerger
}

// nfoImplementor is an unexported struct that satisfies NFOInterface.
// Per the nfoImplementor refactoring: ParseNFO and MergeMovieMetadataWithOptions
// are now package-level pure functions — callers invoke them directly.
// nfoImplementor carries infrastructure dependencies (fs, nfoConfig,
// templateEngine) so MergeWithExistingNFO callers pass domain data only.
type nfoImplementor struct {
	fs             afero.Fs
	nfoConfig      *Config
	templateEngine template.EngineInterface
}

var _ NFOInterface = (*nfoImplementor)(nil)

// ResolveNFOFilename resolves the NFO filename for a movie using the implementor's
// template engine. this method exists on NFOFieldMerger so that
// preview and revert callers resolve NFO paths through the seam instead of
// reaching into the nfo package directly.
func (n nfoImplementor) ResolveNFOFilename(movie *models.Movie, cfg NFONameConfig) string {
	return ResolveNFOFilename(n.templateEngine, movie, cfg)
}

// ResolveNFOPath builds the expected NFO file path and legacy paths using the
// implementor's template engine. this method exists on
// NFOFieldMerger so that revert callers resolve NFO paths through the seam
// instead of reaching into the nfo package directly.
func (n nfoImplementor) ResolveNFOPath(baseDir string, movie *models.Movie, cfg NFONameConfig, videoFilePath string) (string, []string) {
	return resolveNFOPath(baseDir, movie, cfg, videoFilePath, n.templateEngine)
}

// NewNFOImplementor creates the canonical NFOInterface implementation with
// infrastructure dependencies. fs, nfoConfig, and templateEngine
// are owned by the implementor, not threaded through per-call.
func NewNFOImplementor(fs afero.Fs, nfoConfig *Config, templateEngine template.EngineInterface) NFOInterface {
	return nfoImplementor{fs: fs, nfoConfig: nfoConfig, templateEngine: templateEngine}
}

// GeneratorInterface describes the operations for generating NFO files from movie metadata.
type GeneratorInterface interface {
	Generate(ctx context.Context, movie *models.Movie, outputPath string, partSuffix string, videoFilePath string, tags []string) error
	GenerateAtPath(ctx context.Context, movie *models.Movie, nfoPath string, videoFilePath string, tags []string) error

	// ResolveAndGenerate resolves the NFO filename from the template, validates the
	// template, builds the full path, and generates the NFO file in the destination
	// directory. Returns the NFO path on success, or ("", nil) if NFO generation was
	// skipped (e.g. broken template). Returns ("", error) on generation failure.
	// Per architecture deepening: this encapsulates the full NFO write path so that
	// callers (the apply orchestrator) don't need to reach into NFO internals for
	// template validation, filename resolution, and path construction.
	ResolveAndGenerate(ctx context.Context, movie *models.Movie, destDir string, nameCfg NFONameConfig, videoFilePath string, tags []string) (nfoPath string, err error)
}

var _ GeneratorInterface = (*Generator)(nil)

// Generate creates an NFO file from a Movie model
// partSuffix: optional suffix for multi-part files (e.g., "-pt1", "-A")
// videoFilePath: optional path to video file for extracting stream details (empty string to skip)
// tags: pre-resolved tags from caller (e.g., tag database) — replaces internal DB call
func (g *Generator) Generate(ctx context.Context, movie *models.Movie, outputPath string, partSuffix string, videoFilePath string, tags []string) error {
	// Compute the NFO filename using the same logic as ResolveNFOFilename,
	// but fail on template errors (Generate is the write path — broken templates
	// should be reported, not silently fallen back).
	tmplCtx := template.NewContextFromMovieWithOptions(movie, template.ContextOptions{
		FirstNameOrder: g.config.FirstNameOrder,
	})
	tmplCtx.GroupActress = g.config.GroupActress
	tmplCtx.GroupActressName = g.config.GroupActressName
	tmplCtx.GroupUnknownActressName = g.config.GroupUnknownActressName
	tmplCtx.ActressLanguageJa = g.config.ActressLanguageJA
	tmplCtx.ActressDelimiter = g.config.ActressDelimiter
	filename, err := g.templateEngine.Execute(g.config.FilenameTemplate, tmplCtx)
	if err != nil {
		return fmt.Errorf("failed to generate NFO filename: %w", err)
	}

	// Sanitize filename
	filename = template.SanitizeFilename(filename)
	if filename == "" {
		filename = template.SanitizeFilename(movie.ID)
		if filename == "" {
			filename = defaultNFOFileName
		}
	}

	// Remove .nfo extension if present (we'll add it back at the end)
	if strings.HasSuffix(strings.ToLower(filename), ".nfo") {
		filename = filename[:len(filename)-4]
	} else if strings.EqualFold(filename, "nfo") {
		filename = ""
	}

	// After stripping .nfo, filename may be empty (e.g., template produced only ".nfo")
	if filename == "" {
		filename = template.SanitizeFilename(movie.ID)
		if filename == "" {
			filename = defaultNFOFileName
		}
	}

	// Append part suffix before extension (if provided and per_file is enabled)
	if partSuffix != "" && g.config.PerFile {
		filename = filename + partSuffix
	}

	// Ensure .nfo extension
	filename += ".nfo"

	fullPath := filepath.Join(outputPath, filename)
	return g.GenerateAtPath(ctx, movie, fullPath, videoFilePath, tags)
}

// GenerateAtPath creates an NFO file at the specified path without computing
// the filename. Callers that have already resolved the NFO path (e.g., via
// ResolveNFOFilename) should use this method to avoid dual path computation
// and ensure the revert log and generator agree on the target path.
func (g *Generator) GenerateAtPath(ctx context.Context, movie *models.Movie, nfoPath string, videoFilePath string, tags []string) error {
	nfo := g.movieToNFO(ctx, movie, videoFilePath, tags)
	return g.WriteNFO(nfo, nfoPath)
}

// ResolveAndGenerate encapsulates the full NFO write path: template validation,
// filename resolution, path construction, and generation. Per architecture deepening:
// callers don't need to reach into NFO internals for template validation, filename
// resolution, or path construction. Returns the NFO path on success, or ("", nil)
// if NFO generation was skipped (e.g. broken template).
func (g *Generator) ResolveAndGenerate(ctx context.Context, movie *models.Movie, destDir string, nameCfg NFONameConfig, videoFilePath string, tags []string) (string, error) {
	// Validate the template before resolving the filename — broken templates
	// must skip NFO generation rather than silently falling back to movie.ID.nfo.
	tmplCtx := template.NewContextFromMovieWithOptions(movie, template.ContextOptions{
		FirstNameOrder: nameCfg.FirstNameOrder,
	})
	tmplCtx.GroupActress = nameCfg.GroupActress
	tmplCtx.GroupActressName = nameCfg.GroupActressName
	if _, tmplErr := g.templateEngine.Execute(nameCfg.FilenameTemplate, tmplCtx); tmplErr != nil {
		return "", nil //nolint:nilerr // intentional: broken templates skip NFO generation, not an error
	}

	nfoFilename := ResolveNFOFilename(g.templateEngine, movie, nameCfg)
	nfoPath := filepath.ToSlash(filepath.Join(destDir, nfoFilename))

	if genErr := g.GenerateAtPath(ctx, movie, nfoPath, videoFilePath, tags); genErr != nil {
		return "", genErr
	}
	return nfoPath, nil
}

// movieToNFO converts a Movie model to NFO format.
// videoFilePath: optional path to video file for extracting stream details (empty string to skip)
// tags: pre-resolved tags from caller (e.g., tag database) — replaces internal DB call
func (g *Generator) movieToNFO(ctx context.Context, movie *models.Movie, videoFilePath string, tags []string) *Movie {
	input := g.transformMovieForNFO(ctx, movie, videoFilePath, tags)
	return g.buildNFO(input)
}

// WriteNFO encodes the NFO struct to the given path, creating parent directories as needed.
func (g *Generator) WriteNFO(nfo *Movie, path string) error {
	// Ensure output directory exists
	dir := filepath.Dir(path)
	if err := g.fs.MkdirAll(dir, config.DirPerm); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Encode to an in-memory buffer first so we can post-process the XML.
	// Go's encoding/xml escapes " and ' to numeric character references
	// (&#34; and &#39;) in both element text and attribute values. This is
	// valid XML, but NFO consumers like Jellyfin/Emby display them literally
	// in element text instead of decoding them.
	//
	// Only <, >, and & need escaping in XML element text — " and ' are valid
	// unescaped there (they're only required inside attribute values). We
	// therefore reverse the " / ' escaping, but ONLY in element text content.
	// Unescaping inside tags would corrupt attribute values (e.g.
	// name="Bob&#34;s" would become name="Bob"s" — broken XML). The
	// unescapeQuotesInText helper walks the buffer with a tag-aware state
	// machine so attribute values are left untouched.
	var buf bytes.Buffer
	if err := g.encodeFunc(&buf, nfo); err != nil {
		return err
	}
	output := unescapeQuotesInText(buf.String())

	file, err := g.fs.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create NFO file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if _, err := file.WriteString(output); err != nil {
		return fmt.Errorf("failed to write NFO file: %w", err)
	}

	return nil
}

// writeNFOXML encodes an NFO Movie to the given writer as indented XML with a
// leading header and trailing newline. It is separated from WriteNFO so the
// encoding error paths can be exercised directly in tests with a failing
// writer.
func writeNFOXML(w io.Writer, nfo *Movie) error {
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return fmt.Errorf("failed to write XML header: %w", err)
	}
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	if err := encoder.Encode(nfo); err != nil {
		return fmt.Errorf("failed to encode NFO: %w", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write final newline: %w", err)
	}
	return nil
}

// unescapeQuotesInText reverses Go's encoding/xml escaping of " and ' to
// numeric character references (&#34; and &#39;) but ONLY in element text
// content — never inside tags or attribute values, where those characters
// must remain escaped to keep the XML well-formed. It walks the buffer with a
// tag-aware state machine: text between '>' and '<' is unescaped, while
// everything inside '<...>' (including attribute values) is left untouched.
// Quote tracking inside tags ensures a '>' appearing within a quoted
// attribute value does not prematurely end the tag.
func unescapeQuotesInText(s string) string {
	const (
		stText = iota
		stTag
	)
	const (
		encQuote = "&#34;"
		encApos  = "&#39;"
	)
	state := stText
	var quote byte
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch state {
		case stText:
			if c == '<' {
				b.WriteByte(c)
				state = stTag
				quote = 0
				i++
			} else if c == '&' && i+len(encQuote) <= len(s) && s[i:i+len(encQuote)] == encQuote {
				b.WriteByte('"')
				i += len(encQuote)
			} else if c == '&' && i+len(encApos) <= len(s) && s[i:i+len(encApos)] == encApos {
				b.WriteByte('\'')
				i += len(encApos)
			} else {
				b.WriteByte(c)
				i++
			}
		case stTag:
			if quote != 0 {
				b.WriteByte(c)
				if c == quote {
					quote = 0
				}
				i++
			} else if c == '"' || c == '\'' {
				quote = c
				b.WriteByte(c)
				i++
			} else if c == '>' {
				b.WriteByte(c)
				state = stText
				i++
			} else {
				b.WriteByte(c)
				i++
			}
		}
	}
	return b.String()
}

// extractStreamDetails extracts video/audio stream information from a video file
func (g *Generator) extractStreamDetails(ctx context.Context, videoFilePath string) *streamDetails {
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	details, err := g.mediaAnalyzer.Analyze(ctx, videoFilePath)
	if err != nil {
		// Surface extraction failures so they are distinguishable from "no stream
		// details configured". Skip logging for context cancellation, which is
		// expected during shutdown.
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logging.Warnf("failed to extract stream details for %q: %v", filepath.Base(videoFilePath), err)
		}
		return nil
	}
	return details
}

// ResolveNFOFilename computes the NFO filename for a movie using the same logic
// as Generate, without writing the file. On template execution error, falls back
// to movie.ID.nfo (unlike Generate, which returns the error). This ensures that
// history/revert code always gets a usable path even if the template is broken.
// The engine parameter allows injection of a shared template engine with language
// config; if nil, a default engine is created.
func ResolveNFOFilename(engine template.EngineInterface, movie *models.Movie, cfg NFONameConfig) string {
	if engine == nil {
		engine = template.NewEngine()
	}
	tmplCtx := template.NewContextFromMovieWithOptions(movie, template.ContextOptions{
		FirstNameOrder: cfg.FirstNameOrder,
	})
	tmplCtx.GroupActress = cfg.GroupActress
	tmplCtx.GroupActressName = cfg.GroupActressName
	tmplCtx.GroupUnknownActressName = cfg.GroupUnknownActressName
	tmplCtx.ActressLanguageJa = cfg.ActressLanguageJA
	tmplCtx.ActressDelimiter = cfg.ActressDelimiter
	filename, err := engine.Execute(cfg.FilenameTemplate, tmplCtx)
	if err != nil {
		sanitized := template.SanitizeFilename(movie.ID)
		if sanitized == "" {
			sanitized = defaultNFOFileName
		}
		return sanitized + ".nfo"
	}

	// Sanitize FIRST (matching Generate's order), then strip .nfo
	filename = template.SanitizeFilename(filename)
	if filename == "" {
		filename = template.SanitizeFilename(movie.ID)
		if filename == "" {
			filename = defaultNFOFileName
		}
	}

	// Remove .nfo extension if present (we'll add it back at the end)
	if strings.HasSuffix(strings.ToLower(filename), ".nfo") {
		filename = filename[:len(filename)-4]
	} else if strings.EqualFold(filename, "nfo") {
		filename = ""
	}

	// After stripping .nfo, filename may be empty (e.g., template produced only ".nfo")
	if filename == "" {
		filename = template.SanitizeFilename(movie.ID)
		if filename == "" {
			filename = defaultNFOFileName
		}
	}

	if cfg.PartSuffix != "" && cfg.PerFile {
		filename += cfg.PartSuffix
	}
	return filename + ".nfo"
}
