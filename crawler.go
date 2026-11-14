package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const chromeLinuxUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

type SearchResult struct {
	URL         string
	Title       string
	Summary     string
	PublishedAt string
	MediaType   string
}

type Crawler struct {
	SearxURL      string
	Delay         time.Duration
	Diag          *DiagReport
	MCP           *MCPBridge
	discoveredRSS map[string]string
}

func NewCrawler(searxURL string, delay time.Duration) *Crawler {
	return &Crawler{
		SearxURL:      searxURL,
		Delay:         delay,
		discoveredRSS: make(map[string]string),
	}
}

func (c *Crawler) GetDiscoveredRSS(portalURL string) string {
	if c == nil || c.discoveredRSS == nil {
		return ""
	}
	return c.discoveredRSS[portalURL]
}

func (c *Crawler) tryMCPFallback(portalURL string, diagEntry *HomeScrapeDiag) ([]SearchResult, bool) {
	if c == nil || c.MCP == nil {
		return nil, false
	}

	diagEntry.MCPAttempted = true
	fmt.Printf("      [MCP Fallback] Rescatando %s con mcp-web-actor...\n", portalURL)

	page, err := c.MCP.ExtractStructuredNews(context.Background(), portalURL, 25)
	if err != nil {
		diagEntry.MCPError = err.Error()
		return nil, false
	}

	if page.RSSURL != "" {
		if base, err := url.Parse(portalURL); err == nil {
			if absRSS := resolveFeedURL(base, page.RSSURL); absRSS != "" {
				diagEntry.MCPRSSURL = absRSS
				if c.discoveredRSS == nil {
					c.discoveredRSS = make(map[string]string)
				}
				c.discoveredRSS[portalURL] = absRSS
			}
		}
	}

	if len(page.Articles) == 0 {
		diagEntry.MCPError = "0 artículos extraídos por MCP"
		return nil, false
	}

	results := make([]SearchResult, 0, len(page.Articles))
	for _, a := range page.Articles {
		if strings.TrimSpace(a.URL) == "" || strings.TrimSpace(a.Title) == "" {
			continue
		}
		results = append(results, SearchResult{
			URL:         a.URL,
			Title:       a.Title,
			Summary:     a.Summary,
			PublishedAt: a.PublishedAt,
			MediaType:   "mcp_discovery",
		})
	}

	if len(results) == 0 {
		diagEntry.MCPError = "artículos MCP sin título/URL válidos"
		return nil, false
	}

	diagEntry.MCPRecovered = true
	diagEntry.MCPArticles = len(results)
	return results, true
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
	domain := c.ExtractDomain(portalURL)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", portalURL, nil)
	if err != nil {
		c.Diag.RecordHomeScrape(HomeScrapeDiag{
			Domain:  domain,
			RootURL: portalURL,
			Error:   err.Error(),
		})
		return nil, err
	}
	req.Header.Set("User-Agent", chromeLinuxUA)

	res, err := client.Do(req)
	if err != nil {
		diagEntry := HomeScrapeDiag{
			Domain:  domain,
			RootURL: portalURL,
			Error:   err.Error(),
		}
		if mcpNews, ok := c.tryMCPFallback(portalURL, &diagEntry); ok {
			c.Diag.RecordHomeScrape(diagEntry)
			return mcpNews, nil
		}
		c.Diag.RecordHomeScrape(diagEntry)
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		err := fmt.Errorf("bad status: %d", res.StatusCode)
		diagEntry := HomeScrapeDiag{
			Domain:     domain,
			RootURL:    portalURL,
			HTTPStatus: res.StatusCode,
			Error:      err.Error(),
		}
		if mcpNews, ok := c.tryMCPFallback(portalURL, &diagEntry); ok {
			c.Diag.RecordHomeScrape(diagEntry)
			return mcpNews, nil
		}
		c.Diag.RecordHomeScrape(diagEntry)
		return nil, err
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		c.Diag.RecordHomeScrape(HomeScrapeDiag{
			Domain:     domain,
			RootURL:    portalURL,
			HTTPStatus: res.StatusCode,
			Error:      err.Error(),
		})
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(bodyBytes))
	if err != nil {
		c.Diag.RecordHomeScrape(HomeScrapeDiag{
			Domain:     domain,
			RootURL:    portalURL,
			HTTPStatus: res.StatusCode,
			BodyBytes:  len(bodyBytes),
			Error:      err.Error(),
		})
		return nil, err
	}

	u, _ := url.Parse(portalURL)
	baseDomain := c.ExtractDomain(portalURL)
	var newsLinks []SearchResult
	seen := make(map[string]bool)
	totalATags := 0

	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		totalATags++
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

	diagEntry := HomeScrapeDiag{
		Domain:     domain,
		RootURL:    portalURL,
		HTTPStatus: res.StatusCode,
		BodyBytes:  len(bodyBytes),
		TotalATags: totalATags,
		NewsLinks:  len(newsLinks),
	}
	if len(newsLinks) == 0 {
		if mcpNews, ok := c.tryMCPFallback(portalURL, &diagEntry); ok {
			c.Diag.RecordHomeScrape(diagEntry)
			return mcpNews, nil
		}
	}
	c.Diag.RecordHomeScrape(diagEntry)

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
		req, err := http.NewRequest("GET", searchUrl, nil)
		if err != nil {
			c.Diag.RecordSearch(SearchDiag{Query: query, Category: categories, Error: err.Error()})
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 NewsDiscoveryBot/1.0")

		start := time.Now()
		resp, err := client.Do(req)
		latencyMs := time.Since(start).Milliseconds()
		if err != nil {
			log.Printf("Search error for query %s: %v", query, err)
			c.Diag.RecordSearch(SearchDiag{Query: query, Category: categories, LatencyMs: latencyMs, Error: err.Error()})
			continue
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			c.Diag.RecordSearch(SearchDiag{
				Query:      query,
				Category:   categories,
				HTTPStatus: resp.StatusCode,
				LatencyMs:  latencyMs,
				Error:      fmt.Sprintf("bad status: %d", resp.StatusCode),
			})
			continue
		}

		var result struct {
			Results []struct {
				URL           string `json:"url"`
				Title         string `json:"title"`
				Content       string `json:"content"`
				PublishedDate string `json:"publishedDate"`
			} `json:"results"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			c.Diag.RecordSearch(SearchDiag{
				Query:      query,
				Category:   categories,
				HTTPStatus: resp.StatusCode,
				LatencyMs:  latencyMs,
				Error:      err.Error(),
			})
			continue
		}
		resp.Body.Close()

		c.Diag.RecordSearch(SearchDiag{
			Query:        query,
			Category:     categories,
			HTTPStatus:   resp.StatusCode,
			ResultsCount: len(result.Results),
			LatencyMs:    latencyMs,
		})

		for _, r := range result.Results {
			if !seen[r.URL] {
				allResults = append(allResults, SearchResult{
					URL:         r.URL,
					Title:       r.Title,
					Summary:     strings.TrimSpace(r.Content),
					PublishedAt: strings.TrimSpace(r.PublishedDate),
					MediaType:   "search_seed",
				})
				seen[r.URL] = true
			}
		}
	}
	return allResults, nil
}

func resolveFeedURL(base *url.URL, rawHref string) string {
	trimmed := strings.TrimSpace(rawHref)
	if trimmed == "" || base == nil {
		return ""
	}
	resolved, err := base.Parse(trimmed)
	if err != nil {
		return ""
	}
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
}

func looksLikeFeedResponse(contentType string, sample []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "xml") || strings.Contains(ct, "rss") || strings.Contains(ct, "atom") {
		return true
	}
	return looksLikeXMLFeedOrSitemapBody(sample)
}

func looksLikeXMLFeedOrSitemapBody(sample []byte) bool {
	bodyLower := strings.ToLower(string(sample))
	return strings.Contains(bodyLower, "<rss") ||
		strings.Contains(bodyLower, "<feed") ||
		strings.Contains(bodyLower, "<rdf:rdf") ||
		strings.Contains(bodyLower, "<urlset") ||
		strings.Contains(bodyLower, "<sitemapindex")
}

func decodeGzipSampleIfNeeded(raw []byte) []byte {
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err == nil {
			defer gz.Close()
			out, _ := io.ReadAll(io.LimitReader(gz, 8192))
			if len(out) > 0 {
				return out
			}
		}
	}
	return raw
}

var robotsSitemapRe = regexp.MustCompile(`(?im)^sitemap:\s*(\S+)`)

// FindSitemap discovers a public XML or Google News sitemap via /robots.txt
// or common standard paths.
func (c *Crawler) FindSitemap(baseURL *url.URL, client *http.Client) string {
	if baseURL == nil || baseURL.Host == "" {
		return ""
	}
	if client == nil {
		client = &http.Client{Timeout: 6 * time.Second}
	}
	scheme := baseURL.Scheme
	if scheme == "" {
		scheme = "https"
	}

	hosts := []string{baseURL.Host}
	if !strings.HasPrefix(strings.ToLower(baseURL.Host), "www.") {
		hosts = append(hosts, "www."+baseURL.Host)
	}

	var priorityNews []string
	var otherRobots []string
	preferredHost := baseURL.Host

	for _, h := range hosts {
		robotsURL := fmt.Sprintf("%s://%s/robots.txt", scheme, h)
		req, err := http.NewRequest("GET", robotsURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", chromeLinuxUA)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var body []byte
		if resp.StatusCode == 200 {
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		}
		finalHost := h
		if resp.Request != nil && resp.Request.URL != nil && resp.Request.URL.Host != "" {
			finalHost = resp.Request.URL.Host
		}
		resp.Body.Close()

		if len(body) > 0 {
			preferredHost = finalHost
			matches := robotsSitemapRe.FindAllSubmatch(body, -1)
			for _, m := range matches {
				smURL := strings.TrimSpace(string(m[1]))
				if abs := resolveFeedURL(baseURL, smURL); abs != "" {
					low := strings.ToLower(abs)
					if strings.Contains(low, "news") || strings.Contains(low, "actualit") || strings.Contains(low, "ultima") {
						priorityNews = append(priorityNews, abs)
					} else {
						otherRobots = append(otherRobots, abs)
					}
				}
			}
			if len(priorityNews) > 0 || len(otherRobots) > 0 {
				break
			}
		}
	}

	stdNewsPaths := []string{
		"/news-sitemap.xml",
		"/sitemap-news.xml",
		"/sitemaps/new/news.xml.gz",
	}

	candidates := make([]string, 0, len(priorityNews)+len(stdNewsPaths)+len(otherRobots)+2)
	seen := make(map[string]bool)
	addCandidate := func(u string) {
		if u != "" && !seen[u] {
			seen[u] = true
			candidates = append(candidates, u)
		}
	}

	for _, u := range priorityNews {
		addCandidate(u)
	}
	for _, p := range stdNewsPaths {
		addCandidate(fmt.Sprintf("%s://%s%s", scheme, preferredHost, p))
	}
	for _, u := range otherRobots {
		addCandidate(u)
	}
	addCandidate(fmt.Sprintf("%s://%s/sitemap.xml", scheme, preferredHost))

	const maxProbes = 7
	probes := 0
	for _, cand := range candidates {
		if probes >= maxProbes {
			break
		}
		probes++

		req, err := http.NewRequest("GET", cand, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", chromeLinuxUA)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var sample []byte
		if resp.StatusCode == 200 {
			sample, _ = io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		}
		status := resp.StatusCode
		resp.Body.Close()

		if status == 200 {
			decoded := decodeGzipSampleIfNeeded(sample)
			if looksLikeXMLFeedOrSitemapBody(decoded) {
				return cand
			}
		}
	}

	return ""
}

// FindRSS simple RSS detection on a home page with sitemap fallback
func (c *Crawler) FindRSS(portalURL string) (string, error) {
	domain := c.ExtractDomain(portalURL)
	baseURL, err := url.Parse(portalURL)
	if err != nil {
		c.Diag.RecordFindRSS(FindRSSDiag{Domain: domain, RootURL: portalURL, Error: err.Error()})
		return "", err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	sitemapClient := &http.Client{Timeout: 6 * time.Second}
	req, err := http.NewRequest("GET", portalURL, nil)
	if err != nil {
		c.Diag.RecordFindRSS(FindRSSDiag{Domain: domain, RootURL: portalURL, Error: err.Error()})
		return "", err
	}
	req.Header.Set("User-Agent", chromeLinuxUA)

	res, err := client.Do(req)
	if err != nil {
		if sm := c.FindSitemap(baseURL, sitemapClient); sm != "" {
			c.Diag.RecordFindRSS(FindRSSDiag{Domain: domain, RootURL: portalURL, RSSURL: sm})
			return sm, nil
		}
		c.Diag.RecordFindRSS(FindRSSDiag{Domain: domain, RootURL: portalURL, Error: err.Error()})
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		if sm := c.FindSitemap(baseURL, sitemapClient); sm != "" {
			c.Diag.RecordFindRSS(FindRSSDiag{Domain: domain, RootURL: portalURL, HTTPStatus: res.StatusCode, RSSURL: sm})
			return sm, nil
		}
		err := fmt.Errorf("bad status: %d", res.StatusCode)
		c.Diag.RecordFindRSS(FindRSSDiag{Domain: domain, RootURL: portalURL, HTTPStatus: res.StatusCode, Error: err.Error()})
		return "", err
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		if sm := c.FindSitemap(baseURL, sitemapClient); sm != "" {
			c.Diag.RecordFindRSS(FindRSSDiag{Domain: domain, RootURL: portalURL, HTTPStatus: res.StatusCode, RSSURL: sm})
			return sm, nil
		}
		c.Diag.RecordFindRSS(FindRSSDiag{Domain: domain, RootURL: portalURL, HTTPStatus: res.StatusCode, Error: err.Error()})
		return "", err
	}

	rssURL := ""
	doc.Find("link[type='application/rss+xml'], link[type='application/atom+xml']").Each(func(i int, s *goquery.Selection) {
		if rssURL != "" {
			return
		}
		if href, ok := s.Attr("href"); ok {
			if abs := resolveFeedURL(baseURL, href); abs != "" {
				rssURL = abs
			}
		}
	})

	// Try common paths if not found in links
	if rssURL == "" {
		checkPaths := []string{"/rss", "/feed", "/rss.xml"}
		for _, p := range checkPaths {
			testURL := fmt.Sprintf("%s://%s%s", baseURL.Scheme, baseURL.Host, p)
			reqGet, err := http.NewRequest("GET", testURL, nil)
			if err != nil {
				continue
			}
			reqGet.Header.Set("User-Agent", chromeLinuxUA)

			resp, err := client.Do(reqGet)
			if err != nil {
				continue
			}
			var snippet []byte
			if resp.StatusCode == 200 {
				snippet, _ = io.ReadAll(io.LimitReader(resp.Body, 2048))
			}
			ct := resp.Header.Get("Content-Type")
			status := resp.StatusCode
			resp.Body.Close()

			if status == 200 && looksLikeFeedResponse(ct, snippet) {
				rssURL = testURL
				break
			}
		}
	}

	// Try XML/News Sitemap via robots.txt or standard paths as final fallback
	if rssURL == "" {
		if sm := c.FindSitemap(baseURL, sitemapClient); sm != "" {
			rssURL = sm
		}
	}

	isRelative := rssURL != "" && !strings.HasPrefix(rssURL, "http://") && !strings.HasPrefix(rssURL, "https://")
	c.Diag.RecordFindRSS(FindRSSDiag{
		Domain:     domain,
		RootURL:    portalURL,
		HTTPStatus: res.StatusCode,
		RSSURL:     rssURL,
		IsRelative: isRelative,
	})

	return rssURL, nil
}
