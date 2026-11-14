package db

import (
	"compress/gzip"
	"database/sql"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

// ensureUncompressedDB restores dbPath from gzPath if dbPath is missing or empty.
func ensureUncompressedDB(dbPath, gzPath string) error {
	info, err := os.Stat(dbPath)
	if err == nil && info.Size() > 0 {
		return nil
	}
	if _, gzErr := os.Stat(gzPath); gzErr != nil {
		return nil
	}

	inFile, err := os.Open(gzPath)
	if err != nil {
		return err
	}
	defer inFile.Close()

	gzReader, err := gzip.NewReader(inFile)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tmpPath := dbPath + ".restore.tmp"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(outFile, gzReader); err != nil {
		outFile.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := outFile.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dbPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	log.Printf("Restored %s from %s", filepath.Base(dbPath), filepath.Base(gzPath))
	return nil
}

// CompressDBSnapshot flushes the WAL journal and writes a gzip-9 compressed snapshot to gzPath.
func CompressDBSnapshot(dbPath, gzPath string) (int64, int64, error) {
	if DB != nil {
		_, _ = DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	}

	inInfo, err := os.Stat(dbPath)
	if err != nil {
		return 0, 0, err
	}
	rawSize := inInfo.Size()

	inFile, err := os.Open(dbPath)
	if err != nil {
		return 0, 0, err
	}
	defer inFile.Close()

	tmpPath := gzPath + ".tmp"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return 0, 0, err
	}

	gzWriter, err := gzip.NewWriterLevel(outFile, gzip.BestCompression)
	if err != nil {
		outFile.Close()
		os.Remove(tmpPath)
		return 0, 0, err
	}

	if _, err := io.Copy(gzWriter, inFile); err != nil {
		gzWriter.Close()
		outFile.Close()
		os.Remove(tmpPath)
		return 0, 0, err
	}
	if err := gzWriter.Close(); err != nil {
		outFile.Close()
		os.Remove(tmpPath)
		return 0, 0, err
	}
	if err := outFile.Close(); err != nil {
		os.Remove(tmpPath)
		return 0, 0, err
	}
	if err := os.Rename(tmpPath, gzPath); err != nil {
		os.Remove(tmpPath)
		return 0, 0, err
	}

	outInfo, err := os.Stat(gzPath)
	if err != nil {
		return rawSize, 0, err
	}
	return rawSize, outInfo.Size(), nil
}

func InitDB(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	if err := ensureUncompressedDB(dbPath, dbPath+".gz"); err != nil {
		log.Printf("Warning: could not restore DB from %s.gz: %v", dbPath, err)
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

	// GLiNER2 Relations Table
	DB.Exec(`CREATE TABLE IF NOT EXISTS news_relations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		news_id INTEGER,
		head_entity TEXT NOT NULL,
		relation TEXT NOT NULL,
		tail_entity TEXT NOT NULL,
		head_score REAL,
		tail_score REAL,
		event_date TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(news_id, head_entity, relation, tail_entity),
		FOREIGN KEY(news_id) REFERENCES news(id)
	)`)
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_news_relations_date ON news_relations(event_date)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_news_relations_head ON news_relations(head_entity)")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_news_relations_tail ON news_relations(tail_entity)")

	// Chronological Daily Concepts
	DB.Exec(`CREATE TABLE IF NOT EXISTS daily_concepts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_date TEXT NOT NULL,
		concept TEXT NOT NULL,
		entity_type TEXT,
		news_count INTEGER DEFAULT 0,
		portal_count INTEGER DEFAULT 0,
		burst_score REAL DEFAULT 1.0,
		sample_titles TEXT,
		top_relations TEXT,
		is_political BOOLEAN DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(event_date, concept)
	)`)
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_daily_concepts_date ON daily_concepts(event_date)")

	// YouTube Videos & Outliers per Concept
	DB.Exec(`CREATE TABLE IF NOT EXISTS youtube_concept_videos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		concept TEXT NOT NULL,
		event_date TEXT,
		video_id TEXT NOT NULL,
		title TEXT NOT NULL,
		channel_id TEXT,
		channel_title TEXT,
		published_at TEXT,
		thumbnail_url TEXT,
		view_count INTEGER DEFAULT 0,
		like_count INTEGER DEFAULT 0,
		comment_count INTEGER DEFAULT 0,
		subscriber_count INTEGER DEFAULT 0,
		outlier_ratio REAL DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(concept, video_id)
	)`)
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_yt_concept ON youtube_concept_videos(concept)")

	// Daily Scheduler Runs
	DB.Exec(`CREATE TABLE IF NOT EXISTS daily_runs (
		run_date TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		started_at DATETIME,
		finished_at DATETIME,
		portals_ok INTEGER DEFAULT 0,
		news_added INTEGER DEFAULT 0,
		concepts_extracted INTEGER DEFAULT 0,
		relations_extracted INTEGER DEFAULT 0,
		youtube_videos_fetched INTEGER DEFAULT 0,
		notes TEXT
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
