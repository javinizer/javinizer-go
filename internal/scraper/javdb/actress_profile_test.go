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
	require.Equal(t, "ZX", scraper.findActorID(context.Background(), "安倍亜沙美"))
}

func TestResolveActressMetadataFromActorProfile(t *testing.T) {
	profileURL := "https://javdb.test/actors/ZX?locale=en"
	client := resty.New()
	client.SetTransport(&staticRoundTripper{responses: map[string]string{
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
