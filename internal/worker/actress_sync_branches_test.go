package worker

// _ioremove is unused because resolveActressInfo accepts matches directly; keep

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Resolver stub with call counting.
type namedMetadataResolver struct {
	actressSyncScraper
	calls int
	err   error
	data  models.ActressInfo
}

func (s *namedMetadataResolver) Name() string { return s.actressSyncScraper.name }
func (s *namedMetadataResolver) ResolveActressMetadata(context.Context, models.ActressInfo) (models.ActressInfo, error) {
	s.calls++
	return s.data, s.err
}

func TestSyncActressMetadataSkipSentinelSuppressesResolvers(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 42})
	spy := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "dmm"}}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{enabled: []models.Scraper{spy}},
		ActressSyncOptions{ScrapeActress: &on, ActressFieldPriority: []string{"__skip__"}})
	require.NoError(t, err)
	require.Equal(t, 0, spy.calls)
	require.Contains(t, result.Messages, "no_verified_metadata")
}

func TestSyncActressMetadataExclusiveOverrideRestrictsResolvers(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 43})
	skip := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "dmm"}}
	keep := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "minnanoav"}, data: models.ActressInfo{DMMID: 43, JapaneseName: "花子"}}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{enabled: []models.Scraper{skip, keep}},
		ActressSyncOptions{ScrapeActress: &on, ActressFieldPriority: []string{"minnanoav"}})
	require.NoError(t, err)
	require.Equal(t, 0, skip.calls, "override-unlisted resolver must not run")
	require.Equal(t, 1, keep.calls)
	assert.Contains(t, result.UpdatedFields, "japanese_name")
}

// A transient resolver failure must not masquerade as "verified nothing":
// it surfaces in the result warning instead of silently skipping the task.
func TestSyncActressMetadataResolverFailureWarns(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 44})
	bad := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "dmm"}, err: errors.New("upstream burst")}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{enabled: []models.Scraper{bad}},
		ActressSyncOptions{ScrapeActress: &on})
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream burst")
	require.Contains(t, result.Warning, "resolver_error")
	require.Contains(t, result.Warning, "dmm")
}

func TestOrderScrapersByConfiguredPriorityBranches(t *testing.T) {
	dmm := &actressSyncScraper{name: "dmm"}
	javdb := &actressSyncScraper{name: "javdb"}
	minn := &actressSyncScraper{name: "minnanoav"}

	// Blank priority entries are skipped.
	got := orderScrapersByConfiguredPriority([]models.Scraper{dmm, javdb}, []string{"", "javdb", "dmm"})
	require.Equal(t, "javdb", got[0].Name())

	// Unlisted keep order after listed ones.
	got = orderScrapersByConfiguredPriority([]models.Scraper{dmm, javdb, minn}, []string{"minnanoav"})
	require.Equal(t, "minnanoav", got[0].Name())
	require.Equal(t, "dmm", got[1].Name())
	require.Equal(t, "javdb", got[2].Name())

	// Duplicate priority entries only rank once.
	got = orderScrapersByConfiguredPriority([]models.Scraper{dmm, javdb}, []string{"javdb", "javdb"})
	require.Equal(t, "javdb", got[0].Name())

	require.Equal(t, []models.Scraper{dmm}, orderScrapersByConfiguredPriority([]models.Scraper{dmm}, []string{"javdb"}))
	require.Equal(t, []models.Scraper{dmm}, orderScrapersByConfiguredPriority([]models.Scraper{dmm}, nil))
}

// Resolver declaring only some actress fields must not leak the rest.
type cappedFieldsResolver struct {
	actressSyncScraper
	fields []string
}

func (s *cappedFieldsResolver) ActressFields() []string { return s.fields }

func TestFilterActressResolverFieldsClearsUndeclared(t *testing.T) {
	javdbLike := &cappedFieldsResolver{actressSyncScraper: actressSyncScraper{name: "javdb"}, fields: []string{"actress_japanese_name", "actress_url"}}
	info := filterActressResolverFields(javdbLike, models.ActressInfo{JapaneseName: "安倍", FirstName: "Abe", LastName: "Asami", ThumbURL: "x"})
	require.Empty(t, info.FirstName)
	require.Empty(t, info.LastName)
	require.Equal(t, "安倍", info.JapaneseName)
	require.Equal(t, "x", info.ThumbURL)

	undeclared := filterActressResolverFields(&actressSyncScraper{name: "dmm"}, models.ActressInfo{FirstName: "Keep"})
	require.Equal(t, "Keep", undeclared.FirstName)
}

