package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources"
)

const (
	defaultOutput    = "internal/actresscache/data/actresses.json.gz"
	defaultState     = "data/actress-cache/build-state.jsonl"
	defaultUserAgent = "Javinizer-Go-ActressCache/1.0 (+https://github.com/javinizer/javinizer-go)"
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			*s = append(*s, item)
		}
	}
	return nil
}

type parameterMap map[string]string

func (p *parameterMap) String() string {
	keys := make([]string, 0, len(*p))
	for key := range *p {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+(*p)[key])
	}
	return strings.Join(values, ",")
}

func (p *parameterMap) Set(value string) error {
	key, item, ok := strings.Cut(value, "=")
	key, item = strings.TrimSpace(key), strings.TrimSpace(item)
	if !ok || key == "" || item == "" {
		return fmt.Errorf("source option must use key=value")
	}
	if *p == nil {
		*p = make(parameterMap)
	}
	(*p)[key] = item
	return nil
}

type options struct {
	sources           stringList
	output            string
	auditOutput       string
	state             string
	sitemap           string
	r18devDump        string
	legacyCSV         string
	workers           int
	delay             time.Duration
	imageDelay        time.Duration
	timeout           time.Duration
	limit             int
	minDimension      int
	maxImageBytes     int64
	userAgent         string
	refresh           bool
	listSources       bool
	allowPrivateHosts bool
	parameters        parameterMap
}

var exit = os.Exit

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		exit(1)
	}
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	opts := options{
		output:        defaultOutput,
		state:         defaultState,
		workers:       8,
		delay:         250 * time.Millisecond,
		imageDelay:    100 * time.Millisecond,
		timeout:       30 * time.Second,
		minDimension:  64,
		maxImageBytes: 2 << 20,
		userAgent:     defaultUserAgent,
	}
	flags := flag.NewFlagSet("build-actress-cache", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Var(&opts.sources, "source", "Source to crawl; repeat in priority order (highest priority first)")
	flags.Var(&opts.sources, "sources", "Comma-separated sources in priority order")
	flags.Var(&opts.sources, "priority", "Alias for --sources")
	flags.Var(&opts.sources, "source-priority", "Alias for --sources")
	flags.StringVar(&opts.output, "output", opts.output, "Generated compact runtime cache gzip path")
	flags.StringVar(&opts.auditOutput, "audit-output", "", "Optional full audit cache JSON path")
	flags.StringVar(&opts.state, "state", opts.state, "Resumable JSONL state path")
	flags.StringVar(&opts.sitemap, "minnanoav-sitemap", "", "MinnanoAV sitemap index URL")
	flags.StringVar(&opts.r18devDump, "r18dev-dump", "", "Local r18.dev dump SQLite path")
	flags.StringVar(&opts.legacyCSV, "legacy-csv", "", "Original Javinizer jvThumbs.csv path")
	flags.IntVar(&opts.workers, "workers", opts.workers, "Concurrent profile workers")
	flags.DurationVar(&opts.delay, "delay", opts.delay, "Minimum delay between requests to the same host")
	flags.DurationVar(&opts.imageDelay, "image-delay", opts.imageDelay, "Minimum delay between image requests to the same host")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "HTTP request timeout")
	flags.IntVar(&opts.limit, "limit", 0, "Maximum records per source; zero means all")
	flags.IntVar(&opts.minDimension, "min-dimension", opts.minDimension, "Minimum thumbnail width and height")
	flags.Int64Var(&opts.maxImageBytes, "max-image-bytes", opts.maxImageBytes, "Maximum thumbnail response size")
	flags.StringVar(&opts.userAgent, "user-agent", opts.userAgent, "HTTP User-Agent")
	flags.BoolVar(&opts.refresh, "refresh", false, "Re-fetch candidates already successful in state")
	flags.Var(&opts.parameters, "option", "Source-specific option in key=value form; repeat as needed")
	flags.BoolVar(&opts.listSources, "list-sources", false, "List registered sources and exit")
	flags.BoolVar(&opts.allowPrivateHosts, "allow-private-hosts", false, "Allow fetches to loopback/private/link-local hosts (e.g. a local test mirror); off by default to block SSRF")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if len(flags.Args()) != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if opts.workers < 1 {
		return options{}, fmt.Errorf("--workers must be at least 1")
	}
	if opts.delay < 0 {
		return options{}, fmt.Errorf("--delay cannot be negative")
	}
	if opts.imageDelay < 0 {
		return options{}, fmt.Errorf("--image-delay cannot be negative")
	}
	if opts.timeout <= 0 {
		return options{}, fmt.Errorf("--timeout must be positive")
	}
	if opts.limit < 0 {
		return options{}, fmt.Errorf("--limit cannot be negative")
	}
	if opts.minDimension < 0 {
		return options{}, fmt.Errorf("--min-dimension cannot be negative")
	}
	if opts.maxImageBytes <= 0 {
		return options{}, fmt.Errorf("--max-image-bytes must be positive")
	}
	for key := range opts.parameters {
		if !acceptedOptionKeys[key] {
			return options{}, fmt.Errorf("unknown --option key %q (accepted: %s)", key, strings.Join(acceptedOptionKeyList(), ", "))
		}
	}
	return opts, nil
}

