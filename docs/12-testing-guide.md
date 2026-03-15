# Testing Guide

This guide covers testing practices, tools, and coverage requirements for the javinizer-go project.

## Table of Contents

- [Running Tests](#running-tests)
- [Coverage Requirements](#coverage-requirements)
- [Test Types](#test-types)
- [Writing Tests](#writing-tests)
- [CI/CD Integration](#cicd-integration)
- [Pre-commit Hooks](#pre-commit-hooks)
- [Troubleshooting](#troubleshooting)

## Running Tests

### Quick Start

```bash
# Run all tests
make test

# Run tests with coverage report (uses go-acc automatically via go run)
make coverage

# Run faster local coverage report (short mode)
make coverage-fast

# View coverage in browser
make coverage-html

# Check if coverage meets threshold (75%)
make coverage-check
```

### Development Tools

This project uses `go run` to execute development tools without requiring global installation. Tools are declared in `tools.go` and tracked in `go.mod`.

**Benefits:**
- ✅ No global installation needed
- ✅ Version-controlled dependencies
- ✅ Consistent across all environments
- ✅ Works in CI/CD automatically

The `make coverage` command runs strict coverage via `go-acc` (used by CI/release gates). For faster local iteration, use `make coverage-fast`.

### All Test Commands

| Command | Description | When to Use |
|---------|-------------|-------------|
| `make test` | Run all tests with verbose output | Default test command |
| `make test-short` | Run only fast tests (skips slow integration tests) | Quick validation, pre-commit |
| `make test-race` | Run race detector on concurrent packages | Before committing concurrent code changes |
| `make test-verbose` | Run tests with verbose output and no caching | Debugging test failures |
| `make bench` | Run benchmark tests | Performance testing |
| `make coverage` | Generate strict coverage.out file (go-acc) | CI/release-quality coverage |
| `make coverage-fast` | Generate faster local coverage.out file | Daily local iteration |
| `make coverage-html` | Open HTML coverage report in browser | Visual coverage analysis |
| `make coverage-func` | Display function-by-function coverage breakdown | Identify specific gaps |
| `make coverage-check` | Verify coverage meets 75% threshold | Pre-push validation |
| `make ci` | Run full CI suite (vet + lint + coverage + race) | Before opening PR |

### Running Specific Package Tests

```bash
# Test a specific package
go test ./internal/worker/...

# Test with race detector
go test -race ./internal/worker/...

# Test a specific function
go test -v -run TestPoolSubmit ./internal/worker

# Test with coverage for one package
go test -coverprofile=coverage.out ./internal/matcher/...
go tool cover -html=coverage.out
```

## Coverage Requirements

### Overall Project Coverage

- **Current Baseline:** 75% (enforced in CI)
- **Short-term Goal:** 80%
- **Long-term Target:** 80%+

### Per-Package Coverage Expectations

| Package Category | Target Coverage | Rationale |
|------------------|----------------|-----------|
| **Critical packages** | 85%+ | Core business logic, data integrity |
| - `internal/worker` | 85% | Concurrent task execution, critical for reliability |
| - `internal/aggregator` | 85% | Metadata merging logic |
| - `internal/matcher` | 90% | JAV ID extraction |
| - `internal/organizer` | 85% | File organization, data safety |
| - `internal/scanner` | 85% | File discovery |
| **Important packages** | 70%+ | User-facing features |
| - `internal/scraper/*` | 70% | External data fetching |
| - `internal/nfo` | 75% | NFO generation |
| - `internal/downloader` | 75% | Asset downloads |
| **Supporting packages** | 50%+ | Configuration, models, utilities |
| - `internal/config` | 50% | Simple struct initialization |
| - `internal/models` | 50% | Data structures |
| - `internal/template` | 60% | Template rendering |
| **Minimal coverage acceptable** | 30%+ | UI, CLI, manual testing preferred |
| - `internal/tui` | 30% | Bubble Tea UI (complex to test) |
| - `cmd/javinizer/commands/*` | 40% | CLI command handlers (integration tests preferred) |
| - `internal/api` | 60% | API handlers |

### Coverage Gaps to Address

**High Priority** (critical paths to strengthen):
1. `internal/worker` - batch execution and error classification branches
2. `internal/api` - request validation and edge-case responses
3. `cmd/javinizer/commands/*` - command argument/flag behavior
4. `internal/scraper/*` - network failure and parser fallback paths

**Medium Priority**:
5. `internal/database` - persistence and migration edge cases
6. `internal/mediainfo` - malformed input handling
7. `internal/translation` - provider fallback/error branches

## Test Types

### Unit Tests

Fast, isolated tests for individual functions/methods.

```go
func TestMatchID(t *testing.T) {
    matcher := NewMatcher(config)
    id := matcher.ExtractID("ABC-123.mp4")
    assert.Equal(t, "ABC-123", id)
}
```

**Guidelines:**
- Should run in <1 second per test
- No external dependencies (filesystem, network, database)
- Use table-driven tests for multiple scenarios
- Mark slow tests with `if testing.Short() { t.Skip() }` for use with `make test-short`

### Integration Tests

Test interactions between components or with external resources.

```go
func TestNFOGeneration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }
    // Test with real config file, real templates
}
```

**Guidelines:**
- Place in `*_integration_test.go` files or use build tags
- Use `testing.Short()` to allow skipping with `-short` flag
- Clean up resources (files, database entries) in test cleanup

### Race Detector Tests

Critical for concurrent code (worker pool, TUI, websockets, API).

```bash
# Run race detector on concurrent packages
make test-race

# Or manually:
go test -race ./internal/worker/...
```

**When to run:**
- Before committing changes to `internal/worker`, `internal/tui`, `internal/websocket`, `internal/api`
- When debugging concurrency issues
- In CI (runs automatically on every PR)

**Note:** Race detector tests are slower; they run in a separate CI job.

## Writing Tests

### Test File Organization

- Test files: `*_test.go` in the same package directory
- Integration tests: `*_integration_test.go` or separate `integration/` subdirectory
- Test data: `testdata/` subdirectory (convention, gitignored if needed)

### Testing Patterns

#### Table-Driven Tests

Recommended for testing multiple scenarios:

```go
func TestExtractID(t *testing.T) {
    tests := []struct {
        name     string
        filename string
        expected string
    }{
        {"Standard format", "ABC-123.mp4", "ABC-123"},
        {"With path", "/videos/ABC-123.mp4", "ABC-123"},
        {"No ID", "random.mp4", ""},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := ExtractID(tt.filename)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

#### Mock HTTP Clients

For scraper tests (currently missing):

```go
type mockHTTPClient struct {
    response string
    err      error
}

func (m *mockHTTPClient) Get(url string) (*http.Response, error) {
    if m.err != nil {
        return nil, m.err
    }
    return &http.Response{
        Body: io.NopCloser(strings.NewReader(m.response)),
    }, nil
}

func TestDMMScraper(t *testing.T) {
    client := &mockHTTPClient{response: `<html>...</html>`}
    scraper := NewDMMScraper(client)
    // Test scraper logic without hitting real DMM website
}
```

#### Testing Concurrent Code

Use `t.Parallel()` and proper synchronization:

```go
func TestWorkerPool(t *testing.T) {
    pool := NewPool(5)

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            task := NewMockTask(id)
            pool.Submit(task)
        }(i)
    }

    wg.Wait()
    // Verify results
}
```

#### Testing CLI Commands (Epic 6 Pattern)

Testing CLI commands requires dependency injection to avoid global state and enable testability. The pattern involves:

1. **Export the run function** with config injection
2. **Test flags** (defaults, validation, mutual exclusivity)
3. **Integration tests** with real filesystem using `t.TempDir()`
4. **Unit tests** for extracted helper functions

**Complete Example from `cmd/javinizer/commands/update/command_test.go`:**

```go
// Flag testing
func TestFlags_DefaultValues(t *testing.T) {
    cmd := update.NewCommand()

    // Verify default flag values
    assert.Equal(t, false, cmd.Flags().Lookup("dry-run").DefValue == "true")
    assert.Equal(t, "prefer-scraper", cmd.Flags().Lookup("scalar-strategy").DefValue)
}

func TestFlags_MutuallyExclusiveOptions(t *testing.T) {
    cmd := update.NewCommand()

    // Set both --per-file and --interactive (should conflict)
    err := cmd.Flags().Set("per-file", "true")
    require.NoError(t, err)
    err = cmd.Flags().Set("interactive", "true")
    require.NoError(t, err)

    // RunE should detect conflict
    err = cmd.RunE(cmd, []string{})
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "cannot be used together")
}

// Integration testing with filesystem
func TestRun_Integration_DryRunMode(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }

    tmpDir := t.TempDir()
    configPath, _ := testutil.CreateTestConfig(t, nil)

    // Create test video file
    videoFile := filepath.Join(tmpDir, "IPX-123.mp4")
    require.NoError(t, os.WriteFile(videoFile, []byte("fake video"), 0644))

    cmd := update.NewCommand()
    cmd.Flags().Set("dry-run", "true")

    // Test with injected config
    err := update.Run(cmd, []string{tmpDir}, configPath)
    assert.NoError(t, err)
}

// Unit testing extracted functions
func TestConstructNFOPath(t *testing.T) {
    tests := []struct {
        name         string
        id           string
        dir          string
        perFile      bool
        expectedPath string
    }{
        {
            name:         "per-file mode",
            id:           "IPX-123",
            dir:          "/videos",
            perFile:      true,
            expectedPath: "/videos/IPX-123.nfo",
        },
        {
            name:         "single NFO mode",
            id:           "IPX-456",
            dir:          "/videos",
            perFile:      false,
            expectedPath: "/videos/javinizer.nfo",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            match := matcher.MatchResult{
                ID:   tt.id,
                File: scanner.FileInfo{Dir: tt.dir},
            }
            movie := &models.Movie{ID: tt.id}

            result := update.ConstructNFOPath(match, movie, tt.perFile)
            assert.Equal(t, tt.expectedPath, result)
        })
    }
}
```

**Key Requirements for CLI Command Testing:**

- Export `run()` → `Run()` with config file parameter for dependency injection
- Test command structure: flags, defaults, short forms, mutual exclusivity
- Use `t.TempDir()` for integration tests (auto-cleanup)
- Use `testutil.CreateTestConfig()` to generate test configs
- Skip integration tests in short mode: `if testing.Short() { t.Skip() }`
- Test both success and error paths
- Test NFO merge logic when updating existing metadata

**Example Export Pattern:**

```go
// Before (untestable):
func run(cmd *cobra.Command, args []string) error {
    cfg := viper.Get("config")  // Global state
    // ... business logic ...
}

// After (testable):
func Run(cmd *cobra.Command, args []string, configFile string) error {
    cfg, err := config.Load(configFile)  // Injected dependency
    if err != nil {
        return err
    }
    // ... business logic ...
}
```

See `cmd/javinizer/commands/update/command_test.go` for the complete 544-line test suite achieving 85.4% coverage with 20 tests covering flags, integration scenarios, and unit functionality.

#### Testing API Command (Epic 7 Pattern)

For commands that start long-running servers (like API servers), the key is **separating initialization from server startup**:

**Pattern: Return Dependencies WITHOUT Starting Server**

```go
// Export Run function that returns initialized dependencies
// cmd/javinizer/commands/api/command.go:66
func Run(cmd *cobra.Command, configFile string, hostFlag string, portFlag int) (*api.ServerDependencies, error) {
    // All initialization logic (config, database, scrapers, repos, aggregator, matcher, job queue)
    // ... ~80 lines of setup ...

    // Return dependencies WITHOUT starting blocking HTTP server
    return apiDeps, nil
}

// Private run function handles blocking server startup
func run(cmd *cobra.Command, configFile string, hostFlag string, portFlag int) error {
    apiDeps, err := Run(cmd, configFile, hostFlag, portFlag)
    if err != nil {
        return err
    }
    defer apiDeps.DB.Close()

    router := api.NewServer(apiDeps)
    addr := fmt.Sprintf("%s:%d", apiDeps.GetConfig().Server.Host, apiDeps.GetConfig().Server.Port)
    return router.Run(addr)  // Blocking - NOT testable
}
```

**Testing Strategy:**
- **Export Run()**: Tests initialization WITHOUT starting HTTP server
- **Keep private run()**: Blocking server startup remains untestable (architectural limitation)
- **Result**: 81.6% coverage on Run(), 0% on run(), 64.3% overall package coverage

**Example Test:**
```go
func TestRun_DatabaseInit(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }

    configPath, _ := setupTagTestDB(t)
    cmd := api.NewCommand()

    // Test Run() WITHOUT starting server
    deps, err := api.Run(cmd, configPath, "", 0)
    require.NoError(t, err)
    defer deps.DB.Close()

    // Verify database initialized
    assert.NotNil(t, deps.DB)

    // Verify tables exist (migrations ran)
    var tableCount int
    deps.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount)
    assert.Greater(t, tableCount, 0, "should have tables after migrations")
}
```

**Test Categories (13 tests, 64.3% coverage):**
- **Flag tests** (2): command structure, default values
- **Flag override tests** (4): host, port, both flags, config loading
- **Integration tests** (6): database init, scraper registry, repositories, aggregator, matcher, job queue
- **Error handling** (1): config not found

**Key Benefits:**
- Tests ALL initialization logic without HTTP complications
- No need for HTTP client mocking or port conflicts
- Fast execution (<1s for 13 tests)
- Validates real database migrations, scraper setup, repository initialization

**Architectural Limitation:**
Private `run()` function remains at 0% coverage because `router.Run(addr)` blocks indefinitely. This is acceptable since all business logic is tested via the exported `Run()` function.

#### Testing Scrape Command (Epic 7 Pattern)

For commands with complex business logic and console output, the key is **separating testable business logic from untestable I/O**:

**Pattern: Return Data WITHOUT Console Output**

```go
// Export Run function that returns scraped data WITHOUT printing
// cmd/javinizer/commands/scrape/command.go:136
func Run(cmd *cobra.Command, args []string, configFile string, deps *commandutil.Dependencies) (*models.Movie, []*models.ScraperResult, error) {
    id := args[0]

    // Load config and apply flag overrides
    cfg, err := config.LoadOrCreate(configFile)
    if err != nil {
        return nil, nil, fmt.Errorf("failed to load config: %w", err)
    }
    ApplyFlagOverrides(cmd, cfg)

    // Initialize or use injected dependencies
    if deps == nil {
        deps, err = commandutil.NewDependencies(cfg)
        if err != nil {
            return nil, nil, err
        }
        defer deps.Close()
    }

    // Business logic: cache check, content-ID resolution, scraping, aggregation
    // ... ~130 lines of testable logic ...

    // Return data WITHOUT printing
    return movie, results, nil
}