func TestActressIdentityNamesAndCandidatesSingleFields(t *testing.T) {
	// FirstName-only and LastName-only actresses both need candidate matching.
	// Phase-3 codex ruling: singleton field IS a valid match (previously
	// rejected was a phase-1-era over-tightening that silently dropped the
	// candidate and ended with missing_dmm_id).
	firstOnly := actressIdentityNames(&models.Actress{JapaneseName: "無名", FirstName: "Mona"})
	require.Contains(t, firstOnly, "Mona")
	require.True(t, identityCandidateMatches(firstOnly, models.ActressInfo{FirstName: "Mona", LastName: ""}),
		"singleton romanized name must match")

	full := actressIdentityNames(&models.Actress{JapaneseName: "橋本もな", FirstName: "Mona", LastName: "Hashimoto"})
	require.Contains(t, full, "Mona Hashimoto")
	require.Contains(t, full, "Hashimoto Mona")
	require.True(t, identityCandidateMatches(full, models.ActressInfo{JapaneseName: "橋本もな"}))
	require.False(t, identityCandidateMatches(full, models.ActressInfo{FirstName: "", LastName: ""}))

	// Non-blank only fields: the opposite ordering must also match, and a
	// duplicate must not be added.
	require.True(t, identityCandidateMatches(full, models.ActressInfo{FirstName: "Hashimoto", LastName: "Mona"}))
	require.NotContains(t, appendDedupLower(full, "mona hashimoto"), "mona hashimoto")
}

func TestIdentityCandidateMatchesRomaji(t *testing.T) {
	names := actressIdentityNames(&models.Actress{FirstName: "Mona", LastName: "Hashimoto"})
	require.Contains(t, names, "Mona Hashimoto")
	require.Contains(t, names, "Hashimoto Mona")
	require.True(t, identityCandidateMatches(names, models.ActressInfo{FirstName: "Mona", LastName: "Hashimoto"}))
	require.True(t, identityCandidateMatches(names, models.ActressInfo{FirstName: "Hashimoto", LastName: "Mona"}))
	require.False(t, identityCandidateMatches(names, models.ActressInfo{JapaneseName: "別人"}))
}

// resolveActressInfoByRank remaining branches: pre-filled fields untouched,
// mismatched DMM IDs filtered, same-rank ties conflict, invalid thumbs never
// win the pick.
func TestResolveActressInfoByRankRemainingBranches(t *testing.T) {
	actress := &models.Actress{DMMID: 9, FirstName: "Keep"}
	matches := rankActressMatches(
		models.ActressInfo{DMMID: 9, FirstName: "Other", JapaneseName: "jp"},
		models.ActressInfo{DMMID: 2, FirstName: "skip", JapaneseName: ""},
		models.ActressInfo{DMMID: 9, FirstName: "", JapaneseName: "jp", ThumbURL: "garbage"},
	)
	matches[0].rank = 1
	matches[1].rank = 0
	matches[2].rank = 1
	candidate, conflict := resolveActressInfo(actress, matches, true)
	require.Empty(t, candidate.FirstName, "candidates only carry filled blanks")
	require.Equal(t, "jp", candidate.JapaneseName)
	require.False(t, conflict)
	require.Equal(t, "garbage", candidate.ThumbURL)
}

func TestRestrictScrapersByPriorityNamesBranches(t *testing.T) {
	j := &actressSyncScraper{name: "javdb"}
	d := &actressSyncScraper{name: "dmm"}
	got := restrictScrapersByPriorityNames([]models.Scraper{nil, j, d}, []string{"javdb", "  ", "DMM"})
	require.Len(t, got, 2)
	require.Equal(t, j, got[0])
	require.Equal(t, d, got[1])
}

func TestActressSyncThumbnailRankSeeds(t *testing.T) {
	rank := actressSyncThumbnailRank([]string{"", "DMM"}, []string{"javdb", "dmm"})
	require.Equal(t, 0, rank("dmm"))
	require.Less(t, rank("dmm"), rank("javdb"))
	require.Greater(t, rank("minnanoav"), rank("javdb"))
}

