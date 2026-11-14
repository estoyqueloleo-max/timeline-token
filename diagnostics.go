package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type SearchDiag struct {
	Query        string `json:"query"`
	Category     string `json:"category"`
	HTTPStatus   int    `json:"http_status"`
	ResultsCount int    `json:"results_count"`
	LatencyMs    int64  `json:"latency_ms"`
	Error        string `json:"error,omitempty"`
}

type FindRSSDiag struct {
	Domain     string `json:"domain"`
	RootURL    string `json:"root_url"`
	HTTPStatus int    `json:"http_status"`
	RSSURL     string `json:"rss_url,omitempty"`
	IsRelative bool   `json:"is_relative,omitempty"`
	Error      string `json:"error,omitempty"`
}

type HomeScrapeDiag struct {
	Domain         string `json:"domain"`
	RootURL        string `json:"root_url"`
	HTTPStatus     int    `json:"http_status"`
	BodyBytes      int    `json:"body_bytes"`
	TotalATags     int    `json:"total_a_tags"`
	NewsLinks      int    `json:"news_links"`
	Classification string `json:"classification"`
	Reason         string `json:"reason,omitempty"`
	Error          string `json:"error,omitempty"`
	MCPAttempted   bool   `json:"mcp_attempted,omitempty"`
	MCPRecovered   bool   `json:"mcp_recovered,omitempty"`
	MCPArticles    int    `json:"mcp_articles,omitempty"`
	MCPRSSURL      string `json:"mcp_rss_url,omitempty"`
	MCPError       string `json:"mcp_error,omitempty"`
}

type FeedFetchDiag struct {
	PortalID   int    `json:"portal_id"`
	RSSURL     string `json:"rss_url"`
	ItemsCount int    `json:"items_count"`
	Error      string `json:"error,omitempty"`
}

type DiagSummary struct {
	TotalSearches          int `json:"total_searches"`
	EmptySearches          int `json:"empty_searches"`
	FailedSearches         int `json:"failed_searches"`
	TotalFindRSS           int `json:"total_find_rss"`
	RSSFound               int `json:"rss_found"`
	RSSRelative            int `json:"rss_relative"`
	RSSFailed              int `json:"rss_failed"`
	TotalHomeScrapes       int `json:"total_home_scrapes"`
	HomeOK                 int `json:"home_ok"`
	HomeHTTPBlocked        int `json:"home_http_blocked"`
	HomeZeroLinksJS        int `json:"home_zero_links_js_or_empty"`
	HomeZeroLinksHeuristic int `json:"home_zero_links_heuristic"`
	HomeNetworkError       int `json:"home_network_error"`
	HomeOtherHTTPError     int `json:"home_other_http_error"`
	TotalFeedFetches       int `json:"total_feed_fetches"`
	FeedOK                 int `json:"feed_ok"`
	FeedFailed             int `json:"feed_failed"`
	MCPAttempts            int `json:"mcp_attempts"`
	MCPRecoveredPortals    int `json:"mcp_recovered_portals"`
	MCPRecoveredArticles   int `json:"mcp_recovered_articles"`
	MCPRecoveredRSS        int `json:"mcp_recovered_rss"`
}

type DiagReport struct {
	mu          sync.Mutex
	GeneratedAt string           `json:"generated_at"`
	SearxURL    string           `json:"searx_url"`
	Summary     DiagSummary      `json:"summary"`
	Searches    []SearchDiag     `json:"searches"`
	FindRSS     []FindRSSDiag    `json:"find_rss"`
	HomeScrapes []HomeScrapeDiag `json:"home_scrapes"`
	FeedFetches []FeedFetchDiag  `json:"feed_fetches"`
}

func NewDiagReport(searxURL string) *DiagReport {
	return &DiagReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		SearxURL:    searxURL,
	}
}

