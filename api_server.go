package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"timeline-token/db"
)

type ServerConfig struct {
	Port          string
	GLiNERDir     string
	DemocraciaDir string
	GLiNERLimit   int
	YouTubeTopN   int
	Delay         time.Duration
	SearxURL      string
	LLMURL        string
	ModelName     string
	UseMCP        bool
	MCPBin        string
}

type JobState struct {
	mu        sync.Mutex
	Running   bool   `json:"running"`
	Mode      string `json:"mode"`
	StartedAt string `json:"started_at"`
	LastError string `json:"last_error,omitempty"`
}

var bgJob JobState

// EnsureDailyRunStatus checks if today's local date already completed in daily_runs.
func IsTodayCompleted(database *sql.DB) (bool, string, error) {
	today := time.Now().In(time.Local).Format("2006-01-02")
	var status, finished string
	err := database.QueryRow(`
		SELECT status, COALESCE(CAST(finished_at AS TEXT), '')
		FROM daily_runs
		WHERE run_date = ?
	`, today).Scan(&status, &finished)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if status == "completed" {
		return true, finished, nil
	}
	return false, "", nil
}

// RunRebuildConceptsOnly recalculates chronological concepts, extracts pending GLiNER2 relations,
// enriches top YouTube concepts, and regenerates the dashboard HTML without recrawling portals.
func RunRebuildConceptsOnly(cfg ServerConfig) (int, int, int, error) {
	fmt.Println("=== Rebuilding Chronological Concepts, GLiNER2 Relations & YouTube Enrichments ===")
	conceptsN, err := BuildDailyConcepts(db.DB)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("BuildDailyConcepts: %w", err)
	}
	fmt.Printf("[Concepts] Built %d daily concept buckets across timeline.\n", conceptsN)

	relsN, err := ExtractGLiNERRelations(db.DB, cfg.GLiNERDir, cfg.GLiNERLimit)
	if err != nil {
		log.Printf("[GLiNER2] warning during relation extraction: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ytN, ytErr := EnrichTopConceptsWithYouTube(ctx, db.DB, cfg.DemocraciaDir, cfg.YouTubeTopN)
	if ytErr != nil {
		log.Printf("[YouTube] warning during enrichment: %v", ytErr)
	}

	GenerateVisualization()

	today := time.Now().In(time.Local).Format("2006-01-02")
	var portalsWithNews int
	_ = db.DB.QueryRow("SELECT COUNT(DISTINCT portal_id) FROM news WHERE portal_id IS NOT NULL").Scan(&portalsWithNews)
	notes := fmt.Sprintf("rebuild concepts=%d gliner_rels+%d yt+%d", conceptsN, relsN, ytN)
	_, _ = db.DB.Exec(`
		INSERT INTO daily_runs (
			run_date, status, started_at, finished_at, portals_ok,
			concepts_extracted, relations_extracted, youtube_videos_fetched, notes
		) VALUES (?, 'completed', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, ?, ?, ?)
		ON CONFLICT(run_date) DO UPDATE SET
			status = CASE WHEN daily_runs.status = 'running' THEN daily_runs.status ELSE 'completed' END,
			finished_at = CURRENT_TIMESTAMP,
			portals_ok = excluded.portals_ok,
			concepts_extracted = excluded.concepts_extracted,
			relations_extracted = daily_runs.relations_extracted + excluded.relations_extracted,
			youtube_videos_fetched = daily_runs.youtube_videos_fetched + excluded.youtube_videos_fetched,
			notes = excluded.notes
	`, today, portalsWithNews, conceptsN, relsN, ytN, notes)

	return conceptsN, relsN, ytN, nil
}

// RunFullDailyPipeline runs incremental crawl + concept rebuild + GLiNER2 + YouTube + daily_runs tracking.
func RunFullDailyPipeline(cfg ServerConfig) error {
	today := time.Now().In(time.Local).Format("2006-01-02")

	var beforeNews int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM news").Scan(&beforeNews)

	_, _ = db.DB.Exec(`
		INSERT INTO daily_runs (run_date, status, started_at, notes)
		VALUES (?, 'running', CURRENT_TIMESTAMP, 'Incremental crawl + concepts + GLiNER2 + YouTube')
		ON CONFLICT(run_date) DO UPDATE SET
			status = 'running',
			started_at = CURRENT_TIMESTAMP,
			notes = 'Incremental crawl + concepts + GLiNER2 + YouTube'
	`, today)

	// Run incremental crawl (live RSS + homepages + SearXNG discovery)
	crawlOpts := PipelineOptions{
		Delay:        cfg.Delay,
		LLMURL:       cfg.LLMURL,
		ModelName:    cfg.ModelName,
		Mode:         "live",
		Resume:       false,
		SkipAnalysis: true, // We use fast chronological concepts + GLiNER2 instead of brute-force 53k DistilBERT loop
		UseMCP:       cfg.UseMCP,
		MCPBin:       cfg.MCPBin,
		SearxURL:     cfg.SearxURL,
	}
	portalsOK := ExecuteCrawlSteps(crawlOpts)

	var afterNews int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM news").Scan(&afterNews)
	newsAdded := afterNews - beforeNews
	if newsAdded < 0 {
		newsAdded = 0
	}

	conceptsN, relsN, ytN, rebuildErr := RunRebuildConceptsOnly(cfg)
	status := "completed"
	notes := fmt.Sprintf("portals=%d news+%d concepts=%d gliner_rels=%d yt=%d", portalsOK, newsAdded, conceptsN, relsN, ytN)
	if rebuildErr != nil {
		status = "failed"
		notes = rebuildErr.Error()
	}

	_, _ = db.DB.Exec(`
		UPDATE daily_runs
		SET status = ?, finished_at = CURRENT_TIMESTAMP,
		    portals_ok = ?, news_added = ?, concepts_extracted = ?,
		    relations_extracted = ?, youtube_videos_fetched = ?, notes = ?
		WHERE run_date = ?
	`, status, portalsOK, newsAdded, conceptsN, relsN, ytN, notes, today)

	return rebuildErr
}

func StartDashboardServer(cfg ServerConfig) {
	fmt.Println("Regenerating aggregated dashboard (semantic_map.html)...")
	GenerateVisualization()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/overview", handleAPIOverview)
	mux.HandleFunc("/api/timeline", handleAPITimeline)
	mux.HandleFunc("/api/relations", handleAPIRelations)
	mux.HandleFunc("/api/graph", handleAPIGraph)
	mux.HandleFunc("/api/youtube", func(w http.ResponseWriter, r *http.Request) {
		handleAPIYouTube(w, r, cfg)
	})
	mux.HandleFunc("/api/daily-run", func(w http.ResponseWriter, r *http.Request) {
		handleAPIDailyRun(w, r, cfg)
	})

	fileServer := http.FileServer(http.Dir("."))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "semantic_map.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	addr := ":" + cfg.Port
	fmt.Printf("Starting Aggregated Dashboard Server at http://localhost:%s\n", cfg.Port)
	fmt.Printf("Open your browser at: http://localhost:%s/semantic_map.html\n", cfg.Port)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func handleAPIOverview(w http.ResponseWriter, r *http.Request) {
	var totalNews, totalPortals, totalConcepts, totalRels, totalYTVideos, totalOutliers int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM news").Scan(&totalNews)
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM portals").Scan(&totalPortals)
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM daily_concepts").Scan(&totalConcepts)
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM news_relations").Scan(&totalRels)
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM youtube_concept_videos").Scan(&totalYTVideos)
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM youtube_concept_videos WHERE outlier_ratio >= 5.0").Scan(&totalOutliers)

	type dayMeta struct {
		Date      string `json:"date"`
		Concepts  int    `json:"concepts"`
		NewsHits  int    `json:"news_hits"`
		Relations int    `json:"relations"`
	}
	var dates []dayMeta
	dRows, err := db.DB.Query(`
		SELECT dc.event_date,
		       COUNT(*) as concept_cnt,
		       COALESCE(SUM(dc.news_count), 0) as news_hits,
		       COALESCE((SELECT COUNT(*) FROM news_relations nr WHERE nr.event_date = dc.event_date), 0) as rel_cnt
		FROM daily_concepts dc
		GROUP BY dc.event_date
		ORDER BY dc.event_date DESC
		LIMIT 45
	`)
	if err == nil {
		for dRows.Next() {
			var dm dayMeta
			if dRows.Scan(&dm.Date, &dm.Concepts, &dm.NewsHits, &dm.Relations) == nil {
				dates = append(dates, dm)
			}
		}
		dRows.Close()
	}

	type dailyRunRow struct {
		RunDate    string `json:"run_date"`
		Status     string `json:"status"`
		StartedAt  string `json:"started_at"`
		FinishedAt string `json:"finished_at"`
		PortalsOK  int    `json:"portals_ok"`
		NewsAdded  int    `json:"news_added"`
		Concepts   int    `json:"concepts_extracted"`
		Relations  int    `json:"relations_extracted"`
		YTVideos   int    `json:"youtube_videos_fetched"`
		Notes      string `json:"notes"`
	}
	var runs []dailyRunRow
	rRows, rErr := db.DB.Query(`
		SELECT run_date, status, COALESCE(CAST(started_at AS TEXT), ''),
		       COALESCE(CAST(finished_at AS TEXT), ''),
		       COALESCE(portals_ok, 0), COALESCE(news_added, 0),
		       COALESCE(concepts_extracted, 0), COALESCE(relations_extracted, 0),
		       COALESCE(youtube_videos_fetched, 0), COALESCE(notes, '')
		FROM daily_runs
		ORDER BY run_date DESC
		LIMIT 15
	`)
	if rErr == nil {
		for rRows.Next() {
			var rr dailyRunRow
			if rRows.Scan(&rr.RunDate, &rr.Status, &rr.StartedAt, &rr.FinishedAt, &rr.PortalsOK, &rr.NewsAdded, &rr.Concepts, &rr.Relations, &rr.YTVideos, &rr.Notes) == nil {
				runs = append(runs, rr)
			}
		}
		rRows.Close()
	}

	bgJob.mu.Lock()
	jobCopy := struct {
		Running   bool   `json:"running"`
		Mode      string `json:"mode"`
		StartedAt string `json:"started_at"`
		LastError string `json:"last_error,omitempty"`
	}{bgJob.Running, bgJob.Mode, bgJob.StartedAt, bgJob.LastError}
	bgJob.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"stats": map[string]int{
			"news":      totalNews,
			"portals":   totalPortals,
			"concepts":  totalConcepts,
			"relations": totalRels,
			"yt_videos": totalYTVideos,
			"outliers":  totalOutliers,
		},
		"dates":      dates,
		"daily_runs": runs,
		"job":        jobCopy,
	})
}