// Codex late-stage finding: candidate with only FirstName must match a
// missing-ID actress carrying that singleton, not silently drop to missing.
func TestIdentityCandidateMatchesSingletonFirstName(t *testing.T) {
	names := actressIdentityNames(&models.Actress{FirstName: "Mona"})
	require.True(t, identityCandidateMatches(names, models.ActressInfo{FirstName: "Mona"}))
	require.True(t, identityCandidateMatches(names, models.ActressInfo{FirstName: "Mona"}))
	require.False(t, identityCandidateMatches(names, models.ActressInfo{FirstName: "Other"}))
	// Baseline: full-name pair still matches existing contract.
	require.True(t, identityCandidateMatches(names, models.ActressInfo{FirstName: "Mona", LastName: "Hashimoto"}))
}

// Codex round 11: with an actress priority ranking javdb above dmm, the
// missing-scope path must not let the DMM-sourced cache pin fields javdb
// would provide — the cache competes as a dmm-ranked match and backstops
// only what higher-ranked sources leave blank.
func TestMissingScopeCacheCompetesWithHigherRankedSource(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 5577, JapaneseName: "涼子"})
	lookup := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		if dmmID == 5577 {
			return models.ActressInfo{DMMID: 5577, FirstName: "CacheFirst", LastName: "CacheLast"}, true
		}
		return models.ActressInfo{}, false
	}
	javdb := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "javdb"}, data: models.ActressInfo{DMMID: 5577, FirstName: "JavdbFirst", JapaneseName: "涼子"}}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{enabled: []models.Scraper{javdb}},
		ActressSyncOptions{ScrapeActress: &on, LookupCache: lookup, ActressFieldPriority: []string{"javdb", "dmm"}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, javdb.calls, "higher-ranked source must run even on cache hit")
	got, findErr := repo.FindByID(context.Background(), actress.ID)
	require.NoError(t, findErr)
	require.Equal(t, "JavdbFirst", got.FirstName, "javdb outranks the dmm cache")
	require.Equal(t, "CacheLast", got.LastName, "cache backstops fields javdb leaves blank")
}

// The complementary fast path: when dmm leads the priority, the cache
// snapshot is the highest-ranked source and the scrape is short-circuited.
func TestMissingScopeCacheShortCircuitsWhenDMMTopsPriority(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 5578, JapaneseName: "涼子"})
	lookup := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		if dmmID == 5578 {
			return models.ActressInfo{DMMID: 5578, FirstName: "CacheFirst", LastName: "CacheLast", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/cache.jpg"}, true
		}
		return models.ActressInfo{}, false
	}
	javdb := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "javdb"}, data: models.ActressInfo{DMMID: 5578, FirstName: "JavdbFirst"}}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{enabled: []models.Scraper{javdb}},
		ActressSyncOptions{ScrapeActress: &on, LookupCache: lookup, ActressFieldPriority: []string{"dmm", "javdb"}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 0, javdb.calls, "cache-complete actress must not trigger scraping when dmm leads")
	got, findErr := repo.FindByID(context.Background(), actress.ID)
	require.NoError(t, findErr)
	require.Equal(t, "CacheFirst", got.FirstName)
	require.Equal(t, "CacheLast", got.LastName)
}

// Codex round 12: with no actress field priority, the global scrapers
// priority is the ranking fallback — a cache hit must not short-circuit a
// preferred resolver listed ahead of dmm there.
func TestMissingScopeCacheCompetesWithGlobalPriorityFallback(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 5580, JapaneseName: "涼子"})
	lookup := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		if dmmID == 5580 {
			return models.ActressInfo{DMMID: 5580, FirstName: "CacheFirst", LastName: "CacheLast"}, true
		}
		return models.ActressInfo{}, false
	}
	javdb := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "javdb"}, data: models.ActressInfo{DMMID: 5580, FirstName: "JavdbFirst", JapaneseName: "涼子"}}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{enabled: []models.Scraper{javdb}},
		ActressSyncOptions{ScrapeActress: &on, LookupCache: lookup, ScrapersPriority: []string{"javdb", "dmm"}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, javdb.calls, "globally preferred source must run even on cache hit")
	got, findErr := repo.FindByID(context.Background(), actress.ID)
	require.NoError(t, findErr)
	require.Equal(t, "JavdbFirst", got.FirstName, "javdb outranks the dmm cache via global priority")
	require.Equal(t, "CacheLast", got.LastName, "cache backstops fields javdb leaves blank")
}

