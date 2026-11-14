package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"
	"timeline-token/db"
)

type PipelineOptions struct {
	Delay        time.Duration
	LLMURL       string
	ModelName    string
	Mode         string
	Resume       bool
	Regions      string
	SkipAnalysis bool
	UseMCP       bool
	MCPBin       string
	SearxURL     string
}

func parseRegionFilter(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	allowed := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(strings.ToLower(part))
		if p != "" {
			allowed[p] = true
		}
	}
	return allowed
}

func RunPipeline() {
	delayFlag := flag.Duration("delay", 1*time.Second, "Delay between search queries (e.g., 10s for relaxed search)")
	llmURLFlag := flag.String("llm", "http://192.168.1.4:4001", "Local LLM endpoint for archive detection")
	modelNameFlag := flag.String("model", "qwen3", "Local LLM model name")
	modeFlag := flag.String("mode", "live", "Execution mode: live, historical, or both")
	serveFlag := flag.Bool("serve", false, "Start local HTTP server for the aggregated chronological dashboard")
	portFlag := flag.String("port", "8085", "Port for the local HTTP server")
	resumeFlag := flag.Bool("resume", false, "Skip discovery and analysis for existing data")
	testArchiveURL := flag.String("test-archive-url", "", "Test archive detection for a specific URL")
	regionsFlag := flag.String("regions", "", "Comma-separated region filter (e.g., es,global,fr,de,ar,latam)")
	skipAnalysisFlag := flag.Bool("skip-analysis", false, "Skip LLM archive detection and Hugot DistilBERT loop")
	useMCPFlag := flag.Bool("use-mcp", true, "Enable MCP browser fallback (mcp-web-actor) when HTTP scraping fails or returns 0 links")
	mcpBinFlag := flag.String("mcp-bin", "", "Path to mcp-web-actor binary (optional)")

	// New chronological, GLiNER2, YouTube & cron daily flags
	checkTodayFlag := flag.Bool("check-today", false, "Only check if today is already completed in daily_runs (exit 0 if completed, exit 1 if pending)")
	dailyCheckFlag := flag.Bool("daily-check", false, "Validate daily_runs for today; run daily pipeline only if not yet completed today")
	dailyRunFlag := flag.Bool("daily-run", false, "Force full daily pipeline (incremental crawl + chronological concepts + GLiNER2 + YouTube)")
	rebuildConceptsFlag := flag.Bool("rebuild-concepts", false, "Rebuild daily concepts, extract pending GLiNER2 relations and enrich YouTube without crawling")
	glinerDirFlag := flag.String("gliner-dir", "/home/jose/workspace/sandbox/gemini/hugot-gliner2", "Path to hugot-gliner2 model assets directory")
	democraciaDirFlag := flag.String("democracia-dir", "/home/jose/workspace/sandbox/youtube/democracia", "Path to democracia project containing YouTube OAuth credentials")
	glinerLimitFlag := flag.Int("gliner-limit", 45, "Max candidate articles to run through hugot-gliner2 per pass")
	ytTopFlag := flag.Int("yt-top", 6, "Top daily concepts to enrich automatically with YouTube Data API v3")
	flag.Parse()

	if *testArchiveURL != "" {
		fmt.Printf("--- Testing Archive Detection for: %s ---\n", *testArchiveURL)
		fmt.Printf("Using LLM: %s (Model: %s)\n\n", *llmURLFlag, *modelNameFlag)
		res := analyzePortalForArchive(*testArchiveURL, *llmURLFlag, *modelNameFlag)
		fmt.Printf("\nFINAL RESULT: %s\n", res)
		return
	}

	// Initialize DB for all modes (including --serve, --daily-check, --rebuild-concepts)
	if err := db.InitDB("./news_central.db"); err != nil {
		log.Fatalf("Error initializing DB: %v", err)
	}
	if err := db.ExecuteSchema("./db/schema.sql"); err != nil {
		log.Printf("Warning executing schema.sql: %v", err)
	}

	srvCfg := ServerConfig{
		Port:          *portFlag,
		GLiNERDir:     *glinerDirFlag,
		DemocraciaDir: *democraciaDirFlag,
		GLiNERLimit:   *glinerLimitFlag,
		YouTubeTopN:   *ytTopFlag,
		Delay:         *delayFlag,
		SearxURL:      resolveDefaultSearxURL(),
		LLMURL:        *llmURLFlag,
		ModelName:     *modelNameFlag,
		UseMCP:        *useMCPFlag,
		MCPBin:        *mcpBinFlag,
	}

	if *serveFlag {
		StartDashboardServer(srvCfg)
		return
	}

	if *checkTodayFlag {
		done, finishedAt, err := IsTodayCompleted(db.DB)
		if err != nil {
			log.Fatalf("Error checking daily_runs: %v", err)
		}
		if done {
			fmt.Printf("[Daily Check] Hoy ya se completó la ejecución diaria (%s). Nada pendiente.\n", finishedAt)
			return
		}
		fmt.Println("[Daily Check] Pendiente de ejecutar hoy.")
		os.Exit(1)
	}

	if *dailyCheckFlag {
		done, finishedAt, err := IsTodayCompleted(db.DB)
		if err != nil {
			log.Fatalf("Error checking daily_runs: %v", err)
		}
		if done {
			fmt.Printf("[Daily Check] Hoy ya se completó la ejecución diaria (%s). Nada pendiente.\n", finishedAt)
			return
		}
		fmt.Println("[Daily Check] Aún no hay ejecución completada para hoy. Lanzando pipeline diario...")
		if err := RunFullDailyPipeline(srvCfg); err != nil {
			log.Fatalf("Error in daily pipeline: %v", err)
		}
		return
	}

	if *dailyRunFlag {
		if err := RunFullDailyPipeline(srvCfg); err != nil {
			log.Fatalf("Error in daily pipeline: %v", err)
		}
		return
	}

	if *rebuildConceptsFlag {
		conceptsN, relsN, ytN, err := RunRebuildConceptsOnly(srvCfg)
		if err != nil {
			log.Fatalf("Error rebuilding concepts: %v", err)
		}
		fmt.Printf("\nRebuild Finished: %d daily concepts, %d new GLiNER2 relations, %d YouTube videos.\n", conceptsN, relsN, ytN)
		return
	}

	// Standard full/legacy pipeline mode
	opts := PipelineOptions{
		Delay:        *delayFlag,
		LLMURL:       *llmURLFlag,
		ModelName:    *modelNameFlag,
		Mode:         *modeFlag,
		Resume:       *resumeFlag,
		Regions:      *regionsFlag,
		SkipAnalysis: *skipAnalysisFlag,
		UseMCP:       *useMCPFlag,
		MCPBin:       *mcpBinFlag,
		SearxURL:     srvCfg.SearxURL,
	}
	ExecuteCrawlSteps(opts)

	if !*skipAnalysisFlag {
		fmt.Println("Step 3: Semantic Analysis...")
		analyzer, err := NewAnalyzer("models/KnightsAnalytics_all-MiniLM-L6-v2", "models/KnightsAnalytics_distilbert-NER")
		if err != nil {
			log.Printf("Skip Hugot MiniLM/DistilBERT analysis: %v", err)
		} else {
			defer analyzer.Close()

			query := "SELECT id, title, summary FROM news WHERE id NOT IN (SELECT news_id FROM news_embeddings)"
			if *resumeFlag {
				query = "SELECT id, title, summary FROM news WHERE id NOT IN (SELECT news_id FROM news_embeddings) OR id NOT IN (SELECT news_id FROM news_entities)"
			}

			newsRows, qErr := db.DB.Query(query)
			if qErr == nil {
				for newsRows.Next() {
					var mid int
					var title, summary string
					newsRows.Scan(&mid, &title, &summary)
					text := title + " " + summary
					if text != " " {
						analyzer.AnalyzeNews(mid, text)
					}
				}
				newsRows.Close()
			}

			fmt.Println("\nStep 4: Similarity Dissection & Clustering...")
			PrintTopSimilarities(10)
		}

		_, _, _, _ = RunRebuildConceptsOnly(srvCfg)
	} else {
		_, _, _, _ = RunRebuildConceptsOnly(srvCfg)
	}

	fmt.Println("\nPipeline Execution Finished.")
}

