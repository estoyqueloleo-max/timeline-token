package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
	"timeline-token/db"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

type Extractor struct {
	fp   *gofeed.Parser
	Diag *DiagReport
}

func NewExtractor() *Extractor {
	return &Extractor{fp: gofeed.NewParser()}
}

// FetchRSS fetches and stores news from an RSS feed (with browser-UA & XML sitemap fallback)
func (e *Extractor) FetchRSS(portalID int, rssURL string, mediaType string, language string, region string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	feed, err := e.fp.ParseURLWithContext(rssURL, ctx)
	if err != nil {
		return e.fetchWithBrowserUAOrSitemap(ctx, portalID, rssURL, mediaType, language, region, err)
	}

	e.Diag.RecordFeedFetch(FeedFetchDiag{
		PortalID:   portalID,
		RSSURL:     rssURL,
		ItemsCount: len(feed.Items),
	})

	saveFeedItems(portalID, feed, mediaType, language, region)
	return nil
}

func saveFeedItems(portalID int, feed *gofeed.Feed, mediaType, language, region string) {
	for _, item := range feed.Items {
		_, err := db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, summary, url, published_date, media_type, language, region) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			portalID, item.Title, item.Description, item.Link, item.PublishedParsed, mediaType, language, region)
		if err != nil {
			log.Printf("Error saving news item: %v", err)
		}
	}
}

const maxSitemapBytes = 5 * 1024 * 1024
const maxSitemapItems = 50