// Codex: a cache snapshot equal to the stored record must not count as
// revalidation evidence — with every live resolver failing, the sync must
// not report verified_no_changes.
func TestRevalidationCacheIsNotVerificationEvidence(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{
		DMMID: 5590, JapaneseName: "涼子", FirstName: "Ryoko", LastName: "Suzuki",
		ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/ryoko.jpg",
	})
	lookup := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		if dmmID == 5590 {
			return models.ActressInfo{DMMID: 5590, JapaneseName: "涼子", FirstName: "Ryoko", LastName: "Suzuki", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/ryoko.jpg"}, true
		}
		return models.ActressInfo{}, false
	}
	down := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "dmm"}, err: errors.New("upstream outage")}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{enabled: []models.Scraper{down}},
		ActressSyncOptions{ScrapeActress: &on, LookupCache: lookup, Revalidate: true})
	require.Error(t, err)
	require.NotContains(t, result.Messages, "verified_no_changes",
		"cache snapshot equality is not revalidation evidence")
}

// Codex: cache-derived identity (DMM ID assignment / duplicate merge) rides
// the same cache-admission gate as field fill — __skip__ or an exclusive
// priority without dmm must suppress it, including the revalidation
// fallback path.
func TestCacheIdentityGatedBySkipSentinel(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 0, JapaneseName: "今井絵理"})
	lookup := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		return models.ActressInfo{DMMID: 6601, JapaneseName: "今井絵理", FirstName: "Eri", LastName: "Imai"}, true
	}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{},
		ActressSyncOptions{ScrapeActress: &on, LookupCache: lookup, ActressFieldPriority: []string{"__skip__"}})
	require.NoError(t, err)
	require.NotContains(t, result.UpdatedFields, "dmm_id", "__skip__ must suppress cache-derived identity")
	got, findErr := repo.FindByID(context.Background(), actress.ID)
	require.NoError(t, findErr)
	require.Zero(t, got.DMMID)
	require.Contains(t, result.Messages, "missing_dmm_id")
}

func TestCacheIdentityGatedInRevalidationFallback(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 0, JapaneseName: "今井絵理"})
	lookup := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		return models.ActressInfo{DMMID: 6601, JapaneseName: "今井絵理", FirstName: "Eri", LastName: "Imai"}, true
	}
	down := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "javdb"}, err: errors.New("outage")}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{enabled: []models.Scraper{down}},
		ActressSyncOptions{ScrapeActress: &on, LookupCache: lookup, Revalidate: true, ActressFieldPriority: []string{"javdb"}})
	require.NoError(t, err)
	require.NotContains(t, result.UpdatedFields, "dmm_id", "dmm-less priority must suppress cache-derived identity in revalidation too")
	got, findErr := repo.FindByID(context.Background(), actress.ID)
	require.NoError(t, findErr)
	require.Zero(t, got.DMMID)
	require.Contains(t, result.Messages, "missing_dmm_id")
}

// Complement: with no explicit priority the cache identity path still works
// (assignment remains the default behavior).
func TestCacheIdentityAssignedWhenDMMNotExcluded(t *testing.T) {
	_, repo, movies, actress := newActressSyncFixture(t, &models.Actress{DMMID: 0, JapaneseName: "今井絵理"})
	lookup := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		return models.ActressInfo{DMMID: 6601, JapaneseName: "今井絵理", FirstName: "Eri", LastName: "Imai", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/eri.jpg"}, true
	}
	on := true
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movies, &fixedScraperSet{},
		ActressSyncOptions{ScrapeActress: &on, LookupCache: lookup})
	require.NoError(t, err)
	require.Contains(t, result.UpdatedFields, "dmm_id")
	got, findErr := repo.FindByID(context.Background(), actress.ID)
	require.NoError(t, findErr)
	require.Equal(t, 6601, got.DMMID)
}