func handleAPITimeline(w http.ResponseWriter, r *http.Request) {
	selDate := strings.TrimSpace(r.URL.Query().Get("date"))
	onlyPol := r.URL.Query().Get("only_political") == "1"
	q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))

	if selDate == "" {
		_ = db.DB.QueryRow("SELECT COALESCE(MAX(event_date), '') FROM daily_concepts").Scan(&selDate)
	}

	query := `
		SELECT dc.event_date, dc.concept, COALESCE(dc.entity_type, 'TOP'),
		       dc.news_count, dc.portal_count, dc.burst_score,
		       COALESCE(dc.sample_titles, '[]'), COALESCE(dc.top_relations, '[]'),
		       COALESCE(dc.is_political, 0),
		       COALESCE((SELECT COUNT(*) FROM youtube_concept_videos yv WHERE lower(yv.concept) = lower(dc.concept)), 0) as yt_cnt,
		       COALESCE((SELECT MAX(outlier_ratio) FROM youtube_concept_videos yv WHERE lower(yv.concept) = lower(dc.concept)), 0) as max_outlier
		FROM daily_concepts dc
		WHERE 1=1
	`
	var args []any
	if selDate != "" && selDate != "all" {
		query += " AND dc.event_date = ?"
		args = append(args, selDate)
	}
	if onlyPol {
		query += " AND dc.is_political = 1"
	}
	if q != "" {
		query += " AND (lower(dc.concept) LIKE ? OR lower(dc.sample_titles) LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	query += " ORDER BY dc.event_date DESC, (dc.portal_count * 4 + dc.news_count + dc.burst_score * 2) DESC LIMIT 120"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type conceptDTO struct {
		EventDate    string                   `json:"event_date"`
		Concept      string                   `json:"concept"`
		EntityType   string                   `json:"entity_type"`
		NewsCount    int                      `json:"news_count"`
		PortalCount  int                      `json:"portal_count"`
		BurstScore   float64                  `json:"burst_score"`
		Samples      []SampleTitleItem        `json:"samples"`
		Relations    []ConceptRelationSnippet `json:"relations"`
		IsPolitical  bool                     `json:"is_political"`
		YouTubeCount int                      `json:"youtube_count"`
		MaxOutlier   float64                  `json:"max_outlier"`
	}

	var items []conceptDTO
	for rows.Next() {
		var dto conceptDTO
		var rawSamples, rawRels string
		if err := rows.Scan(
			&dto.EventDate, &dto.Concept, &dto.EntityType,
			&dto.NewsCount, &dto.PortalCount, &dto.BurstScore,
			&rawSamples, &rawRels, &dto.IsPolitical,
			&dto.YouTubeCount, &dto.MaxOutlier,
		); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(rawSamples), &dto.Samples)
		_ = json.Unmarshal([]byte(rawRels), &dto.Relations)
		items = append(items, dto)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"selected_date": selDate,
		"concepts":      items,
	})
}

