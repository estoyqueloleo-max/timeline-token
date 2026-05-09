package db

import (
	"database/sql"
	"math"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	var err error
	// Use WAL mode and busy timeout to prevent "database is locked" errors
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	DB, err = sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}

	// Simple migrations: add columns if they don't exist
	DB.Exec("ALTER TABLE portals ADD COLUMN domain TEXT")
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_portals_domain ON portals(domain)")
	DB.Exec("ALTER TABLE portals ADD COLUMN has_archive BOOLEAN DEFAULT NULL")
	DB.Exec("ALTER TABLE portals ADD COLUMN archive_url TEXT")
	DB.Exec("ALTER TABLE news ADD COLUMN news_hash TEXT")
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_news_hash ON news(news_hash)")
	DB.Exec("ALTER TABLE news ADD COLUMN is_historical BOOLEAN DEFAULT 0")
	DB.Exec("ALTER TABLE portals ADD COLUMN last_home_scrape DATETIME")

	// NER Table
	DB.Exec(`CREATE TABLE IF NOT EXISTS news_entities (
		news_id INTEGER,
		word TEXT,
		type TEXT,
		PRIMARY KEY(news_id, word, type),
		FOREIGN KEY(news_id) REFERENCES news(id)
	)`)

	return nil
}

func ExecuteSchema(schemaPath string) error {
	content, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}

	_, err = DB.Exec(string(content))
	return err
}

// CosineSimilarity helper for semantic analysis
func CosineSimilarity(a, b []float32) float32 {
	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
