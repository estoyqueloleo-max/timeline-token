package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

func TestFindAndCollectSitemapOffline(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprintf(w, "User-agent: *\nSitemap: %s/sitemap-index.xml\nSitemap: %s/news-sitemap.xml\n", srv.URL, srv.URL)
		case "/news-sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:news="http://www.google.com/schemas/sitemap-news/0.9">
  <url>
    <loc>https://example.test/world/major-global-summit-starts-today</loc>
    <news:news>
      <news:publication_date>2026-11-14T10:00:00Z</news:publication_date>
      <news:title>Major global summit begins with climate and trade talks</news:title>
      <news:keywords>summit,climate,trade</news:keywords>
    </news:news>
  </url>
  <url>
    <loc>https://example.test/tech/new-open-source-browser-engine-released-today</loc>
    <lastmod>2026-11-14T11:30:00Z</lastmod>
  </url>
</urlset>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	cr := NewCrawler("", 0)
	sm := cr.FindSitemap(base, srv.Client())
	if sm != srv.URL+"/news-sitemap.xml" {
		t.Fatalf("expected prioritized news sitemap, got %q", sm)
	}

	body, _, err := downloadFeedOrSitemapBytes(context.Background(), sm)
	if err != nil {
		t.Fatal(err)
	}
	var entries []sitemapNewsEntry
	collectSitemapEntries(context.Background(), body, 0, &entries)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Title != "Major global summit begins with climate and trade talks" || entries[0].PublishedAt != "2026-11-14T10:00:00Z" || entries[0].Summary != "summit,climate,trade" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].Title == "" || entries[1].PublishedAt != "2026-11-14T11:30:00Z" {
		t.Fatalf("expected slug title fallback for second entry, got %+v", entries[1])
	}
}

func TestLiveNewsSitemaps(t *testing.T) {
	if os.Getenv("LIVE_SITEMAP_TEST") != "1" {
		t.Skip("set LIVE_SITEMAP_TEST=1 to probe live news sitemaps")
	}

	portals := []string{
		"https://www.nytimes.com",
		"https://www.bloomberg.com",
		"https://www.dw.com",
		"https://thehill.com",
	}

	cr := NewCrawler("", 0)
	for _, p := range portals {
		sm, err := cr.FindRSS(p)
		if err != nil || sm == "" {
			t.Errorf("%s: FindRSS failed/empty: sm=%q err=%v", p, sm, err)
			continue
		}
		body, _, dlErr := downloadFeedOrSitemapBytes(context.Background(), sm)
		if dlErr != nil {
			t.Errorf("%s: download sitemap %s failed: %v", p, sm, dlErr)
			continue
		}
		var entries []sitemapNewsEntry
		collectSitemapEntries(context.Background(), body, 0, &entries)
		t.Logf("%s -> %s -> extracted %d entries (sample=%+v)", p, sm, len(entries), func() any {
			if len(entries) > 0 {
				return entries[0]
			}
			return nil
		}())
		if len(entries) == 0 {
			t.Errorf("%s: sitemap %s extracted 0 entries", p, sm)
		}
	}
}

