package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"timeline-token/db"
)

func RunPipeline() {
	delayFlag := flag.Duration("delay", 1*time.Second, "Delay between search queries (e.g., 10s for relaxed search)")
	llmURLFlag := flag.String("llm", "http://192.168.1.4:4000", "Local LLM endpoint for archive detection")
	modelNameFlag := flag.String("model", "qwen3", "Local LLM model name")
	modeFlag := flag.String("mode", "live", "Execution mode: live, historical, or both")
	serveFlag := flag.Bool("serve", false, "Start a local HTTP server for the interactive semantic map")
	portFlag := flag.String("port", "8080", "Port for the local HTTP server")
	resumeFlag := flag.Bool("resume", false, "Skip discovery and analysis for existing data")
	testArchiveURL := flag.String("test-archive-url", "", "Test archive detection for a specific URL")
	flag.Parse()

	if *testArchiveURL != "" {
		fmt.Printf("--- Testing Archive Detection for: %s ---\n", *testArchiveURL)
		fmt.Printf("Using LLM: %s (Model: %s)\n\n", *llmURLFlag, *modelNameFlag)
		res := analyzePortalForArchive(*testArchiveURL, *llmURLFlag, *modelNameFlag)
		fmt.Printf("\nFINAL RESULT: %s\n", res)
		return
	}

	if *serveFlag {
		fmt.Printf("Regenerating interactive map template...\n")
		GenerateVisualization()
		fmt.Printf("Starting local HTTP server at http://localhost:%s\n", *portFlag)
		fmt.Printf("Open your browser at: http://localhost:%s/semantic_map.html\n", *portFlag)
		http.Handle("/", http.FileServer(http.Dir(".")))
		log.Fatal(http.ListenAndServe(":"+*portFlag, nil))
		return
	}

	// 1. Initialize DB
	err := db.InitDB("./news_central.db")
	if err != nil {
		log.Fatalf("Error initializing DB: %v", err)
	}
	db.ExecuteSchema("./db/schema.sql")

	// 2. Discover Portals with Multi-language Queries
	crawler := NewCrawler("http://192.168.1.4:8080", *delayFlag)
	fmt.Printf("Step 1: Discovering Portals (Global & Local) with delay %v...\n", *delayFlag)

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

	if *resumeFlag {
		fmt.Println("Step 1 (Resume): Skipping SearchPortal discovery.")
	} else {
		for _, config := range searchConfigs {
			fmt.Printf("\nBuscando portales en región '%s' (idioma: %s)...\n", config.region, config.lang)
			results, _ := crawler.SearchPortals(config.queries, config.category)
			fmt.Printf("  Encontrados %d resultados. Extrayendo RSS...\n", len(results))
			for _, r := range results {
				domain := crawler.ExtractDomain(r.URL)
				if domain == "" {
					continue
				}

				// Normalize portal to root
				rootURL := r.URL
				if u, err := url.Parse(r.URL); err == nil {
					rootURL = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
				}

				name := crawler.CleanDomainName(domain)

				rss, _ := crawler.FindRSS(rootURL)
				if rss != "" {
					fmt.Printf("    + %s -> RSS found at root: %s\n", domain, rss)
				}

				// 1. Insert/Update Portal based on domain
				db.DB.Exec("INSERT OR IGNORE INTO portals (name, domain, base_url, rss_url, language, region) VALUES (?, ?, ?, ?, ?, ?)",
					name, domain, rootURL, rss, config.lang, config.region)

				// Get the portal ID
				var portalID int
				err := db.DB.QueryRow("SELECT id FROM portals WHERE domain = ?", domain).Scan(&portalID)
				if err != nil {
					continue
				}

				// 2. Initial Home Scrape (Discovery Seed) - ONLY once per session per domain
				if !scrapedDomains[domain] {
					fmt.Printf("    [Discovery]: Scrapeando portada de %s...\n", domain)
					homeNews, _ := crawler.ScrapeHomeLinks(rootURL)
					for _, hn := range homeNews {
						hash := fmt.Sprintf("%s|%s", strings.ToLower(hn.Title), domain)
						db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, url, news_hash, media_type, language, region) VALUES (?, ?, ?, ?, ?, ?, ?)",
							portalID, hn.Title, hn.URL, hash, "home_discovery", config.lang, config.region)
					}
					scrapedDomains[domain] = true
				}

				// 3. Insert the actual search result as a "seed" news item
				if r.URL != rootURL && r.URL != rootURL+"/" {
					hash := fmt.Sprintf("%s|%s", strings.ToLower(r.Title), domain)
					db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, url, news_hash, media_type, language, region) VALUES (?, ?, ?, ?, ?, ?, ?)",
						portalID, r.Title, r.URL, hash, "search_seed", config.lang, config.region)
				}
			}
		}
	}

	// 2.1 Discover YouTube Global News
	if !*resumeFlag {
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
				// Try to guess language
				lang := "en"
				if strings.Contains(strings.ToLower(r.Title), "noticias") || strings.Contains(strings.ToLower(r.Title), "hoy") {
					lang = "es"
				}
				fmt.Printf("    + YT Video (%s): %s\n", lang, r.Title)
				db.DB.Exec("INSERT OR IGNORE INTO news (title, url, media_type, language) VALUES (?, ?, ?, ?)",
					r.Title, r.URL, "youtube", lang)
			}
		}
	}

	// 2.2 Archive Detection for portals
	if (*modeFlag == "live" || *modeFlag == "both") && !*resumeFlag {
		DetectArchives(*llmURLFlag, *modelNameFlag)
	}

	// 3. Extract News
	extractor := NewExtractor()

	if *modeFlag == "live" || *modeFlag == "both" {
		fmt.Println("Step 2 (Live): Extracting News from RSS...")
		rows, err := db.DB.Query(`
			SELECT id, rss_url, language, region 
			FROM portals 
			WHERE rss_url IS NOT NULL 
			AND (last_crawled IS NULL OR last_crawled < datetime('now', '-12 hours'))
		`)
		if err != nil {
			log.Printf("Error querying portals: %v", err)
		} else {
			for rows.Next() {
				var id int
				var rss, lang, reg string
				rows.Scan(&id, &rss, &lang, &reg)
				fmt.Printf("Fetching RSS for portal ID %d (%s/%s)...\n", id, lang, reg)
				extractor.FetchRSS(id, rss, "rss", lang, reg)
				db.DB.Exec("UPDATE portals SET last_crawled = CURRENT_TIMESTAMP WHERE id = ?", id)
			}
			rows.Close()
		}
	}

	if *modeFlag == "historical" || *modeFlag == "both" {
		fmt.Println("Step 2 (Historical): Extracting News from Archives...")
		rows, err := db.DB.Query(`
			SELECT id, archive_url, language, region 
			FROM portals 
			WHERE has_archive = 1 AND archive_url IS NOT NULL
		`)
		if err != nil {
			log.Printf("Error querying historical portals: %v", err)
		} else {
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

	// 2.3 Systematic Homepage Discovery (Deep Crawling)
	fmt.Println("\nStep 2.3: Systematic Homepage Discovery (Deep Crawling)...")
	CrawlHomepages()

	// 3. Semantic Analysis
	fmt.Println("Step 3: Semantic Analysis...")
	// Use a path that matches what Hugot creates to avoid redundant downloads
	analyzer, err := NewAnalyzer("models/KnightsAnalytics_all-MiniLM-L6-v2", "models/KnightsAnalytics_distilbert-NER")
	if err != nil {
		log.Printf("Skip semantic analysis: %v", err)
	} else {
		defer analyzer.Close()

		// If resume, skip news that already have BOTH embeddings and entities
		query := "SELECT id, title, summary FROM news WHERE id NOT IN (SELECT news_id FROM news_embeddings)"
		if *resumeFlag {
			query = "SELECT id, title, summary FROM news WHERE id NOT IN (SELECT news_id FROM news_embeddings) OR id NOT IN (SELECT news_id FROM news_entities)"
		}

		newsRows, err := db.DB.Query(query)
		if err != nil {
			log.Printf("Error querying news for analysis: %v", err)
		} else {
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

		// 5. Final Report: Heatmap / Similarity Dissection & Clustering
		fmt.Println("\nStep 4: Similarity Dissection & Clustering...")
		PrintTopSimilarities(10)
	}

	// 6. Semantic Map Visualization
	GenerateVisualization()

	fmt.Println("\nPipeline Execution Finished.")
}

func CrawlHomepages() {
	rows, err := db.DB.Query(`
		SELECT id, name, domain, base_url, language, region 
		FROM portals
		WHERE last_home_scrape IS NULL OR last_home_scrape < datetime('now', '-2 hours')
	`)
	if err != nil {
		log.Printf("Error querying portals for home crawl: %v", err)
		return
	}
	defer rows.Close()

	cr := NewCrawler("", 1)

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

		db.DB.Exec("UPDATE portals SET last_home_scrape = CURRENT_TIMESTAMP WHERE id = ?", id)

		fmt.Printf("      Encontrados %d posibles noticias.\n", len(links))
		for _, l := range links {
			hash := fmt.Sprintf("%s|%s", strings.ToLower(l.Title), domain)
			db.DB.Exec("INSERT OR IGNORE INTO news (portal_id, title, url, news_hash, media_type, language, region) VALUES (?, ?, ?, ?, ?, ?, ?)",
				id, l.Title, l.URL, hash, "home_discovery", lang, region)
		}
	}
}

func main() {
	RunPipeline()
}