type sitemapIndexXML struct {
	Sitemaps []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"sitemap"`
}

type urlSetXML struct {
	URLs []struct {
		Loc          string `xml:"loc"`
		LastMod      string `xml:"lastmod"`
		NewsTitle    string `xml:"news>title"`
		NewsPubDate  string `xml:"news>publication_date"`
		NewsKeywords string `xml:"news>keywords"`
		ImageTitle   string `xml:"image>title"`
	} `xml:"url"`
}

type sitemapNewsEntry struct {
	Title       string
	URL         string
	Summary     string
	PublishedAt string
}

func downloadFeedOrSitemapBytes(ctx context.Context, targetURL string) ([]byte, int, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", chromeLinuxUA)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml;q=0.9, */*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, fmt.Errorf("http error: %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSitemapBytes))
	if err != nil {
		return nil, resp.StatusCode, err
	}

	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		gz, gzErr := gzip.NewReader(bytes.NewReader(raw))
		if gzErr == nil {
			defer gz.Close()
			unpacked, uErr := io.ReadAll(io.LimitReader(gz, maxSitemapBytes))
			if uErr == nil && len(unpacked) > 0 {
				return unpacked, resp.StatusCode, nil
			}
		}
	}
	return raw, resp.StatusCode, nil
}

func scoreSubSitemapURL(rawURL string) int {
	low := strings.ToLower(rawURL)
	for _, bad := range []string{"image", "video", "tag", "author", "category", "topic", "archive-19", "archive-200"} {
		if strings.Contains(low, bad) {
			return -100
		}
	}
	score := 0
	if strings.Contains(low, "news") {
		score += 50
	}
	if strings.Contains(low, "latest") || strings.Contains(low, "recent") || strings.Contains(low, "today") || strings.Contains(low, "2026") {
		score += 30
	}
	if strings.Contains(low, "article") || strings.Contains(low, "post") || strings.Contains(low, "story") {
		score += 15
	}
	if strings.Contains(low, "2025") {
		score += 5
	}
	return score
}

func titleFromURLSlug(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	p := strings.TrimRight(u.Path, "/")
	if p == "" {
		return ""
	}
	seg := path.Base(p)
	for _, ext := range []string{".html", ".htm", ".aspx", ".php"} {
		seg = strings.TrimSuffix(seg, ext)
	}
	// Strip pure trailing numeric ID segments like a-79947535 when part is very short
	seg = strings.ReplaceAll(seg, "-", " ")
	seg = strings.ReplaceAll(seg, "_", " ")
	fields := strings.Fields(seg)
	if len(fields) < 4 {
		return ""
	}
	// Drop a single short numeric token at the end (common CMS article ID)
	if len(fields) > 4 {
		last := fields[len(fields)-1]
		if len(last) >= 5 && strings.IndexFunc(last, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
			fields = fields[:len(fields)-1]
		}
	}
	title := strings.Join(fields, " ")
	if len(title) < 20 {
		return ""
	}
	return strings.ToUpper(title[:1]) + title[1:]
}

func collectSitemapEntries(ctx context.Context, body []byte, depth int, out *[]sitemapNewsEntry) {
	if len(*out) >= maxSitemapItems || depth > 2 {
		return
	}
	lower := strings.ToLower(string(body))

	if strings.Contains(lower, "<sitemapindex") {
		var idx sitemapIndexXML
		if err := xml.Unmarshal(body, &idx); err != nil || len(idx.Sitemaps) == 0 {
			return
		}
		type scored struct {
			loc   string
			score int
		}
		var ranked []scored
		for _, sm := range idx.Sitemaps {
			loc := strings.TrimSpace(sm.Loc)
			if loc == "" {
				continue
			}
			sc := scoreSubSitemapURL(loc)
			if sc < 0 {
				continue
			}
			ranked = append(ranked, scored{loc: loc, score: sc})
		}
		sort.SliceStable(ranked, func(i, j int) bool {
			return ranked[i].score > ranked[j].score
		})
		if len(ranked) > 2 {
			ranked = ranked[:2]
		}
		for _, r := range ranked {
			if len(*out) >= maxSitemapItems {
				break
			}
			subBody, _, err := downloadFeedOrSitemapBytes(ctx, r.loc)
			if err != nil {
				continue
			}
			collectSitemapEntries(ctx, subBody, depth+1, out)
		}
		return
	}

	if strings.Contains(lower, "<urlset") {
		var us urlSetXML
		if err := xml.Unmarshal(body, &us); err != nil {
			return
		}
		for _, u := range us.URLs {
			if len(*out) >= maxSitemapItems {
				break
			}
			loc := strings.TrimSpace(u.Loc)
			if loc == "" {
				continue
			}
			title := strings.TrimSpace(u.NewsTitle)
			if title == "" {
				title = strings.TrimSpace(u.ImageTitle)
			}
			if title == "" {
				title = titleFromURLSlug(loc)
			}
			if len(title) < 20 {
				continue
			}
			pubDate := strings.TrimSpace(u.NewsPubDate)
			if pubDate == "" {
				pubDate = strings.TrimSpace(u.LastMod)
			}
			*out = append(*out, sitemapNewsEntry{
				Title:       title,
				URL:         loc,
				Summary:     strings.TrimSpace(u.NewsKeywords),
				PublishedAt: pubDate,
			})
		}
	}
}

func (e *Extractor) fetchWithBrowserUAOrSitemap(ctx context.Context, portalID int, rssURL, mediaType, language, region string, origErr error) error {
	body, _, dlErr := downloadFeedOrSitemapBytes(ctx, rssURL)
	if dlErr != nil {
		e.Diag.RecordFeedFetch(FeedFetchDiag{
			PortalID: portalID,
			RSSURL:   rssURL,
			Error:    origErr.Error(),
		})
		return origErr
	}

	// 1. Try parsing as RSS/Atom in case gofeed failed due to its default User-Agent
	if feed, parseErr := e.fp.Parse(bytes.NewReader(body)); parseErr == nil && feed != nil && len(feed.Items) > 0 {
		e.Diag.RecordFeedFetch(FeedFetchDiag{
			PortalID:   portalID,
			RSSURL:     rssURL,
			ItemsCount: len(feed.Items),
		})
		saveFeedItems(portalID, feed, mediaType, language, region)
		return nil
	}

	// 2. Try parsing as XML Sitemap / Google News Sitemap
	lower := strings.ToLower(string(body))
	if strings.Contains(lower, "<sitemapindex") || strings.Contains(lower, "<urlset") {
		var entries []sitemapNewsEntry
		collectSitemapEntries(ctx, body, 0, &entries)
		if len(entries) > 0 {
			e.Diag.RecordFeedFetch(FeedFetchDiag{
				PortalID:   portalID,
				RSSURL:     rssURL,
				ItemsCount: len(entries),
			})
			for _, entry := range entries {
				var pubDate any
				if entry.PublishedAt != "" {
					pubDate = entry.PublishedAt
				}
				_, err := db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, summary, url, published_date, media_type, language, region) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
					portalID, entry.Title, entry.Summary, entry.URL, pubDate, "sitemap", language, region)
				if err != nil {
					log.Printf("Error saving sitemap item: %v", err)
				}
			}
			return nil
		}
	}

	e.Diag.RecordFeedFetch(FeedFetchDiag{
		PortalID: portalID,
		RSSURL:   rssURL,
		Error:    origErr.Error(),
	})
	return origErr
}

// FollowLinks recursively finds more news links on a news page
func (e *Extractor) FollowLinks(portalID int, newsURL string, depth int) {
	if depth <= 0 {
		return
	}

	res, err := http.Get(newsURL)
	if err != nil {
		return
	}
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return
	}

	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		link, ok := s.Attr("href")
		if !ok {
			return
		}

		// Resolve relative URLs
		base, _ := url.Parse(newsURL)
		u, err := url.Parse(link)
		if err != nil {
			return
		}
		absoluteLink := base.ResolveReference(u).String()

		if !strings.HasPrefix(absoluteLink, "http") {
			return
		}

		// Improved heuristic for news links
		isNews := strings.Contains(absoluteLink, "/noticia/") ||
			strings.Contains(absoluteLink, "/articulo/") ||
			strings.Contains(absoluteLink, "/2024/") ||
			strings.Contains(absoluteLink, "/2025/")

		if isNews {
			title := strings.TrimSpace(s.Text())
			if title == "" {
				title = absoluteLink
			}
			_, err = db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, url, found_at_depth, media_type) VALUES (?, ?, ?, ?, ?)",
				portalID, title, absoluteLink, depth, "hyperlink")

			if err == nil && depth > 1 {
				// We could recurse here but let's be careful with depth
			}
		}
	})
}

// FetchArchive scrapes the archive URL and stores links as historical news
func (e *Extractor) FetchArchive(portalID int, archiveURL string, language string, region string) {
	if strings.HasPrefix(archiveURL, "{") {
		// It's a pattern!
		var patternData struct {
			Pattern string `json:"pattern"`
			Type    string `json:"type"`
		}
		if err := json.Unmarshal([]byte(archiveURL), &patternData); err == nil && patternData.Type == "date-based" {
			fmt.Printf("    Processing date-based archive pattern for portal %d...\n", portalID)
			// Iterate last 7 days as a sample/test
			now := time.Now()
			for i := 0; i < 7; i++ {
				d := now.AddDate(0, 0, -i)
				targetURL := patternData.Pattern
				targetURL = strings.ReplaceAll(targetURL, "YYYY", d.Format("2006"))
				targetURL = strings.ReplaceAll(targetURL, "MM", d.Format("01"))
				targetURL = strings.ReplaceAll(targetURL, "DD", d.Format("02"))

				fmt.Printf("      -> Scraping date %s: %s\n", d.Format("2006-01-02"), targetURL)
				e.scrapeArchiveURL(portalID, targetURL, language, region)
			}
			return
		}
	}

	e.scrapeArchiveURL(portalID, archiveURL, language, region)
}

func (e *Extractor) scrapeArchiveURL(portalID int, archiveURL string, language string, region string) {
	if archiveURL == "" || !strings.HasPrefix(archiveURL, "http") {
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", archiveURL, nil)
	if err != nil {
		fmt.Printf("      !! Error creating request for %s: %v\n", archiveURL, err)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	res, err := client.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return
	}

	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		link, ok := s.Attr("href")
		if !ok || link == "" {
			return
		}

		base, _ := url.Parse(archiveURL)
		u, err := url.Parse(link)
		if err != nil {
			return
		}
		absoluteLink := base.ResolveReference(u).String()

		if !strings.HasPrefix(absoluteLink, "http") {
			return
		}

		// Save as historical news if it looks like an article
		title := strings.TrimSpace(s.Text())
		if title == "" {
			title = absoluteLink
		}

		// Heuristic to avoid saving common UI links as news
		if len(title) < 10 && !strings.Contains(absoluteLink, ".html") {
			return
		}

		_, err = db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, url, media_type, language, region, is_historical) VALUES (?, ?, ?, ?, ?, ?, 1)",
			portalID, title, absoluteLink, "archive_link", language, region)
		if err != nil {
			log.Printf("Error saving historical news item: %v", err)
		}
	})
}
