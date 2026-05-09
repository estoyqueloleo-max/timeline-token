package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"timeline-token/db"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/options"
	"github.com/knights-analytics/hugot/pipelines"
)

type Analyzer struct {
	session     *hugot.Session
	embPipeline *pipelines.FeatureExtractionPipeline
	nerPipeline *pipelines.TokenClassificationPipeline
}

func NewAnalyzer(embModelPath, nerModelPath string) (*Analyzer, error) {
	// Hugot model names
	hfEmbModel := "KnightsAnalytics/all-MiniLM-L6-v2"
	hfNerModel := "KnightsAnalytics/distilbert-NER" // Known-good Hugot compatible NER model

	// 1. Ensure models exist
	if _, err := os.Stat(embModelPath); os.IsNotExist(err) {
		fmt.Printf("Model %s not found. Downloading %s...\n", embModelPath, hfEmbModel)
		p, err := hugot.DownloadModel(hfEmbModel, filepath.Dir(embModelPath), hugot.NewDownloadOptions())
		if err == nil {
			fmt.Printf("  -> Downloaded %s to %s\n", hfEmbModel, p)
			embModelPath = p
		} else {
			return nil, fmt.Errorf("failed to download embedding model: %v", err)
		}
	}
	if _, err := os.Stat(nerModelPath); os.IsNotExist(err) {
		fmt.Printf("Model %s not found. Downloading %s...\n", nerModelPath, hfNerModel)
		p, err := hugot.DownloadModel(hfNerModel, filepath.Dir(nerModelPath), hugot.NewDownloadOptions())
		if err == nil {
			fmt.Printf("  -> Downloaded %s to %s\n", hfNerModel, p)
			nerModelPath = p
		} else {
			return nil, fmt.Errorf("failed to download ner model: %v", err)
		}
	}

	fmt.Printf("Using models:\n - Emb: %s\n - NER: %s\n", embModelPath, nerModelPath)

	// 2. Init Session (OpenVINO GPU)
	ovOptions := map[string]string{"device_type": "GPU"}
	session, err := hugot.NewORTSession(options.WithOpenVINO(ovOptions))
	if err != nil {
		return nil, err
	}

	// 3. Create Embeddings Pipeline
	embConfig := hugot.FeatureExtractionConfig{
		ModelPath: embModelPath,
		Name:      "embeddings",
	}
	embPipeline, err := hugot.NewPipeline(session, embConfig)
	if err != nil {
		session.Destroy()
		return nil, err
	}

	// 4. Create NER Pipeline
	nerConfig := hugot.TokenClassificationConfig{
		ModelPath: nerModelPath,
		Name:      "ner",
	}
	nerPipeline, err := hugot.NewPipeline(session, nerConfig)
	if err != nil {
		session.Destroy()
		return nil, err
	}

	return &Analyzer{
		session:     session,
		embPipeline: embPipeline,
		nerPipeline: nerPipeline,
	}, nil
}

func (a *Analyzer) Close() {
	if a.session != nil {
		a.session.Destroy()
	}
}

// GenerateEmbeddings and Entities for a news item and store it
func (a *Analyzer) AnalyzeNews(newsID int, text string) (err error) {
	// Panic protection for Hugot internals
	defer func() {
		if r := recover(); r != nil {
			log.Printf("  !! Recovered from panic in AnalyzeNews (news %d): %v", newsID, r)
			err = nil // Don't crash the loop
		}
	}()

	// 1. Run Embeddings
	embResult, err := a.embPipeline.Run([]string{text})
	if err != nil {
		return err
	}
	embeddings := embResult.GetOutput()[0].([]float32)

	buf := make([]byte, len(embeddings)*4)
	for i, f := range embeddings {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}

	_, err = db.DB.Exec("INSERT OR REPLACE INTO news_embeddings (news_id, embedding) VALUES (?, ?)", newsID, buf)
	if err != nil {
		return err
	}

	// 2. Run NER
	// 1. Check if already analyzed (Embeddings and basic NER)
	var count int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM news_entities WHERE news_id = ?", newsID).Scan(&count)

	if count == 0 {
		// Truncate to avoid potential Hugot/Tokenization out-of-bounds panics on long text
		nerText := text
		if len(nerText) > 500 {
			nerText = nerText[:500]
		}
		// Run Model-based NER if not already done
		nerResult, err := a.nerPipeline.RunPipeline([]string{nerText})
		if err != nil {
			log.Printf("Error NER for news %d: %v", newsID, err)
		} else {
			for _, entities := range nerResult.Entities { // nerResult.Entities is [][]hugot.Entity
				for _, ent := range entities {
					if ent.Entity == "PER" || ent.Entity == "LOC" || ent.Entity == "ORG" || ent.Entity == "MISC" ||
						ent.Entity == "B-PER" || ent.Entity == "B-LOC" || ent.Entity == "B-ORG" || ent.Entity == "B-MISC" {
						typeClean := strings.Split(ent.Entity, "-")
						finalType := typeClean[len(typeClean)-1]
						_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)",
							newsID, strings.ToLower(ent.Word), finalType)
					}
				}
			}
		}
	}

	// 2. Always run Regex-based "Entities" (Fast and reaches 10 types)
	foundAny := false
	// DATE detection
	dateRegex := regexp.MustCompile(`\b(\d{1,2}[\/-]\d{1,2}[\/-]\d{2,4}|january|february|march|april|may|june|july|august|september|october|november|december|enero|febrero|marzo|abril|mayo|junio|julio|agosto|septiembre|octubre|noviembre|diciembre)\b`)
	for _, m := range dateRegex.FindAllString(strings.ToLower(text), -1) {
		_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)", newsID, m, "DATE")
		foundAny = true
	}

	// MONEY detection
	moneyRegex := regexp.MustCompile(`([$€£¥]|usd|eur|gbp)\s?\d+([.,]\d+)?`)
	for _, m := range moneyRegex.FindAllString(strings.ToLower(text), -1) {
		_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)", newsID, m, "MONEY")
		foundAny = true
	}

	if foundAny {
		log.Printf("  -> Regex found entities in news %d", newsID)
	}

	// TIME detection
	timeRegex := regexp.MustCompile(`\b\d{1,2}:\d{2}\s?(am|pm|h)?\b`)
	for _, m := range timeRegex.FindAllString(strings.ToLower(text), -1) {
		_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)", newsID, m, "TIME")
	}

	// QUAN detection (Numbers > 1000 or percentages)
	quanRegex := regexp.MustCompile(`\b\d{4,}\b|\b\d+%\b`)
	for _, m := range quanRegex.FindAllString(text, -1) {
		_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)", newsID, m, "QUAN")
	}

	// URLL detection
	urlRegex := regexp.MustCompile(`(https?://|www\.)[^\s]+`)
	for _, m := range urlRegex.FindAllString(text, -1) {
		_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)", newsID, m, "URLL")
	}

	// EMAIL detection
	emailRegex := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	for _, m := range emailRegex.FindAllString(text, -1) {
		_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)", newsID, m, "EMAIL")
	}

	// PHONE detection
	phoneRegex := regexp.MustCompile(`\+?\d{1,4}[-.\s]?\(?\d{1,3}?\)?[-.\s]?\d{1,4}[-.\s]?\d{1,4}[-.\s]?\d{1,9}`)
	for _, m := range phoneRegex.FindAllString(text, -1) {
		if len(m) > 7 { // Simple heuristic to avoid small numbers
			_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)", newsID, m, "PHONE")
		}
	}

	return nil
}
