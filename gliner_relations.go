package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"hugot-gliner2/pkg/gliner"
	"hugot-gliner2/pkg/ortinit"
)

// DefaultNewsRelationLabels defines the pre-configured GLiNER2 relation schema
// tailored for politics, institutional analysis, economy, and current affairs.
var DefaultNewsRelationLabels = []string{
	"criticó a",
	"apoyó a",
	"se reunió con",
	"es investigado por",
	"fue condenado por",
	"fue arrestado por",
	"demandó a",
	"impuso sanciones a",
	"firmó un acuerdo con",
	"votó a favor de",
	"vetó",
	"declaró la guerra a",
	"es aliado de",
	"dimitió de",
	"fue despedido de",
	"trabaja en",
	"adquirió",
	"invirtió en",
	"fue multada por",
	"anunció",
}

// LoadRelationSchemaLabels reads a custom relation list from relations_schema.json
// if present, falling back to DefaultNewsRelationLabels.
func LoadRelationSchemaLabels(customPath string) []string {
	if customPath == "" {
		customPath = "relations_schema.json"
	}
	b, err := os.ReadFile(customPath)
	if err == nil {
		var labels []string
		if json.Unmarshal(b, &labels) == nil && len(labels) > 0 {
			return labels
		}
	}
	return DefaultNewsRelationLabels
}

// ExtractGLiNERRelations runs the native Go hugot-gliner2 pipeline on representative
// candidate articles and persists extracted (head, relation, tail) triples into news_relations.
func ExtractGLiNERRelations(database *sql.DB, glinerDir string, maxItems int) (int, error) {
	if database == nil {
		return 0, fmt.Errorf("nil database")
	}
	if glinerDir == "" {
		glinerDir = "/home/jose/workspace/sandbox/gemini/hugot-gliner2"
	}

	encoderPath := filepath.Join(glinerDir, "encoder.onnx")
	countPath := filepath.Join(glinerDir, "count_embed.onnx")
	safetensorsPath := filepath.Join(glinerDir, "gliner_classifiers.safetensors")
	tokenizerPath := filepath.Join(glinerDir, "tokenizer_out", "tokenizer.json")
	promptPath := filepath.Join(glinerDir, "tests", "testdata", "prompt_ids.json")

	for _, required := range []string{encoderPath, countPath, safetensorsPath, tokenizerPath} {
		if _, err := os.Stat(required); err != nil {
			return 0, fmt.Errorf("missing GLiNER2 asset %s: %w", required, err)
		}
	}

	candidates, err := SelectCandidateNewsForGLiNER(database, maxItems)
	if err != nil {
		return 0, fmt.Errorf("select GLiNER2 candidates: %w", err)
	}
	if len(candidates) == 0 {
		fmt.Println("[GLiNER2] No pending candidate news items for relation extraction.")
		return 0, nil
	}

	if err := ortinit.SetupONNX(); err != nil {
		return 0, fmt.Errorf("init ONNX Runtime: %w", err)
	}

	labels := LoadRelationSchemaLabels("relations_schema.json")
	fmt.Printf("[GLiNER2] Initializing Cross-Encoder pipeline (%d relation labels) from %s...\n", len(labels), glinerDir)

	pipeline, err := gliner.NewPipeline(
		encoderPath,
		countPath,
		safetensorsPath,
		tokenizerPath,
		promptPath,
		labels,
		nil,
	)
	if err != nil {
		return 0, fmt.Errorf("create GLiNER2 pipeline: %w", err)
	}
	defer pipeline.Close()

	stmt, err := database.Prepare(`
		INSERT OR IGNORE INTO news_relations (
			news_id, head_entity, relation, tail_entity, head_score, tail_score, event_date
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for i, item := range candidates {
		text := strings.TrimSpace(item.Title)
		if item.Summary != "" {
			summary := strings.TrimSpace(item.Summary)
			if len([]rune(summary)) > 260 {
				runes := []rune(summary)
				summary = string(runes[:260])
			}
			text = text + ". " + summary
		}

		_, rels, _, _, err := pipeline.ExtractFromText(text)
		if err != nil {
			log.Printf("[GLiNER2] news_id=%d error: %v", item.ID, err)
			continue
		}

		for _, r := range rels {
			res, execErr := stmt.Exec(
				item.ID,
				strings.TrimSpace(r.Head.Text),
				r.Label,
				strings.TrimSpace(r.Tail.Text),
				r.Head.Score,
				r.Tail.Score,
				item.EventDate,
			)
			if execErr != nil {
				continue
			}
			if aff, _ := res.RowsAffected(); aff > 0 {
				inserted++
			}
		}

		if (i+1)%10 == 0 || i+1 == len(candidates) {
			fmt.Printf("[GLiNER2] Processed %d/%d articles -> %d relations saved\n", i+1, len(candidates), inserted)
		}
	}

	// Refresh top_relations links on daily_concepts now that new triples are persisted
	if err := RefreshDailyConceptRelations(database); err != nil {
		log.Printf("[GLiNER2] warning refreshing top_relations: %v", err)
	}

	return inserted, nil
}

// RefreshDailyConceptRelations updates daily_concepts.top_relations from news_relations.
func RefreshDailyConceptRelations(database *sql.DB) error {
	relByDate := loadRelationsByDate(database)
	if len(relByDate) == 0 {
		return nil
	}

	rows, err := database.Query("SELECT id, event_date, concept, COALESCE(sample_titles, '[]') FROM daily_concepts")
	if err != nil {
		return err
	}

	type conceptRow struct {
		id      int64
		evDate  string
		concept string
		samples []SampleTitleItem
	}
	var updates []conceptRow
	for rows.Next() {
		var r conceptRow
		var rawSamples string
		if err := rows.Scan(&r.id, &r.evDate, &r.concept, &rawSamples); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(rawSamples), &r.samples)
		updates = append(updates, r)
	}
	rows.Close()

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare("UPDATE daily_concepts SET top_relations = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?")
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, u := range updates {
		matched := matchRelationsForConcept(relByDate[u.evDate], u.concept, u.samples)
		if len(matched) == 0 {
			continue
		}
		b, _ := json.Marshal(matched)
		if _, err := stmt.Exec(string(b), u.id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