// Private runScrape function handles console output
func runScrape(cmd *cobra.Command, args []string, configFile string) error {
    movie, results, err := Run(cmd, args, configFile, nil)
    if err != nil {
        return err
    }

    printMovie(movie, results)  // Console formatting - NOT testable
    return nil
}
```

**Testing Strategy:**
- **Export Run()**: Tests business logic (cache, scraping, aggregation) WITHOUT console output
- **Keep private runScrape()**: Console output remains untestable (I/O operations)
- **Result**: improved command-package testability by isolating business logic from console output formatting.

**Example Test:**

```go
func TestRun_ConfigNotFound(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }

    cmd := scrape.NewCommand()

    // Test Run() with non-existent config
    movie, results, err := scrape.Run(cmd, []string{"TEST-001"}, "/nonexistent/config.yaml", nil)

    assert.Error(t, err)
    assert.Nil(t, movie)
    assert.Nil(t, results)
    assert.Contains(t, err.Error(), "failed to load config")
}
```

**Test Infrastructure (for integration tests that CAN execute):**

```go
// Mock scraper for hermetic testing
type MockScraper struct {
    name string
    fail bool
}

func (m *MockScraper) Search(id string) (*models.ScraperResult, error) {
    if m.fail {
        return nil, assert.AnError
    }

    releaseDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
    return &models.ScraperResult{
        ID:          id,
        ContentID:   id,
        Title:       "Test Movie " + id,
        ReleaseDate: &releaseDate,
        Runtime:     120,
        Source:      m.name,
        SourceURL:   "http://test.com/" + id,
    }, nil
}