func handleAPIRelations(w http.ResponseWriter, r *http.Request) {
	evDate := strings.TrimSpace(r.URL.Query().Get("date"))
	concept := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("concept")))
	relLabel := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("relation")))

	query := `
		SELECT nr.id, COALESCE(nr.news_id, 0), nr.head_entity, nr.relation, nr.tail_entity,
		       COALESCE(nr.head_score, 0), COALESCE(nr.tail_score, 0), COALESCE(nr.event_date, ''),
		       COALESCE(n.title, ''), COALESCE(n.url, ''), COALESCE(p.name, p.domain, '')
		FROM news_relations nr
		LEFT JOIN news n ON n.id = nr.news_id
		LEFT JOIN portals p ON p.id = n.portal_id
		WHERE 1=1
	`
	var args []any
	if evDate != "" && evDate != "all" {
		query += " AND nr.event_date = ?"
		args = append(args, evDate)
	}
	if concept != "" {
		query += " AND (lower(nr.head_entity) LIKE ? OR lower(nr.tail_entity) LIKE ? OR lower(COALESCE(n.title, '')) LIKE ?)"
		like := "%" + concept + "%"
		args = append(args, like, like, like)
	}
	if relLabel != "" {
		query += " AND lower(nr.relation) = ?"
		args = append(args, relLabel)
	}
	query += " ORDER BY nr.event_date DESC, (nr.head_score + nr.tail_score) DESC LIMIT 200"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type relationDTO struct {
		ID        int64   `json:"id"`
		NewsID    int64   `json:"news_id"`
		Head      string  `json:"head"`
		Relation  string  `json:"relation"`
		Tail      string  `json:"tail"`
		HeadScore float64 `json:"head_score"`
		TailScore float64 `json:"tail_score"`
		EventDate string  `json:"event_date"`
		Title     string  `json:"title"`
		URL       string  `json:"url"`
		Portal    string  `json:"portal"`
	}

	var list []relationDTO
	for rows.Next() {
		var item relationDTO
		if rows.Scan(&item.ID, &item.NewsID, &item.Head, &item.Relation, &item.Tail, &item.HeadScore, &item.TailScore, &item.EventDate, &item.Title, &item.URL, &item.Portal) == nil {
			list = append(list, item)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"relations": list,
	})
}