// ExecuteCrawlSteps runs portal discovery, RSS/sitemap extraction, and homepage crawling.
func ExecuteCrawlSteps(opts PipelineOptions) int {
	searxURL := opts.SearxURL
	if searxURL == "" {
		searxURL = resolveDefaultSearxURL()
	}
	diag := NewDiagReport(searxURL)
	crawler := NewCrawler(searxURL, opts.Delay)
	crawler.Diag = diag

	var mcpBridge *MCPBridge
	if opts.UseMCP {
		if b, bErr := NewMCPBridge(opts.MCPBin); bErr != nil {
			log.Printf("Warning: MCP fallback disabled (%v)", bErr)
		} else {
			mcpBridge = b
			defer mcpBridge.Close()
			crawler.MCP = mcpBridge
			fmt.Printf("MCP Fallback enabled: web-actor=%s\n", mcpBridge.WebActorPath())
		}
	}

	regionFilter := parseRegionFilter(opts.Regions)
	fmt.Printf("Step 1: Discovering Portals (Global & Local) with delay %v...\n", opts.Delay)

	searchConfigs := []struct {
		queries  []string
		category string
		lang     string
		region   string
	}{
		{
			queries: []string{
				"periódicos digitales españa", "últimas noticias españa hoy",
				"prensa digital regional españa", "noticias tecnología españa",
			},
			category: "news", lang: "es", region: "es",
		},
		{
			queries: []string{
				"latest world news today", "breaking news international",
				"top tech news articles 2026", "global economy trends",
			},
			category: "news", lang: "en", region: "global",
		},
		{
			queries: []string{
				"actualités france aujourd'hui", "le monde actualité direct",
			},
			category: "news", lang: "fr", region: "fr",
		},
		{
			queries: []string{
				"aktuelle nachrichten deutschland", "deutsche zeitungen online",
				"breaking news germany",
			},
			category: "news", lang: "de", region: "de",
		},
		{
			queries: []string{
				"latest news india today", "indian media news online",
				"top stories india english",
			},
			category: "news", lang: "en", region: "in",
		},
		{
			queries: []string{
				"latest news china english", "breaking news chinese media",
				"china technology news",
			},
			category: "news", lang: "en", region: "cn",
		},
		{
			queries: []string{
				"periódicos digitales argentina", "últimas noticias buenos aires",
				"diarios nacionales argentina hoy", "noticias política argentina",
			},
			category: "news", lang: "es", region: "ar",
		},
		{
			queries: []string{
				"noticias chile hoy", "periódicos colombia prensa",
				"última hora perú", "noticias brasil castellano",
			},
			category: "news", lang: "es", region: "latam",
		},
		{
			queries: []string{
				"latest news nigeria today", "south africa breaking news",
				"kenya news online english", "african business trends",
			},
			category: "news", lang: "en", region: "africa",
		},
		{
			queries: []string{
				"latest news canada english", "cbc news breaking",
				"canada politics today", "actualités canada français",
			},
			category: "news", lang: "en", region: "ca",
		},
	}

	scrapedDomains := make(map[string]bool)

	if opts.Resume {
		fmt.Println("Step 1 (Resume): Skipping SearchPortal discovery.")
	} else {
		for _, config := range searchConfigs {
			if regionFilter != nil && !regionFilter[config.region] {
				continue
			}
			fmt.Printf("\nBuscando portales en región '%s' (idioma: %s)...\n", config.region, config.lang)
			results, _ := crawler.SearchPortals(config.queries, config.category)
			fmt.Printf("  Encontrados %d resultados. Extrayendo RSS...\n", len(results))
			for _, r := range results {
				domain := crawler.ExtractDomain(r.URL)
				if domain == "" {
					continue
				}

				rootURL := r.URL
				if u, err := url.Parse(r.URL); err == nil {
					rootURL = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
				}

				name := crawler.CleanDomainName(domain)

				var rss string
				if !scrapedDomains[domain] {
					var rssErr error
					rss, rssErr = crawler.FindRSS(rootURL)
					if rssErr != nil {
						log.Printf("    [FindRSS error] %s: %v", domain, rssErr)
					} else if rss != "" {
						fmt.Printf("    + %s -> RSS found at root: %s\n", domain, rss)
					}
				}

				db.DB.Exec("INSERT OR IGNORE INTO portals (name, domain, base_url, rss_url, language, region) VALUES (?, ?, ?, ?, ?, ?)",
					name, domain, rootURL, rss, config.lang, config.region)

				var portalID int
				err := db.DB.QueryRow("SELECT id FROM portals WHERE domain = ?", domain).Scan(&portalID)
				if err != nil {
					continue
				}

				if !scrapedDomains[domain] {
					fmt.Printf("    [Discovery]: Scrapeando portada de %s...\n", domain)
					homeNews, homeErr := crawler.ScrapeHomeLinks(rootURL)
					if homeErr != nil {
						log.Printf("    [ScrapeHome error] %s: %v", domain, homeErr)
					}
					if rss == "" {
						saveRescuedPortalRSS(portalID, domain, rootURL, crawler)
					}
					saveDiscoveredNews(portalID, domain, config.lang, config.region, homeNews)
					db.DB.Exec("UPDATE portals SET last_home_scrape = CURRENT_TIMESTAMP WHERE id = ?", portalID)
					scrapedDomains[domain] = true
				}

				if r.URL != rootURL && r.URL != rootURL+"/" {
					hash := fmt.Sprintf("%s|%s", strings.ToLower(r.Title), domain)
					var pubDate any
					if r.PublishedAt != "" {
						pubDate = r.PublishedAt
					}
					db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, summary, url, news_hash, published_date, media_type, language, region) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
						portalID, r.Title, r.Summary, r.URL, hash, pubDate, "search_seed", config.lang, config.region)
				}
			}
		}
	}

	if !opts.Resume {
		fmt.Println("\nStep 1.1: Discovering Global YouTube News...")
		videoQueries := []string{
			"noticias españa hoy youtube",
			"breaking news live stream",
			"international news today youtube",
		}
		videoResults, _ := crawler.SearchPortals(videoQueries, "videos")
		fmt.Printf("  Encontrados %d videos. Filtrando Youtube...\n", len(videoResults))
		for _, r := range videoResults {
			if strings.Contains(r.URL, "youtube.com") || strings.Contains(r.URL, "youtu.be") {
				lang := "en"
				if strings.Contains(strings.ToLower(r.Title), "noticias") || strings.Contains(strings.ToLower(r.Title), "hoy") {
					lang = "es"
				}
				var pubDate any
				if r.PublishedAt != "" {
					pubDate = r.PublishedAt
				}
				db.DB.Exec("INSERT OR IGNORE INTO news (title, summary, url, published_date, media_type, language) VALUES (?, ?, ?, ?, ?, ?)",
					r.Title, r.Summary, r.URL, pubDate, "youtube", lang)
			}
		}
	}

	if (opts.Mode == "live" || opts.Mode == "both") && !opts.Resume && !opts.SkipAnalysis {
		DetectArchives(opts.LLMURL, opts.ModelName)
	}

	extractor := NewExtractor()
	extractor.Diag = diag

	if opts.Mode == "live" || opts.Mode == "both" {
		fmt.Println("Step 2 (Live): Extracting News from RSS...")
		rows, err := db.DB.Query(`
			SELECT id, rss_url, language, region 
			FROM portals 
			WHERE rss_url IS NOT NULL AND rss_url != ''
			AND (last_crawled IS NULL OR last_crawled < datetime('now', '-12 hours'))
		`)
		if err != nil {
			log.Printf("Error querying portals: %v", err)
		} else {
			for rows.Next() {
				var id int
				var rss, lang, reg string
				rows.Scan(&id, &rss, &lang, &reg)
				fmt.Printf("Fetching RSS for portal ID %d (%s/%s): %s...\n", id, lang, reg, rss)
				if err := extractor.FetchRSS(id, rss, "rss", lang, reg); err != nil {
					log.Printf("  [FetchRSS error] portal=%d url=%q: %v", id, rss, err)
				}
				db.DB.Exec("UPDATE portals SET last_crawled = CURRENT_TIMESTAMP WHERE id = ?", id)
			}
			rows.Close()
		}
	}

	if opts.Mode == "historical" || opts.Mode == "both" {
		fmt.Println("Step 2 (Historical): Extracting News from Archives...")
		rows, err := db.DB.Query(`
			SELECT id, archive_url, language, region 
			FROM portals 
			WHERE has_archive = 1 AND archive_url IS NOT NULL
		`)
		if err == nil {
			for rows.Next() {
				var id int
				var archURL, lang, reg string
				rows.Scan(&id, &archURL, &lang, &reg)
				fmt.Printf("Fetching Archive for portal ID %d (%s/%s): %s...\n", id, lang, reg, archURL)
				extractor.FetchArchive(id, archURL, lang, reg)
			}
			rows.Close()
		}
	}

	if !(opts.SkipAnalysis && !opts.Resume) || opts.Mode == "live" {
		fmt.Println("\nStep 2.3: Systematic Homepage Discovery (Deep Crawling)...")
		CrawlHomepages(diag, mcpBridge)
	}

	diag.PrintSummary()
	if err := diag.Save("./crawl_diagnostics.json"); err != nil {
		log.Printf("Error saving crawl_diagnostics.json: %v", err)
	} else {
		fmt.Println("Diagnostics saved to ./crawl_diagnostics.json")
	}

	var portalsWithNews int
	_ = db.DB.QueryRow("SELECT COUNT(DISTINCT portal_id) FROM news WHERE portal_id IS NOT NULL").Scan(&portalsWithNews)
	return portalsWithNews
}

