package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"timeline-token/db"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

type Extractor struct {
	fp *gofeed.Parser
}

func NewExtractor() *Extractor {
	return &Extractor{fp: gofeed.NewParser()}
}

// FetchRSS fetches and stores news from an RSS feed
func (e *Extractor) FetchRSS(portalID int, rssURL string, mediaType string, language string, region string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	feed, err := e.fp.ParseURLWithContext(rssURL, ctx)
	if err != nil {
		return err
	}

	for _, item := range feed.Items {
		_, err := db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, summary, url, published_date, media_type, language, region) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			portalID, item.Title, item.Description, item.Link, item.PublishedParsed, mediaType, language, region)
		if err != nil {
			log.Printf("Error saving news item: %v", err)
		}
	}
	return nil
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