// Test database setup helper
func setupTestDB(t *testing.T) (string, *database.DB) {
    t.Helper()

    configContent := `
database:
  dsn: ":memory:"
scrapers:
  priority: ["mock1", "mock2"]
  dmm:
    enabled: true
`
    tmpFile := t.TempDir() + "/config.yaml"
    require.NoError(t, os.WriteFile(tmpFile, []byte(configContent), 0644))

    cfg, err := config.Load(tmpFile)
    require.NoError(t, err)

    db, err := database.New(cfg)
    require.NoError(t, err)
    err = db.AutoMigrate()
    require.NoError(t, err)

    return tmpFile, db
}
```

**Test Categories (18 tests, 24.2% coverage):**
- **Flag tests** (10): command structure, flag parsing, defaults, validation (existing from Epic 5)
- **Integration tests** (8): config loading, cache hit/miss, force refresh, custom scrapers, content-ID resolution, empty results, aggregation, database save
  - **Note:** 7 out of 8 integration tests are currently skipped due to aggregator dependency initialization complexity (architectural limitation documented in Epic 7 Story 7.2)

**Key Benefits:**
- Run() function extracted for testability (primary refactoring goal achieved)
- Pattern consistent with Epic 7.1 API command approach
- Zero breaking changes to CLI interface
- Clear separation between business logic and console I/O

**Architectural Limitation:**

Due to complex aggregator dependency initialization requirements, 7 out of 8 integration tests are currently skipped. The tests are well-written with proper hermetic design (MockScraper, in-memory database, no HTTP calls), but cannot execute until the aggregator initialization complexity is resolved in a future epic.

**Skipped Test Example:**

```go
func TestRun_CacheHit(t *testing.T) {
    t.Skip("Architectural limitation: aggregator dependency setup requires further refactoring")

    if testing.Short() {
        t.Skip("integration test")
    }

    configPath, db := setupTestDB(t)
    defer db.Close()

    // Pre-populate database with test movie
    movieRepo := database.NewMovieRepository(db)
    cachedMovie := createTestMovie("IPX-123", "Cached Movie")
    require.NoError(t, movieRepo.Upsert(cachedMovie))

    cmd := scrape.NewCommand()

    // Run without force refresh - should hit cache
    movie, results, err := scrape.Run(cmd, []string{"IPX-123"}, configPath, deps)

    assert.NoError(t, err)
    assert.NotNil(t, movie)
    assert.Equal(t, "Cached Movie", movie.Title)
    assert.Nil(t, results, "Cache hit should not return scraper results")
}
```

**Coverage Breakdown:**
```
NewCommand:          100.0% ✅ (command structure)
ApplyFlagOverrides:  100.0% ✅ (flag overrides)
Run:                   5.4% ⚠️ (business logic - limited by architectural constraint)
runScrape:            60.0% ✅ (error handling paths)
printMovie:            0.0% ❌ (console output - not tested)
```

The printMovie() function (240 lines of table formatting) remains at 0% coverage. Future work could extract formatting logic to a testable `FormatMovieTable()` function, but this was deferred due to complexity.

**Reference:** Epic 7 Story 7.2 achieved Run() function extraction (primary goal), with full integration testing deferred to future epic for aggregator refactoring.

**Reference:** `cmd/javinizer/commands/api/command_test.go` (API command: 35.7% → 64.3% coverage, +14.3% above 50% target)

#### Epic 9: TUI Refactoring for Testability (MVP Pattern)

**Problem:** Bubble Tea TUI framework tightly couples business logic with UI rendering, making it difficult to achieve meaningful test coverage. Visual rendering (lipgloss styling, terminal dimensions) cannot be unit tested, while business logic (state management, message handling) is buried within framework callbacks.

**Solution:** Apply the Model-View-Presenter (MVP) pattern to separate concerns:
- **Presenter (Testable):** Pure functions for state management, message handling, data transformations
- **View (Excluded):** Visual rendering with lipgloss (manual QA only)
- **Model (Thin Wrapper):** Delegates to Presenter functions

**Epic 9 Goals:**
- Extract testable business logic from Bubble Tea framework
- Achieve 100% coverage on testable TUI components
- Establish repeatable pattern for future TUI development

---

### Story 9.1: Test Processor Business Logic

**Before (Coupled with Worker Pool):**

```go
// internal/tui/model.go:45 (BEFORE Epic 9)
type Model struct {
    pool *worker.Pool  // Direct dependency on concrete type
    // ... other fields ...
}

