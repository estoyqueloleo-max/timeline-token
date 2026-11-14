package main

import (
	"os"
	"path/filepath"
	"testing"

	"timeline-token/db"
)

func TestNormalizeEventDate(t *testing.T) {
	cases := []struct {
		pub     string
		created string
		want    string
	}{
		{"2026-11-13T17:33:00", "2026-11-14 14:08:03", "2026-11-13"},
		{"2026-11-18", "2026-11-14 14:08:03", "2026-11-14"}, // future capped to created_at
		{"Hace 5 horas", "2026-11-14 14:08:03", "2026-11-14"},
		{"Education 2d ago", "2026-11-14 14:08:03", "2026-11-12"},
		{"Maps Nov 6, 2026", "2026-11-14 14:08:03", "2026-11-06"},
		{"", "2026-11-14 14:08:03", "2026-11-14"},
		{"dom", "2026-11-14 14:08:03", "2026-11-14"},
	}
	for _, tc := range cases {
		got := NormalizeEventDate(tc.pub, tc.created)
		if got != tc.want {
			t.Errorf("NormalizeEventDate(%q, %q) = %q; want %q", tc.pub, tc.created, got, tc.want)
		}
	}
}

func TestExtractConceptsFromTitle(t *testing.T) {
	hits := ExtractConceptsFromTitle("El PP cancela su campaña tras los incidentes en Congreso de los Diputados y EE.UU.", "Elperiodico")
	found := make(map[string]bool)
	for _, h := range hits {
		found[h.Display] = true
	}
	for _, expected := range []string{"PP", "Congreso de los Diputados", "Estados Unidos"} {
		if !found[expected] {
			t.Errorf("Expected concept %q in %+v", expected, hits)
		}
	}

	slugHits := ExtractConceptsFromTitle("una nueva encuesta pregunto por las elecciones y la democracia", "Contexto")
	foundSlug := make(map[string]bool)
	for _, h := range slugHits {
		foundSlug[h.Display] = true
	}
	if !foundSlug["Democracia"] || !foundSlug["Elecciones"] {
		t.Errorf("Expected Democracia and Elecciones from slug headline, got %+v", slugHits)
	}
}

func TestCompressDBSnapshot(t *testing.T) {
	origDB := db.DB
	defer func() {
		if db.DB != nil && db.DB != origDB {
			_ = db.DB.Close()
		}
		db.DB = origDB
	}()

	tmpDir := t.TempDir()
	rawPath := filepath.Join(tmpDir, "test_central.db")
	gzPath := rawPath + ".gz"

	if err := db.InitDB(rawPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO daily_runs (run_date, status, notes) VALUES ('2026-11-14', 'completed', 'snapshot-test')`); err != nil {
		t.Fatalf("insert test row failed: %v", err)
	}

	rawSize, gzSize, err := db.CompressDBSnapshot(rawPath, gzPath)
	if err != nil {
		t.Fatalf("CompressDBSnapshot failed: %v", err)
	}
	if rawSize <= 0 || gzSize <= 0 {
		t.Fatalf("expected positive sizes, got raw=%d gz=%d", rawSize, gzSize)
	}

	gzBytes, err := os.ReadFile(gzPath)
	if err != nil {
		t.Fatalf("read gz snapshot failed: %v", err)
	}
	if len(gzBytes) < 2 || gzBytes[0] != 0x1f || gzBytes[1] != 0x8b {
		t.Fatalf("invalid gzip magic header in %s", gzPath)
	}

	// Simulate fresh checkout with only .db.gz present and verify automatic restore in InitDB
	_ = db.DB.Close()
	db.DB = nil
	_ = os.Remove(rawPath)
	_ = os.Remove(rawPath + "-wal")
	_ = os.Remove(rawPath + "-shm")

	if err := db.InitDB(rawPath); err != nil {
		t.Fatalf("InitDB restore from gz failed: %v", err)
	}

	var note string
	if err := db.DB.QueryRow(`SELECT notes FROM daily_runs WHERE run_date = '2026-11-14'`).Scan(&note); err != nil {
		t.Fatalf("query restored row failed: %v", err)
	}
	if note != "snapshot-test" {
		t.Fatalf("unexpected restored note %q", note)
	}
}

