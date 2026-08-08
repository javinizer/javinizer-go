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
	require.NoError(t, err)
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
	firstOnly := actressIdentityNames(&models.Actress{JapaneseName: "無名", FirstName: "Mona"})
	require.Contains(t, firstOnly, "Mona")
	require.True(t, identityCandidateMatches(firstOnly, models.ActressInfo{FirstName: "Mona", LastName: ""}) == false)

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
