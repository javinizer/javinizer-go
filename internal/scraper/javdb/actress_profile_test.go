package javdb

import (
	"context"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestJavdbActorIDFromAvatarURL(t *testing.T) {
	require.Equal(t, "ZX", javdbActorIDFromAvatarURL("https://c0.jdbstatic.com/avatars/zx/ZX.jpg"))
	require.Equal(t, "", javdbActorIDFromAvatarURL("https://example.com/avatars/zx/ZX.jpg"))
	require.Equal(t, "", javdbActorIDFromAvatarURL("https://c0.jdbstatic.com/avatars/zx/ZX"))
}

func TestExtractJavDBActorMetadata(t *testing.T) {
	doc := docFromHTML(t, `<html><head><meta property="og:title" content="安倍亜沙美 - JavDB"></head><body><div class="actor-panel"><h1 class="title is-4">安倍亜沙美</h1><img src="https://c0.jdbstatic.com/avatars/zx/ZX.jpg"></div></body></html>`)
	got := extractJavDBActorMetadata(doc, 19244, "ZX")
	require.Equal(t, models.ActressInfo{DMMID: 19244, JapaneseName: "安倍亜沙美", ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"}, got)
}

func TestExtractJavDBActorMetadataLatinName(t *testing.T) {
	doc := docFromHTML(t, `<h1 class="title is-4">Yui Hatano</h1>`)
	got := extractJavDBActorMetadata(doc, 5786, "A5yq")
	require.Equal(t, models.ActressInfo{DMMID: 5786, FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://c0.jdbstatic.com/avatars/a5/A5yq.jpg"}, got)
}

func TestExtractJavDBActorMetadataActualProfileMarkup(t *testing.T) {
	doc := docFromHTML(t, `<h2 class="title is-4 has-text-justified"><span class="actor-section-name">安倍亜沙美</span><br><span class="section-meta">6912 movie(s)</span></h2>`)
	got := extractJavDBActorMetadata(doc, 19244, "ZX")
	require.Equal(t, models.ActressInfo{DMMID: 19244, JapaneseName: "安倍亜沙美", ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"}, got)
}

func TestFindActorIDByName(t *testing.T) {
	client := resty.New()
	client.SetTransport(&staticRoundTripper{responses: map[string]string{
		"https://javdb.test/actors?locale=en&search=%E5%AE%89%E5%80%8D%E4%BA%9C%E6%B2%99%E7%BE%8E": `<a href="/actors/ZX" title="安倍亜沙美"><img class="avatar"></a>`,
	}})
	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}
	actorID, findErr := scraper.findActorID(context.Background(), "安倍亜沙美")
	require.NoError(t, findErr)
	require.Equal(t, "ZX", actorID)
}

func TestResolveActressMetadataFromActorProfile(t *testing.T) {
	profileURL := "https://javdb.test/actors/ZX?locale=en"
	client := resty.New()
	client.SetTransport(&staticRoundTripper{responses: map[string]string{
		// Codex: with a Japanese name present, the avatar-derived ID is
		// trusted only after the exact-name search confirms the same actor.
		"https://javdb.test/actors?locale=en&search=%E5%AE%89%E5%80%8D%E4%BA%9C%E6%B2%99%E7%BE%8E": `<a href="/actors/ZX" title="安倍亜沙美"><img class="avatar"></a>`,
		profileURL: `<html><body><h1 class="title is-4">安倍亜沙美</h1><img src="https://c0.jdbstatic.com/avatars/zx/ZX.jpg"></body></html>`,
	}})
	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	got, gotErr := scraper.ResolveActressMetadata(context.Background(), models.ActressInfo{
		DMMID: 19244, JapaneseName: "安倍亜沙美", ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg",
	})
	require.NoError(t, gotErr)
	require.Equal(t, models.ActressInfo{DMMID: 19244, JapaneseName: "安倍亜沙美", ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"}, got)
}

// Codex: an avatar-derived actor ID must be confirmed by the exact-name
// search — a stale or misassigned avatar must not fetch another actor's
// profile into this record.
func TestResolveActressMetadataDistrustsMismatchedAvatar(t *testing.T) {
	search := "https://javdb.test/actors?locale=en&search=%E5%AE%89%E5%80%8D%E4%BA%9C%E6%B2%99%E7%BE%8E"

	client := resty.New()
	client.SetTransport(&staticRoundTripper{responses: map[string]string{
		search:                                   `<a href="/actors/AB" title="安倍亜沙美"></a>`,
		"https://javdb.test/actors/AB?locale=en": `<html><body><h1 class="title is-4">安倍亜沙美</h1><img src="https://c0.jdbstatic.com/avatars/ab/AB.jpg"></body></html>`,
	}})
	s := &scraper{client: client, enabled: true, baseURL: "https://javdb.test", rateLimiter: ratelimit.NewLimiter(0), settings: models.ScraperSettings{Enabled: true}}
	got, err := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 19244, JapaneseName: "安倍亜沙美", ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"})
	require.NoError(t, err)
	require.Equal(t, models.ActressInfo{DMMID: 19244, JapaneseName: "安倍亜沙美", ThumbURL: "https://c0.jdbstatic.com/avatars/ab/AB.jpg"}, got,
		"must follow the name-confirmed actor, not the avatar")

	// When the name resolves nowhere, the avatar ID is not trusted at all.
	client2 := resty.New()
	client2.SetTransport(&staticRoundTripper{responses: map[string]string{
		search: `<a href="/actors/NO" title="誰か"></a>`,
	}})
	s2 := &scraper{client: client2, enabled: true, baseURL: "https://javdb.test", rateLimiter: ratelimit.NewLimiter(0), settings: models.ScraperSettings{Enabled: true}}
	got2, err2 := s2.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 19244, JapaneseName: "安倍亜沙美", ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"})
	require.NoError(t, err2)
	require.Equal(t, models.ActressInfo{DMMID: 19244}, got2, "unconfirmed avatar must not be trusted")
}