func classifyHomeScrape(d *HomeScrapeDiag) {
	if d.Error != "" && d.HTTPStatus == 0 {
		d.Classification = "NETWORK_ERROR"
		d.Reason = d.Error
		return
	}
	switch d.HTTPStatus {
	case 401, 403, 406, 429, 451, 503:
		d.Classification = "HTTP_BLOCKED"
		d.Reason = fmt.Sprintf("Bloqueo/WAF HTTP %d", d.HTTPStatus)
		return
	}
	if d.HTTPStatus != 200 {
		d.Classification = "HTTP_OTHER_ERROR"
		d.Reason = fmt.Sprintf("HTTP %d", d.HTTPStatus)
		return
	}
	if d.NewsLinks > 0 {
		d.Classification = "OK"
		d.Reason = fmt.Sprintf("%d noticias extraídas de %d enlaces <a>", d.NewsLinks, d.TotalATags)
		return
	}
	d.Classification = "ZERO_LINKS_DOM_OR_JS"
	if d.TotalATags == 0 {
		d.Reason = fmt.Sprintf("total_a_tags=0 (%d bytes; posible JS rendering o challenge anti-bot)", d.BodyBytes)
	} else {
		d.Reason = fmt.Sprintf("total_a_tags=%d descartados por heurística URL (%d bytes)", d.TotalATags, d.BodyBytes)
	}
}

func (r *DiagReport) RecordSearch(d SearchDiag) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.Searches = append(r.Searches, d)
	r.mu.Unlock()
	if d.Error != "" {
		fmt.Printf("    [DIAG-SEARCH] ERROR query=%q cat=%s status=%d latency=%dms err=%s\n", d.Query, d.Category, d.HTTPStatus, d.LatencyMs, d.Error)
	} else if d.ResultsCount == 0 {
		fmt.Printf("    [DIAG-SEARCH] VACIO query=%q cat=%s status=%d latency=%dms results=0\n", d.Query, d.Category, d.HTTPStatus, d.LatencyMs)
	} else {
		fmt.Printf("    [DIAG-SEARCH] OK query=%q cat=%s status=%d latency=%dms results=%d\n", d.Query, d.Category, d.HTTPStatus, d.LatencyMs, d.ResultsCount)
	}
}

func (r *DiagReport) RecordFindRSS(d FindRSSDiag) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.FindRSS = append(r.FindRSS, d)
	r.mu.Unlock()
	if d.Error != "" {
		fmt.Printf("    [DIAG-RSS] FAIL %s status=%d err=%s\n", d.Domain, d.HTTPStatus, d.Error)
	} else if d.RSSURL != "" {
		if d.IsRelative {
			fmt.Printf("    [DIAG-RSS] WARN-RELATIVE %s rss=%q (ruta relativa fallará en FetchRSS)\n", d.Domain, d.RSSURL)
		} else {
			fmt.Printf("    [DIAG-RSS] OK %s rss=%s\n", d.Domain, d.RSSURL)
		}
	} else {
		fmt.Printf("    [DIAG-RSS] NONE %s status=%d\n", d.Domain, d.HTTPStatus)
	}
}

func (r *DiagReport) RecordHomeScrape(d HomeScrapeDiag) {
	if r == nil {
		return
	}
	if d.Classification == "" {
		classifyHomeScrape(&d)
	}
	r.mu.Lock()
	r.HomeScrapes = append(r.HomeScrapes, d)
	r.mu.Unlock()
	mcpSuffix := ""
	if d.MCPAttempted {
		if d.MCPRecovered {
			mcpSuffix = fmt.Sprintf(" [MCP-OK articles=%d rss=%t]", d.MCPArticles, d.MCPRSSURL != "")
		} else {
			mcpSuffix = fmt.Sprintf(" [MCP-FAIL err=%q]", d.MCPError)
		}
	}
	fmt.Printf("    [DIAG-HOME] %s domain=%s status=%d bytes=%d a_tags=%d news=%d (%s)%s\n",
		d.Classification, d.Domain, d.HTTPStatus, d.BodyBytes, d.TotalATags, d.NewsLinks, d.Reason, mcpSuffix)
}