func handleAPIGraph(w http.ResponseWriter, r *http.Request) {
	evDate := strings.TrimSpace(r.URL.Query().Get("date"))
	onlyPol := r.URL.Query().Get("only_political") == "1"
	q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))

	type graphNode struct {
		ID          string `json:"id"`
		Value       int    `json:"value"`
		Type        string `json:"type"`
		IsPolitical bool   `json:"is_political"`
	}
	type graphLink struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Label  string `json:"label"`
		Weight int    `json:"weight"`
		Kind   string `json:"kind"` // "gliner" | "cooccur"
	}

	nodeMap := make(map[string]*graphNode)
	ensureNode := func(label, typ string, val int, pol bool) string {
		clean := strings.TrimSpace(label)
		if clean == "" {
			return ""
		}
		if existing, ok := nodeMap[strings.ToLower(clean)]; ok {
			existing.Value += val
			if pol {
				existing.IsPolitical = true
			}
			return existing.ID
		}
		nodeMap[strings.ToLower(clean)] = &graphNode{
			ID:          clean,
			Value:       maxInt(1, val),
			Type:        typ,
			IsPolitical: pol,
		}
		return clean
	}

	var links []graphLink
	seenLink := make(map[string]bool)

	// 1. Add GLiNER2 directed relations as primary semantic graph edges
	relQuery := `
		SELECT head_entity, relation, tail_entity, COALESCE(event_date, '')
		FROM news_relations
		WHERE 1=1
	`
	var rArgs []any
	if evDate != "" && evDate != "all" {
		relQuery += " AND event_date = ?"
		rArgs = append(rArgs, evDate)
	}
	if q != "" {
		relQuery += " AND (lower(head_entity) LIKE ? OR lower(tail_entity) LIKE ? OR lower(relation) LIKE ?)"
		like := "%" + q + "%"
		rArgs = append(rArgs, like, like, like)
	}
	relQuery += " ORDER BY (head_score + tail_score) DESC LIMIT 80"

	rRows, err := db.DB.Query(relQuery, rArgs...)
	if err == nil {
		for rRows.Next() {
			var h, rel, t, d string
			if rRows.Scan(&h, &rel, &t, &d) == nil {
				hid := ensureNode(h, "REL", 4, isPoliticalConcept(strings.ToLower(h)))
				tid := ensureNode(t, "REL", 4, isPoliticalConcept(strings.ToLower(t)))
				if hid != "" && tid != "" && hid != tid {
					k := hid + "|" + rel + "|" + tid
					if !seenLink[k] {
						seenLink[k] = true
						links = append(links, graphLink{
							Source: hid,
							Target: tid,
							Label:  rel,
							Weight: 3,
							Kind:   "gliner",
						})
					}
				}
			}
		}
		rRows.Close()
	}

	// 2. Add top daily_concepts & headline co-occurrence edges between top concepts
	cQuery := `
		SELECT concept, COALESCE(entity_type, 'TOP'), news_count, portal_count, COALESCE(is_political, 0), COALESCE(sample_titles, '[]')
		FROM daily_concepts
		WHERE 1=1
	`
	var cArgs []any
	if evDate != "" && evDate != "all" {
		cQuery += " AND event_date = ?"
		cArgs = append(cArgs, evDate)
	}
	if onlyPol {
		cQuery += " AND is_political = 1"
	}
	if q != "" {
		cQuery += " AND (lower(concept) LIKE ? OR lower(sample_titles) LIKE ?)"
		like := "%" + q + "%"
		cArgs = append(cArgs, like, like)
	}
	cQuery += " ORDER BY (portal_count * 4 + news_count) DESC LIMIT 45"

	type cItem struct {
		id      string
		newsIDs map[int64]bool
	}
	var cItems []cItem

	cRows, err := db.DB.Query(cQuery, cArgs...)
	if err == nil {
		for cRows.Next() {
			var c, typ, rawSamples string
			var nCnt, pCnt int
			var isPol bool
			if cRows.Scan(&c, &typ, &nCnt, &pCnt, &isPol, &rawSamples) == nil {
				nid := ensureNode(c, typ, nCnt, isPol)
				var samples []SampleTitleItem
				_ = json.Unmarshal([]byte(rawSamples), &samples)
				idSet := make(map[int64]bool, len(samples))
				for _, s := range samples {
					idSet[s.NewsID] = true
				}
				if nid != "" {
					cItems = append(cItems, cItem{id: nid, newsIDs: idSet})
				}
			}
		}
		cRows.Close()
	}

	for i := 0; i < len(cItems); i++ {
		for j := i + 1; j < len(cItems); j++ {
			shared := 0
			for nid := range cItems[i].newsIDs {
				if cItems[j].newsIDs[nid] {
					shared++
				}
			}
			if shared > 0 {
				k := cItems[i].id + "|co|" + cItems[j].id
				if !seenLink[k] {
					seenLink[k] = true
					links = append(links, graphLink{
						Source: cItems[i].id,
						Target: cItems[j].id,
						Label:  "co-ocurrencia (" + strconv.Itoa(shared) + ")",
						Weight: shared,
						Kind:   "cooccur",
					})
				}
			}
		}
	}

	nodes := make([]*graphNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"links": links,
	})
}