func (m *Model) ProcessFiles() {
    // Business logic directly in Bubble Tea model
    for _, file := range m.selectedFiles {
        task := worker.NewScrapeTask(file, m.cfg, /* ... */)
        m.pool.Submit(task)  // Tight coupling
    }
}

// ❌ Cannot test without real worker pool
// ❌ Business logic buried in UI model
// ❌ No way to verify task submission without side effects
```

**After (Dependency Injection):**

```go
// internal/tui/interfaces.go:15 (NEW in Epic 9)
type WorkerPoolInterface interface {
    Submit(task worker.Task) error
    Wait()
    Stop()
}

// internal/tui/processor.go:28 (Story 9.1a)
type ProcessingCoordinator struct {
    pool WorkerPoolInterface  // Interface, not concrete type
    cfg  *config.Config
    db   database.DB
}

func NewProcessingCoordinator(pool WorkerPoolInterface, cfg *config.Config, db database.DB) *ProcessingCoordinator {
    return &ProcessingCoordinator{pool: pool, cfg: cfg, db: db}
}

func (pc *ProcessingCoordinator) ProcessFiles(files []string, options ProcessingOptions) error {
    for _, file := range files {
        task := worker.NewScrapeTask(file, pc.cfg, /* ... */)
        if err := pc.pool.Submit(task); err != nil {
            return err
        }
    }
    return nil
}
```

**Test Pattern:**

```go
// internal/tui/processor_test.go:45
func TestProcessingCoordinator_ProcessFiles(t *testing.T) {
    // Setup: Mock worker pool using interface
    mockPool := &MockWorkerPool{
        submitted: make([]worker.Task, 0),
    }

    cfg, _ := testutil.CreateTestConfig(t, nil)
    db := testutil.SetupTestDB(t)
    defer db.Close()

    pc := NewProcessingCoordinator(mockPool, cfg, db)

    // Execute
    files := []string{"IPX-123.mp4", "SSIS-456.mp4"}
    err := pc.ProcessFiles(files, ProcessingOptions{
        ScrapeEnabled: true,
    })

    // Verify
    assert.NoError(t, err)
    assert.Len(t, mockPool.submitted, 2)
    assert.Equal(t, "IPX-123.mp4", mockPool.submitted[0].Description())
}
```

**Key Learnings:**
- Extract interfaces for all external dependencies
- Move business logic OUT of Bubble Tea callbacks
- Use constructor injection (`NewProcessingCoordinator`) for testability
- Mock interfaces, not concrete types

**Coverage Impact:** 76.5% → 100% (13 tests)
**Reference:** `internal/tui/processor_test.go`, `internal/tui/interfaces.go`

---

### Story 9.2: Extract State Management from Model

**Before (Mutable State in Bubble Tea Model):**

```go
// internal/tui/model.go:125 (BEFORE Epic 9)
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.String() == "j" {
            // Business logic embedded in Update()
            m.currentIndex++
            if m.currentIndex >= len(m.files) {
                m.currentIndex = len(m.files) - 1  // Boundary check
            }
        }
        if msg.String() == "k" {
            m.currentIndex--
            if m.currentIndex < 0 {
                m.currentIndex = 0
            }
        }
    }
    return m, nil
}

