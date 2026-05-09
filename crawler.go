package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type SearchResult struct {
	URL   string
	Title string
}

type Crawler struct {
	SearxURL string
	Delay    time.Duration
}

func NewCrawler(searxURL string, delay time.Duration) *Crawler {
	return &Crawler{SearxURL: searxURL, Delay: delay}
}

// ExtractDomain gets the root domain from a URL (e.g., https://sub.example.com/page -> example.com)
func (c *Crawler) ExtractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	return host
}

// CleanDomainName turns "elpais.com" into "El Pais"
func (c *Crawler) CleanDomainName(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) > 0 {
		name := parts[0]
		// Capitalize
		if len(name) > 0 {
			name = strings.Title(name)
		}
		return name
	}
	return domain
}

// ScrapeHomeLinks fetches a homepage and tries to find links that look like news articles
func (c *Crawler) ScrapeHomeLinks(portalURL string) ([]SearchResult, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", portalURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("bad status: %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	u, _ := url.Parse(portalURL)
	baseDomain := c.ExtractDomain(portalURL)
	var newsLinks []SearchResult
	seen := make(map[string]bool)

	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		title := strings.TrimSpace(s.Text())
		if !ok || len(title) < 10 {
			return
		}

		// Resolve relative URLs
		absoluteURL := href
		if reqURL, err := u.Parse(href); err == nil {
			absoluteURL = reqURL.String()
		} else {
			return
		}

		// Filter: Must be same domain
		if c.ExtractDomain(absoluteURL) != baseDomain {
			return
		}

		// Heuristic: News articles usually have long paths or dates or many slashes
		// and they are not just "index.html" or "/" or "category/news"
		cleanPath := strings.Trim(u.ResolveReference(func() *url.URL { tu, _ := url.Parse(href); return tu }()).Path, "/")
		parts := strings.Split(cleanPath, "/")

		isLikelyNews := false
		// 1. Path contains YYYY/MM/DD
		if strings.Contains(absoluteURL, "/2024/") || strings.Contains(absoluteURL, "/2025/") || strings.Contains(absoluteURL, "/2026/") {
			isLikelyNews = true
		}
		// 2. Path is deep (more than 2 levels)
		if len(parts) >= 2 && len(cleanPath) > 20 {
			isLikelyNews = true
		}
		// 3. Ends in .html and is long
		if strings.HasSuffix(absoluteURL, ".html") && len(cleanPath) > 15 {
			isLikelyNews = true
		}

		if isLikelyNews && !seen[absoluteURL] {
			newsLinks = append(newsLinks, SearchResult{URL: absoluteURL, Title: title})
			seen[absoluteURL] = true
		}
	})

	return newsLinks, nil
}

// SearchPortals searches for news portals using multiple queries and optional categories
func (c *Crawler) SearchPortals(queries []string, categories string) ([]SearchResult, error) {
	var allResults []SearchResult
	seen := make(map[string]bool)

	for _, query := range queries {
		// Sleep to avoid rate limiting SearXNG/Google based on user configuration
		if c.Delay > 0 {
			time.Sleep(c.Delay)
		} else {
			time.Sleep(1 * time.Second) // default backoff
		}

		catParam := ""
		if categories != "" {
			catParam = "&categories=" + categories
		}

		fmt.Printf("  -> Buscando: %q\n", query)

		searchUrl := fmt.Sprintf("%s/search?q=%s&format=json%s", c.SearxURL, url.QueryEscape(query), catParam)
		client := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest("GET", searchUrl, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 NewsDiscoveryBot/1.0")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Search error for query %s: %v", query, err)
			continue
		}

		var result struct {
			Results []struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"results"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		for _, r := range result.Results {
			if !seen[r.URL] {
				allResults = append(allResults, SearchResult{URL: r.URL, Title: r.Title})
				seen[r.URL] = true
			}
		}
	}
	return allResults, nil
}

// FindRSS simple RSS detection on a home page
func (c *Crawler) FindRSS(portalURL string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", portalURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return "", fmt.Errorf("bad status: %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return "", err
	}

	rssURL := ""
	doc.Find("link[type='application/rss+xml'], link[type='application/atom+xml']").Each(func(i int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			rssURL = href
		}
	})

	// Try common paths if not found in links
	if rssURL == "" {
		u, _ := url.Parse(portalURL)
		checkPaths := []string{"/rss", "/feed", "/rss.xml"}
		for _, p := range checkPaths {
			testURL := fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, p)
			reqHead, _ := http.NewRequest("HEAD", testURL, nil)
			reqHead.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

			resp, err := client.Do(reqHead)
			if err == nil && resp.StatusCode == 200 {
				rssURL = testURL
				break
			}
		}
	}

	return rssURL, nil
}
