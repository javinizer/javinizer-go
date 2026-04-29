# Requirements: Javinizer Go

**Defined:** 2026-04-29
**Core Value:** Users can organize and enrich their JAV library with accurate metadata from multiple sources.

## v0.3.1 Requirements

### Decomposition

- [x] **DECOMP-01**: RunBatchScrapeOnce is decomposed from 754 lines into focused sub-functions with clear responsibilities
- [x] **DECOMP-02**: processUpdateMode is decomposed from 406 lines into focused sub-functions
- [x] **DECOMP-03**: processOrganizeJob is decomposed from 383 lines into focused sub-functions
- [x] **DECOMP-04**: processBatchJob is decomposed from 187 lines into focused sub-functions
- [x] **DECOMP-05**: Large scraper files (dmm.go 1570L, translation/service.go 1231L, aggregator.go 1137L) are split into focused files per responsibility

### Deduplication

- [x] **DEDUP-01**: 28 duplicate normalize* functions across 9 scrapers are consolidated into shared scraperutil helpers
- [x] **DEDUP-02**: Duplicate FlareSolverrConfig struct in configutil/validation.go and config/translation_config.go is unified into a single definition
- [x] **DEDUP-03**: Unused `_ = db` parameter is removed from 11 scraper module.go Config() methods

### Frontend Quality

- [x] **FEQ-01**: All `any` types in frontend TypeScript are replaced with proper typed interfaces (config:any across 10+ settings components, types.ts nfo_value?:any, proxy:any, flaresolverr?:any)
- [x] **FEQ-02**: settings/+page.svelte (1323L) is split into focused sub-components
- [x] **FEQ-03**: review/[jobId]/+page.svelte (1072L) is split into focused sub-components
- [x] **FEQ-04**: actresses/+page.svelte (991L) is split into focused sub-components
- [x] **FEQ-05**: Debug console.log statements are removed from websocket.ts production code

### CI & Quality Gates

- [x] **CI-01**: Frontend tests (npm run test) are included in CI pipeline
- [x] **CI-02**: golangci-lint config is expanded with meaningful custom rules beyond bare minimum
- [x] **CI-03**: govulncheck is added to CI pipeline for dependency vulnerability scanning

### Context & API

- [x] **CTX-01**: context.Background() is replaced with caller context in 7 scraper GetURL() wrappers for cancellable scrapes
- [x] **CTX-02**: Scraper GetURL method naming is normalized (GetURLCtx vs getURLWithContext) to consistent convention
- [x] **CTX-03**: Swagger/godoc annotations are added to remaining 84 API handler files

### Code Quality (Audit Gap Closure)

- [ ] **CQ-01**: defer fileCancel() in for-loop is fixed in process_update.go — no context leaks from deferred cancel inside loops
- [ ] **CQ-02**: initBatchDependencies error is handled explicitly in process_batch.go — no blank identifier discard
- [ ] **CQ-03**: Debug log artifact (*** DEBUG ***) is removed from production logging in scrape_nfo_merge.go
- [ ] **CQ-04**: TypeScript type for original_should_crop_poster uses `boolean | null` matching Go `*bool`
- [ ] **CQ-05**: golangci-lint exclusions for noctx, goconst, nilerr, unparam are narrowed to specific files (not all of internal/)
- [ ] **CQ-06**: G201 (SQL injection) and G204 (command injection) are removed from global gosec exclusions
- [ ] **CQ-07**: All decomposed batch functions use consistent error wrapping with fmt.Errorf and %w
- [ ] **CQ-08**: saveScrapedResult returns Upsert errors to callers instead of logging and swallowing
- [ ] **CQ-09**: Frontend store errors surface to users via toast/notification system
- [ ] **CQ-10**: Retry loops use context-aware sleep (select on ctx.Done() + time.After) instead of time.Sleep
- [ ] **CQ-11**: applyDisplayTitle exists in a single shared location, not duplicated across packages
- [ ] **CQ-12**: NFO discovery logic in mergeCachedNFO/mergeScrapedNFO is extracted into shared helper

## Future Requirements

Deferred to future release. Tracked but not in current roadmap.

### Decomposition

- **DECOMP-F01**: Split remaining 37 Go files over 500 lines into focused files

### Frontend Quality

- **FEQ-F01**: Add structural frontend test coverage (component, API client, store, controller tests)
- **FEQ-F02**: Migrate remaining onMount data-fetching patterns in review page to svelte-query

### CI & Quality Gates

- **CI-F01**: Add go mod tidy / go mod verify checks to CI
- **CI-F02**: Expand race detector coverage beyond 4 packages (add aggregator, downloader, scraperutil)
- **CI-F03**: Add API backward compatibility check to CI
- **CI-F04**: Add macOS and ARM Linux test matrix

### Context & API

- **CTX-F01**: Replace hardcoded chromedp.Sleep(3s) with configurable timeout in dmm/browser.go
- **CTX-F02**: Replace hardcoded SSRF/network timeouts with configurable values

## Out of Scope

| Feature | Reason |
|---------|--------|
| Mobile application | Web-first, mobile later |
| Full ScraperModule interface refactor (any→concrete types) | High churn, separate milestone |
| Application service layer extraction | Architectural refactor, too broad |
| ServerDependencies god object split | Low priority, working as-is |
| FlareSolverr session cleanup | Low priority, no active bug reports |
| Test migration to assert/require libraries | Gradual, not milestone-scoped |
| Pipeline integration test | Complex setup, defer to dedicated test milestone |
| Tag management CRUD (UI-01, UI-02) | Deferred per user decision, separate milestone |
| Scraper config struct elimination (9 of 14 with no unique fields) | High churn, separate milestone |
| Frontend test coverage (structural tests) | Separate milestone scope |
| Race detector expansion | Lower priority than other CI improvements |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| DECOMP-01 | 057 | Complete |
| DECOMP-02 | 057 | Complete |
| DECOMP-03 | 057 | Complete |
| DECOMP-04 | 057 | Complete |
| DECOMP-05 | 058-03 | Complete |
| DEDUP-01 | 055-01 | Complete |
| DEDUP-02 | 055-02 | Complete |
| DEDUP-03 | 055-02 | Complete |
| FEQ-01 | 059 | Complete |
| FEQ-02 | 060-01 | Complete |
| FEQ-03 | 060-02 | Complete |
| FEQ-04 | 060-03 | Complete |
| FEQ-05 | 060-03 | Complete |
| CI-01 | 061-01 | Complete |
| CI-02 | 061-01 | Complete |
| CI-03 | 061-02 | Complete |
| CTX-01 | 056-02 | Complete |
| CTX-02 | 064 | Pending (gap closure) |
| CTX-03 | 062 | Complete |
| CQ-01 | 063 | Pending |
| CQ-02 | 063 | Pending |
| CQ-03 | 063 | Pending |
| CQ-04 | 065 | Pending |
| CQ-05 | 065 | Pending |
| CQ-06 | 065 | Pending |
| CQ-07 | 064 | Pending |
| CQ-08 | 063 | Pending |
| CQ-09 | 065 | Pending |
| CQ-10 | 064 | Pending |
| CQ-11 | 064 | Pending |
| CQ-12 | 064 | Pending |

**Coverage:**
- v0.3.1 original requirements: 19 total, 19 satisfied
- v0.3.1 audit gap requirements: 12 total, 0 satisfied
- Total: 31 requirements, 19 satisfied, 12 pending

---
*Requirements defined: 2026-04-29*
*Last updated: 2026-04-29 after initial definition*