func (r *DiagReport) RecordFeedFetch(d FeedFetchDiag) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.FeedFetches = append(r.FeedFetches, d)
	r.mu.Unlock()
	if d.Error != "" {
		fmt.Printf("    [DIAG-FEED] FAIL portal=%d rss=%q err=%s\n", d.PortalID, d.RSSURL, d.Error)
	} else {
		fmt.Printf("    [DIAG-FEED] OK portal=%d rss=%q items=%d\n", d.PortalID, d.RSSURL, d.ItemsCount)
	}
}

func (r *DiagReport) ComputeSummary() DiagSummary {
	r.mu.Lock()
	defer r.mu.Unlock()

	var s DiagSummary
	s.TotalSearches = len(r.Searches)
	for _, x := range r.Searches {
		if x.Error != "" {
			s.FailedSearches++
		} else if x.ResultsCount == 0 {
			s.EmptySearches++
		}
	}

	s.TotalFindRSS = len(r.FindRSS)
	for _, x := range r.FindRSS {
		if x.Error != "" {
			s.RSSFailed++
		} else if x.RSSURL != "" {
			s.RSSFound++
			if x.IsRelative {
				s.RSSRelative++
			}
		}
	}

	s.TotalHomeScrapes = len(r.HomeScrapes)
	for _, x := range r.HomeScrapes {
		switch x.Classification {
		case "OK":
			s.HomeOK++
		case "HTTP_BLOCKED":
			s.HomeHTTPBlocked++
		case "ZERO_LINKS_DOM_OR_JS":
			if x.TotalATags == 0 {
				s.HomeZeroLinksJS++
			} else {
				s.HomeZeroLinksHeuristic++
			}
		case "NETWORK_ERROR":
			s.HomeNetworkError++
		default:
			s.HomeOtherHTTPError++
		}
		if x.MCPAttempted {
			s.MCPAttempts++
		}
		if x.MCPRecovered {
			s.MCPRecoveredPortals++
			s.MCPRecoveredArticles += x.MCPArticles
		}
		if x.MCPRSSURL != "" {
			s.MCPRecoveredRSS++
		}
	}

	s.TotalFeedFetches = len(r.FeedFetches)
	for _, x := range r.FeedFetches {
		if x.Error != "" {
			s.FeedFailed++
		} else {
			s.FeedOK++
		}
	}

	r.Summary = s
	return s
}

func (r *DiagReport) PrintSummary() {
	s := r.ComputeSummary()
	fmt.Println("\n==================== RESUMEN DE DIAGNÓSTICO ====================")
	fmt.Printf("SearXNG (%s): %d queries | vacías=%d | errores=%d\n",
		r.SearxURL, s.TotalSearches, s.EmptySearches, s.FailedSearches)
	fmt.Printf("FindRSS: %d portales | encontrados=%d (relativos=%d) | errores=%d\n",
		s.TotalFindRSS, s.RSSFound, s.RSSRelative, s.RSSFailed)
	fmt.Printf("ScrapeHomeLinks: %d portales | OK=%d | HTTP_BLOCKED=%d | ZERO_LINKS(a=0/JS)=%d | ZERO_LINKS(heurística)=%d | NETWORK_ERROR=%d | HTTP_OTHER=%d\n",
		s.TotalHomeScrapes, s.HomeOK, s.HomeHTTPBlocked, s.HomeZeroLinksJS, s.HomeZeroLinksHeuristic, s.HomeNetworkError, s.HomeOtherHTTPError)
	fmt.Printf("MCP Fallback: intentos=%d | portales recuperados=%d | artículos=%d | RSS rescatados=%d\n",
		s.MCPAttempts, s.MCPRecoveredPortals, s.MCPRecoveredArticles, s.MCPRecoveredRSS)
	fmt.Printf("FetchRSS: %d feeds | OK=%d | FAIL=%d\n",
		s.TotalFeedFetches, s.FeedOK, s.FeedFailed)
	fmt.Println("================================================================")
}

func (r *DiagReport) Save(path string) error {
	r.ComputeSummary()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