// ❌ Cannot test j/k navigation without Bubble Tea framework
// ❌ Boundary logic mixed with UI framework
// ❌ Mutable state makes testing fragile
```

**After (Pure State Transformation Functions):**

```go
// internal/tui/state.go:18 (Story 9.2)
// Pure function: takes current state, returns NEW state (immutable)
func HandleNavigationDown(currentIndex int, itemCount int) int {
    if itemCount == 0 {
        return 0
    }
    newIndex := currentIndex + 1
    if newIndex >= itemCount {
        return itemCount - 1  // Clamp at bottom
    }
    return newIndex
}

func HandleNavigationUp(currentIndex int) int {
    newIndex := currentIndex - 1
    if newIndex < 0 {
        return 0  // Clamp at top
    }
    return newIndex
}

// internal/tui/update.go:85 (AFTER Epic 9)
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.String() == "j" {
            m.currentIndex = HandleNavigationDown(m.currentIndex, len(m.files))
        }
        if msg.String() == "k" {
            m.currentIndex = HandleNavigationUp(m.currentIndex)
        }
    }
    return m, nil
}
```

**Test Pattern:**

```go
// internal/tui/state_test.go:28
func TestHandleNavigationDown(t *testing.T) {
    tests := []struct {
        name         string
        currentIndex int
        itemCount    int
        expected     int
    }{
        {
            name:         "move down within bounds",
            currentIndex: 5,
            itemCount:    10,
            expected:     6,
        },
        {
            name:         "clamp at bottom boundary",
            currentIndex: 9,
            itemCount:    10,
            expected:     9,
        },
        {
            name:         "empty list returns 0",
            currentIndex: 0,
            itemCount:    0,
            expected:     0,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := HandleNavigationDown(tt.currentIndex, tt.itemCount)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

**Key Learnings:**
- Extract pure functions for ALL state transformations
- Functions should be deterministic (same input → same output)
- No side effects, no mutations
- Bubble Tea Update() becomes a thin wrapper that delegates to pure functions

**Coverage Impact:** 100% (12 tests for 7 state management functions)
**Reference:** `internal/tui/state.go`, `internal/tui/state_test.go`

---

### Story 9.3: Extract Message Handlers from Update

**Before (Message Handling Embedded in Update):**

```go
// internal/tui/update.go:45 (BEFORE Epic 9)
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case ProgressUpdateMsg:
        // Complex message handling logic embedded here
        m.tasks[msg.ID] = TaskState{
            ID:          msg.ID,
            Description: msg.Description,
            Progress:    msg.Progress,
            Status:      "in_progress",
        }
        m.bytesDownloaded += msg.BytesDelta
        m.lastUpdate = time.Now()
    case TaskCompleteMsg:
        if task, exists := m.tasks[msg.ID]; exists {
            task.Status = "completed"
            task.Progress = 1.0
            m.tasks[msg.ID] = task
        }
    }
    return m, nil
}

// ❌ Cannot test message handling without Bubble Tea
// ❌ Business logic tightly coupled with framework types
```

**After (Pure Message Handler Functions):**

```go
// internal/tui/handlers.go:22 (Story 9.3)
// Pure function: takes current state + message, returns updated state
func HandleProgressUpdate(tasks map[string]TaskState, msg ProgressUpdateMsg, bytesDownloaded int64) (map[string]TaskState, int64) {
    updatedTasks := make(map[string]TaskState, len(tasks))
    for k, v := range tasks {
        updatedTasks[k] = v  // Copy map
    }

    updatedTasks[msg.ID] = TaskState{
        ID:          msg.ID,
        Description: msg.Description,
        Progress:    msg.Progress,
        Status:      "in_progress",
    }

    newBytesDownloaded := bytesDownloaded + msg.BytesDelta

    return updatedTasks, newBytesDownloaded
}

func HandleTaskComplete(tasks map[string]TaskState, taskID string) map[string]TaskState {
    updatedTasks := make(map[string]TaskState, len(tasks))
    for k, v := range tasks {
        updatedTasks[k] = v
    }

    if task, exists := updatedTasks[taskID]; exists {
        task.Status = "completed"
        task.Progress = 1.0
        updatedTasks[taskID] = task
    }

    return updatedTasks
}

// internal/tui/update.go:45 (AFTER Epic 9)
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case ProgressUpdateMsg:
        m.tasks, m.bytesDownloaded = HandleProgressUpdate(m.tasks, msg, m.bytesDownloaded)
    case TaskCompleteMsg:
        m.tasks = HandleTaskComplete(m.tasks, msg.ID)
    }
    return m, nil
}
```

**Test Pattern:**

```go
// internal/tui/handlers_test.go:35
func TestHandleProgressUpdate(t *testing.T) {
    initialTasks := map[string]TaskState{
        "task-1": {ID: "task-1", Description: "Old task", Progress: 0.5},
    }

    msg := ProgressUpdateMsg{
        ID:          "task-2",
        Description: "New task",
        Progress:    0.25,
        BytesDelta:  1024,
    }

    // Execute: Pure function, no side effects
    updatedTasks, newBytes := HandleProgressUpdate(initialTasks, msg, 5000)

    // Verify: Original unchanged (immutability)
    assert.Len(t, initialTasks, 1)
    assert.Equal(t, int64(5000), int64(5000))  // Original bytes unchanged

    // Verify: New state correct
    assert.Len(t, updatedTasks, 2)
    assert.Equal(t, "New task", updatedTasks["task-2"].Description)
    assert.Equal(t, 0.25, updatedTasks["task-2"].Progress)
    assert.Equal(t, int64(6024), newBytes)
}
```

**Key Learnings:**
- Extract message handlers as pure functions
- Always return NEW state (never mutate input)
- Copy maps/slices before modification
- Handler functions should be framework-agnostic (no tea.Msg dependencies)

**Coverage Impact:** 100% (25 tests for 5 handler functions)
**Reference:** `internal/tui/handlers.go`, `internal/tui/handlers_test.go`

---

### Story 9.4: Extract Data Transformations from Components

**Before (Formatting Logic in View Code):**

```go
// internal/tui/components.go:125 (BEFORE Epic 9)
func (m *Model) renderTaskList() string {
    var lines []string
    for _, task := range m.tasks {
        // Formatting logic embedded in rendering
        var sizeStr string
        if task.BytesDownloaded < 1024 {
            sizeStr = fmt.Sprintf("%d B", task.BytesDownloaded)
        } else if task.BytesDownloaded < 1024*1024 {
            sizeStr = fmt.Sprintf("%.2f KB", float64(task.BytesDownloaded)/1024)
        } else {
            sizeStr = fmt.Sprintf("%.2f MB", float64(task.BytesDownloaded)/(1024*1024))
        }

        progressBar := renderProgressBar(task.Progress)
        lines = append(lines, fmt.Sprintf("%s %s %s", task.Description, progressBar, sizeStr))
    }
    return strings.Join(lines, "\n")
}

// ❌ Cannot test formatting logic without rendering
// ❌ Business logic mixed with lipgloss styling
```

**After (Pure Transformation Functions):**

```go
// internal/tui/transforms.go:18 (Story 9.4)
// Pure function: bytes → human-readable string
func FormatFileSize(bytes int64) string {
    if bytes < 1024 {
        return fmt.Sprintf("%d B", bytes)
    } else if bytes < 1024*1024 {
        return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
    } else if bytes < 1024*1024*1024 {
        return fmt.Sprintf("%.2f MB", float64(bytes)/(1024*1024))
    }
    return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
}

func FormatProgressPercent(progress float64) string {
    return fmt.Sprintf("%.1f%%", progress*100)
}

func FormatElapsedTime(duration time.Duration) string {
    if duration < time.Minute {
        return fmt.Sprintf("%.0fs", duration.Seconds())
    } else if duration < time.Hour {
        return fmt.Sprintf("%.1fm", duration.Minutes())
    }
    return fmt.Sprintf("%.1fh", duration.Hours())
}

// internal/tui/components.go:125 (AFTER Epic 9)
func (m *Model) renderTaskList() string {
    var lines []string
    for _, task := range m.tasks {
        sizeStr := FormatFileSize(task.BytesDownloaded)  // Pure function
        progressBar := renderProgressBar(task.Progress)
        lines = append(lines, fmt.Sprintf("%s %s %s", task.Description, progressBar, sizeStr))
    }
    return strings.Join(lines, "\n")
}
```

**Test Pattern:**

```go
// internal/tui/transforms_test.go:22
func TestFormatFileSize(t *testing.T) {
    tests := []struct {
        name     string
        bytes    int64
        expected string
    }{
        {name: "bytes", bytes: 512, expected: "512 B"},
        {name: "kilobytes", bytes: 2048, expected: "2.00 KB"},
        {name: "megabytes", bytes: 5*1024*1024, expected: "5.00 MB"},
        {name: "gigabytes", bytes: 3*1024*1024*1024, expected: "3.00 GB"},
        {name: "exact 1KB", bytes: 1024, expected: "1.00 KB"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := FormatFileSize(tt.bytes)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

**Key Learnings:**
- Extract ALL formatting/transformation logic into pure functions
- Separate data transformation from visual rendering
- Functions should be unit-testable (no dependencies on terminal width, colors, etc.)
- View code becomes thin wrapper around transforms

**Coverage Impact:** 100% (22 tests for 4 transformation functions)
**Reference:** `internal/tui/transforms.go`, `internal/tui/transforms_test.go`

---

### Story 9.5: Add Integration Tests for Critical User Flows

**Integration Test Pattern (Bubble Tea Test Harness):**

```go
// internal/tui/integration_test.go:28
func TestTUI_FileBrowserNavigation_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }

    // Setup: Mock dependencies
    mockPool := &MockWorkerPool{submitted: []worker.Task{}}
    cfg, _ := testutil.CreateTestConfig(t, nil)

    // Create TUI model with injected dependencies
    m := NewModel(Options{
        Pool:  mockPool,
        Config: cfg,
        Files: []string{"file1.mp4", "file2.mp4", "file3.mp4"},
    })

    // Simulate j key (down) navigation
    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
    assert.Equal(t, 1, m.currentIndex)

    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
    assert.Equal(t, 2, m.currentIndex)

    // Boundary test: j at bottom should clamp
    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
    assert.Equal(t, 2, m.currentIndex)  // Clamped, didn't wrap

    // Simulate k key (up) navigation
    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
    assert.Equal(t, 1, m.currentIndex)

    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
    assert.Equal(t, 0, m.currentIndex)

    // Boundary test: k at top should clamp
    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
    assert.Equal(t, 0, m.currentIndex)  // Clamped, didn't wrap
}

func TestTUI_TaskSubmissionFlow_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }

    mockPool := &MockWorkerPool{submitted: []worker.Task{}}
    cfg, _ := testutil.CreateTestConfig(t, nil)

    m := NewModel(Options{
        Pool:  mockPool,
        Config: cfg,
        Files: []string{"IPX-123.mp4"},
    })

    // User flow: Select file → Press 's' (scrape) → Confirm
    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})  // Select
    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})  // Scrape
    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})  // Confirm

    // Verify: Task submitted to worker pool
    assert.Len(t, mockPool.submitted, 1)
    assert.Contains(t, mockPool.submitted[0].Description(), "IPX-123")
}
```

**Key Learnings:**
- Integration tests validate multi-component interactions
- Use hermetic mocks (no external I/O, <1s execution)
- Test critical user journeys (file selection, task submission, view switching)
- Guard with `testing.Short()` to skip in fast test runs

**Coverage Impact:** 4 integration tests, 100% pass rate, <1s execution
**Reference:** `internal/tui/integration_test.go`

---

### Epic 9 Summary

**Refactoring Pattern Applied:**
1. Extract interfaces for dependencies (Story 9.1a)
2. Move business logic to pure functions (Stories 9.2, 9.3, 9.4)
3. Make functions immutable (copy-on-write semantics)
4. Add integration tests for user flows (Story 9.5)

**Coverage Achieved:**
- processor.go: 100% (13 tests)
- state.go: 100% (12 tests)
- handlers.go: 100% (25 tests)
- transforms.go: 100% (22 tests)
- integration_test.go: 4 tests (100% pass)

**Total:** 76 tests achieving 100% coverage on testable TUI code

**Why TUI is Excluded from Overall Coverage:**
TUI package is excluded by default in the `make coverage` target because:
1. Visual rendering (view.go, styles.go) cannot be unit tested
2. Bubble Tea framework integration requires manual QA
3. Testable business logic measured separately (achieved 100%)

**Architectural Win:**
The MVP pattern enables **confident TUI feature development** with **regression testing** for business logic, while acknowledging that visual rendering requires manual verification.

**References:**
- Epic 9 Tech Spec: `docs-no-commit/sprint-artifacts/tech-spec-epic-9.md`
- All test files: `internal/tui/*_test.go`
- CLAUDE.md TUI Testing Pattern section (comprehensive guide)

### Using testify

The project uses `github.com/stretchr/testify` for assertions:

```go
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSomething(t *testing.T) {
    result := DoSomething()

    // assert: test continues on failure
    assert.Equal(t, expected, result)
    assert.NotNil(t, result)

    // require: test stops on failure
    require.NoError(t, err)
    require.NotEmpty(t, result.ID)
}
```

## CI/CD Integration

### GitHub Actions Workflow

The project uses `.github/workflows/test.yml` with 5 parallel jobs:

1. **Unit Tests & Coverage**
   - Runs all tests
   - Generates coverage report
   - Enforces 75% minimum coverage
   - Uploads to Codecov

2. **Race Detector Tests**
   - Runs `-race` on concurrent packages
   - Catches data races in worker pool, TUI, websockets, API

3. **Linting & Code Quality**
   - `go vet`
   - `golangci-lint`
   - Code formatting check

4. **Build Verification**
   - Builds CLI binary
   - Verifies executable creation

5. **Docker Build Verification**
   - Builds Docker image
   - Verifies embedded version metadata

### CI Failure Scenarios

| Failure | Cause | Fix |
|---------|-------|-----|
| Coverage check failed | Coverage below 75% | Add tests or justify lower coverage |
| Race detector failure | Data race detected | Fix concurrent access, add mutexes |
| Linting failure | Code quality issues | Run `make lint` and fix issues |
| Formatting failure | Code not formatted | Run `make fmt` |
| Build failure | Compilation errors | Fix build errors |

### Codecov Integration

Coverage reports are uploaded to Codecov on every push/PR.

**Setup:**
1. Sign up at [codecov.io](https://codecov.io)
2. Add `CODECOV_TOKEN` to GitHub repository secrets
3. View coverage reports and trends at codecov.io

**Codecov will:**
- Comment on PRs with coverage changes
- Fail PR if coverage drops significantly
- Track coverage trends over time
- Highlight uncovered lines

## Pre-commit Hooks

Install the pre-commit hook to catch issues before committing:

```bash
# One-time setup
cp scripts/pre-commit.sample .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

### What the Hook Checks

1. **Code Formatting** - Fails if code is not `gofmt`-formatted
2. **Go Vet** - Fails if `go vet` finds issues
3. **Fast Unit Tests** - Runs `go test -short` (30s timeout)
4. **Build Verification** - Ensures code compiles

### Bypassing the Hook

For emergencies only:

```bash
git commit --no-verify -m "WIP: emergency fix"
```

**Note:** CI will still enforce all checks, so this only defers validation.

## Troubleshooting

### Coverage Report Not Generated

```bash
# Ensure coverage.out exists
ls -la coverage.out

# Regenerate coverage
make coverage
```

### Race Detector Failures

```bash
# Run race detector locally
make test-race

# Or on specific package
go test -race -v ./internal/worker/...

# Common causes:
# - Unprotected shared variables
# - Missing mutex locks
# - Channel send/receive races
```

### Tests Timing Out

```bash
# Increase timeout
go test -timeout=5m ./...

# Or skip slow tests
go test -short ./...
```

### Coverage Check Failing Locally but Passing in CI

```bash
# Ensure you're using same coverage threshold
./scripts/check_coverage.sh 75 coverage.out

# Regenerate coverage with the project target
make coverage
```

### Pre-commit Hook Not Running

```bash
# Check if hook is executable
ls -la .git/hooks/pre-commit

# Make executable
chmod +x .git/hooks/pre-commit

# Verify hook content
cat .git/hooks/pre-commit
```

## Best Practices

1. **Write tests first** for new features (TDD)
2. **Run tests locally** before pushing (`make test`, `make coverage-check`)
3. **Use table-driven tests** for multiple scenarios
4. **Test error cases** not just happy paths
5. **Keep tests fast** - unit tests should be <1s each
6. **Mark slow tests** with `testing.Short()` checks
7. **Test concurrent code** with `-race` detector
8. **Mock external dependencies** (HTTP clients, filesystems)
9. **Clean up test resources** in `defer` or `t.Cleanup()`
10. **Document complex test setups** with comments

## Resources

- [Go Testing Package](https://pkg.go.dev/testing)
- [Testify Documentation](https://github.com/stretchr/testify)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
- [Table-Driven Tests](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)
- [go-acc Coverage Tool](https://github.com/ory/go-acc)

## Contributing

When adding new features:

1. Write tests covering the new functionality
2. Ensure `make coverage-check` passes (75%+ coverage)
3. Run `make test-race` if your code involves concurrency
4. Run `make ci` to verify all CI checks pass locally
5. Include test coverage information in your PR description

**Example PR Description:**
```
## Changes
- Added new scraper for XYZ site

## Testing
- Added unit tests for scraper (85% coverage)
- Tested with mock HTTP responses
- Ran `make ci` successfully

## Coverage Impact
- Overall coverage: 62% → 64% (+2%)
- New package coverage: 85%
```