// newFetcherWithOptions is the fetcher constructor seam; tests swap it to
// exercise the fetcher-construction error propagation in run().
var newFetcherWithOptions = actresscache.NewFetcherWithOptions

// journalStale is the post-publish journal seam; tests swap it to exercise
// the stale-commit error propagation in run().
var journalStale = actresscache.JournalStale

// acceptedOptionKeys is the complete set of --option keys any cache source
// consumes. Registration rejects typos loudly instead of silently ignoring
// them (e.g. a misspelled legacy.csv would otherwise vanish).
var acceptedOptionKeys = map[string]bool{
	"legacy.csv":        true,
	"jvthumbs.csv":      true,
	"minnanoav.sitemap": true,
	"sitemap":           true,
	"r18dev.dump":       true,
}

// acceptedOptionKeyList returns the accepted keys in stable sorted order.
func acceptedOptionKeyList() []string {
	keys := make([]string, 0, len(acceptedOptionKeys))
	for key := range acceptedOptionKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// absPath is a seam for tests (a dead working directory breaks Abs).
var absPath = filepath.Abs

// artifactKeysFoldCase is a seam so tests can exercise case-insensitive
// collision detection on any host OS.
var artifactKeysFoldCase = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// rejectSharedArtifactPaths refuses runs where --state, --output, or
// --audit-output collide: the atomic writers would otherwise clobber the
// JSONL resume journal with cache data (corrupting/quarantining it next run)
// or silently discard the audit artifact entirely.
func rejectSharedArtifactPaths(opts options) error {
	seen := make(map[string]string, 3)
	// artifactKey normalizes for collision detection; Abs can fail when the
	// working directory is gone -- fall back to lexical cleaning so identical
	// spellings still collide.
	artifactKey := func(path string) string {
		key := ""
		if abs, err := absPath(path); err == nil {
			key = abs
		} else {
			key = filepath.Clean(path)
		}
		// Windows and most macOS volumes are case-insensitive: differently
		// cased spellings then name the SAME artifact, and a positional
		// string match would let the runtime writer clobber the journal.
		if artifactKeysFoldCase {
			key = strings.ToLower(key)
		}
		return key
	}
	for _, artifact := range []struct {
		flag string
		path string
	}{
		{"--state", opts.state},
		{"--output", opts.output},
		{"--audit-output", opts.auditOutput},
	} {
		if strings.TrimSpace(artifact.path) == "" {
			continue
		}
		abs := artifactKey(artifact.path)
		if prev, dup := seen[abs]; dup {
			return fmt.Errorf("%s and %s must name distinct paths (both resolve to %s)", prev, artifact.flag, abs)
		}
		seen[abs] = artifact.flag
	}
	return nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if err := rejectSharedArtifactPaths(opts); err != nil {
		return err
	}
	registry := actresscache.NewRegistry()
	sources.RegisterAll(registry)
	// Resolve the dump path before registration: the documented
	// --option r18dev.dump=PATH form is equivalent to --r18dev-dump PATH,
	// with the explicit flag taking precedence.
	dumpPath := opts.r18devDump
	if dumpPath == "" {
		dumpPath = opts.parameters["r18dev.dump"]
	}
	var dumpStore io.Closer
	if dumpPath != "" {
		var openErr error
		dumpStore, openErr = sources.RegisterR18Dev(registry, dumpPath)
		if openErr != nil {
			return openErr
		}
		defer func() { _ = dumpStore.Close() }()
	}
	if opts.listSources {
		_, _ = fmt.Fprintln(stdout, strings.Join(registry.Names(), "\n"))
		return nil
	}
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return errors.New("default transport is not a *http.Transport")
	}
	transport := defaultTransport.Clone()
	maxConnections := opts.workers * 2
	if maxConnections < 16 {
		maxConnections = 16
	}
	transport.MaxIdleConns = maxConnections * 2
	transport.MaxIdleConnsPerHost = maxConnections
	transport.MaxConnsPerHost = maxConnections
	client := &http.Client{Timeout: opts.timeout, Transport: transport}
	fetcher, err := newFetcherWithOptions(client, opts.delay, opts.userAgent, map[string]time.Duration{
		"pics.dmm.co.jp": opts.imageDelay,
	}, opts.allowPrivateHosts)
	if err != nil {
		return err
	}
	parameters := make(parameterMap, len(opts.parameters)+1)
	for key, value := range opts.parameters {
		parameters[key] = value
	}
	if opts.legacyCSV != "" {
		parameters["legacy.csv"] = opts.legacyCSV
	}
	cache, report, err := actresscache.Build(ctx, actresscache.BuildOptions{
		Registry:              registry,
		Sources:               opts.sources,
		StatePath:             opts.state,
		Refresh:               opts.refresh,
		MinThumbnailDimension: opts.minDimension,
		AllowPrivateHosts:     opts.allowPrivateHosts,
		MaxThumbnailBytes:     opts.maxImageBytes,
		SourceOptions: actresscache.SourceOptions{
			Parameters: parameters,
			Fetcher:    fetcher,
			Limit:      opts.limit,
			Workers:    opts.workers,
			SitemapURL: opts.sitemap,
		},
	})
	if err != nil {
		return err
	}
	if report.Records == 0 {
		// A zero-record build is operator error (header-only CSV, empty dump
		// table). Publishing it would atomically replace the checked-in
		// artifact with an empty cache — and pruning completed sources would
		// already have marked reused state stale. Fail loudly instead.
		return fmt.Errorf("refusing to publish empty actress cache (sources: %s)", strings.Join(opts.sources, ","))
	}
	// The runtime projection is stricter than the audit set: records with no
	// DMM ID, names, or aliases drop out. If everything drops out, the runtime
	// artifact would be empty despite report.Records > 0 — catch that too.
	if len(actresscache.NewRuntimeCache(cache).Records) == 0 {
		return fmt.Errorf("refusing to publish runtime cache with zero projected records (audit built %d records — check source identity fields)", report.Records)
	}

	if opts.auditOutput != "" {
		if err := actresscache.WriteFile(opts.auditOutput, cache); err != nil {
			return fmt.Errorf("write actress audit cache: %w", err)
		}
	}
	if err := actresscache.WriteRuntimeFile(opts.output, cache); err != nil {
		return fmt.Errorf("write actress runtime cache: %w", err)
	}
	// Journal prune decisions ONLY after every artifact landed: ordering the
	// stale commit after publication keeps last-good resume state intact
	// whenever an output write fails.
	if err := journalStale(opts.state, report.StaleKeys); err != nil {
		return fmt.Errorf("journal stale entries: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "sources=%s cached=%d candidates=%d validated=%d rejected=%d failed=%d records=%d output=%s audit_output=%s state=%s\n", strings.Join(report.Sources, ","), report.Cached, report.Candidates, report.Validated, report.Rejected, report.Failed, report.Records, opts.output, opts.auditOutput, opts.state)
	return nil
}