func saveRescuedPortalRSS(portalID int, domain, portalURL string, c *Crawler) {
	if mcpRSS := c.GetDiscoveredRSS(portalURL); mcpRSS != "" {
		db.DB.Exec("UPDATE portals SET rss_url = ? WHERE id = ? AND (rss_url IS NULL OR rss_url = '')", mcpRSS, portalID)
		fmt.Printf("    + %s -> RSS rescued via MCP: %s\n", domain, mcpRSS)
	}
}

func saveDiscoveredNews(portalID int, domain, lang, region string, items []SearchResult) {
	for _, hn := range items {
		hash := fmt.Sprintf("%s|%s", strings.ToLower(hn.Title), domain)
		mediaType := hn.MediaType
		if mediaType == "" {
			mediaType = "home_discovery"
		}
		var pubDate any
		if hn.PublishedAt != "" {
			pubDate = hn.PublishedAt
		}
		db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, summary, url, news_hash, published_date, media_type, language, region) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			portalID, hn.Title, hn.Summary, hn.URL, hash, pubDate, mediaType, lang, region)
	}
}

func CrawlHomepages(diag *DiagReport, mcpBridge *MCPBridge) {
	rows, err := db.DB.Query(`
		SELECT id, name, domain, base_url, language, region 
		FROM portals
		WHERE last_home_scrape IS NULL OR last_home_scrape < datetime('now', '-6 hours')
	`)
	if err != nil {
		log.Printf("Error querying portals for home crawl: %v", err)
		return
	}
	defer rows.Close()

	cr := NewCrawler("", 1)
	cr.Diag = diag
	cr.MCP = mcpBridge

	for rows.Next() {
		var id int
		var name, domain, baseURL, lang, region string
		rows.Scan(&id, &name, &domain, &baseURL, &lang, &region)

		fmt.Printf("  -> Scrapeando portada de %s (%s)...\n", name, domain)
		links, err := cr.ScrapeHomeLinks(baseURL)
		if err != nil {
			log.Printf("      Error scrapeando %s: %v", domain, err)
			continue
		}

		saveRescuedPortalRSS(id, domain, baseURL, cr)
		db.DB.Exec("UPDATE portals SET last_home_scrape = CURRENT_TIMESTAMP WHERE id = ?", id)

		fmt.Printf("      Encontrados %d posibles noticias.\n", len(links))
		saveDiscoveredNews(id, domain, lang, region, links)
	}
}

func main() {
	RunPipeline()
}