func handleAPIYouTube(w http.ResponseWriter, r *http.Request, cfg ServerConfig) {
	concept := strings.TrimSpace(r.URL.Query().Get("concept"))
	evDate := strings.TrimSpace(r.URL.Query().Get("date"))
	refresh := r.URL.Query().Get("refresh") == "1"
	onlyOutliers := r.URL.Query().Get("outliers") == "1"

	videos, err := LoadCachedYouTubeVideos(db.DB, concept, onlyOutliers)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var fetchErr string
	if concept != "" && (refresh || len(videos) == 0) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		fetched, fErr := FetchVideosForConcept(ctx, db.DB, cfg.DemocraciaDir, concept, evDate, 8)
		if fErr != nil {
			fetchErr = fErr.Error()
		} else if len(fetched) > 0 {
			videos, _ = LoadCachedYouTubeVideos(db.DB, concept, onlyOutliers)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"concept":     concept,
		"videos":      videos,
		"fetch_error": fetchErr,
	})
}

func handleAPIDailyRun(w http.ResponseWriter, r *http.Request, cfg ServerConfig) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "rebuild"
	}

	bgJob.mu.Lock()
	if bgJob.Running {
		runningMode := bgJob.Mode
		startedAt := bgJob.StartedAt
		bgJob.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{
			"running":    true,
			"mode":       runningMode,
			"started_at": startedAt,
			"message":    "Ya hay un proceso en ejecución",
		})
		return
	}
	bgJob.Running = true
	bgJob.Mode = mode
	bgJob.StartedAt = time.Now().In(time.Local).Format(time.RFC3339)
	bgJob.LastError = ""
	bgJob.mu.Unlock()

	go func() {
		var runErr error
		if mode == "daily" {
			runErr = RunFullDailyPipeline(cfg)
		} else {
			_, _, _, runErr = RunRebuildConceptsOnly(cfg)
		}
		bgJob.mu.Lock()
		bgJob.Running = false
		if runErr != nil {
			bgJob.LastError = runErr.Error()
		}
		bgJob.mu.Unlock()
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": true,
		"mode":    mode,
	})
}

func resolveDefaultSearxURL() string {
	if env := strings.TrimSpace(os.Getenv("SEARXNG_URL")); env != "" {
		return env
	}
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	if resp, err := client.Get("http://localhost:8080/search?q=test&format=json"); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return "http://localhost:8080"
		}
	}
	return "http://localhost:8080"
}
