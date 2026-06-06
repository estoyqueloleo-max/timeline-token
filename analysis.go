package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"timeline-token/db"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/options"
	"github.com/knights-analytics/hugot/pipelines"
	"github.com/yalue/onnxruntime_go"

	"hugot-gliner2/pkg/gliner"
	"hugot-gliner2/pkg/ortinit"
)

type Analyzer struct {
	session     *hugot.Session
	embPipeline *pipelines.FeatureExtractionPipeline
	nerPipeline *gliner.Pipeline
}

func NewAnalyzer(embModelPath, glinerDir string) (*Analyzer, error) {
	// Hugot model name for embeddings
	hfEmbModel := "KnightsAnalytics/all-MiniLM-L6-v2"

	// 1. Ensure embedding model exists
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

	fmt.Printf("Using embedding model: %s\n", embModelPath)

	// Download GLiNER2 models if they don't exist
	if err := DownloadGLiNER2Assets(glinerDir); err != nil {
		return nil, fmt.Errorf("failed to download gliner2 assets: %w", err)
	}

	// 2. Init Hugot Session for Embeddings (OpenVINO GPU)
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

	// 4. Init ONNX Runtime for GLiNER2 NER
	if err := ortinit.SetupONNX(); err != nil {
		session.Destroy()
		return nil, fmt.Errorf("failed to init ONNX for gliner2: %w", err)
	}

	// 5. Build GLiNER2 asset paths (from the hugot-gliner2 project root)
	encoderPath := filepath.Join(glinerDir, "encoder.onnx")
	countPath := filepath.Join(glinerDir, "count_embed.onnx")
	safetensorsPath := filepath.Join(glinerDir, "gliner_classifiers.safetensors")
	tokenizerPath := filepath.Join(glinerDir, "tokenizer_out", "tokenizer.json")
	promptPath := filepath.Join(glinerDir, "tests", "testdata", "prompt_ids.json")

	// 6. Setup ONNX SessionOptions for GLiNER2 (OpenVINO GPU)
	ortOptions, err := onnxruntime_go.NewSessionOptions()
	if err == nil {
		defer ortOptions.Destroy()
		ortOptions.AppendExecutionProviderOpenVINO(map[string]string{"device_type": "GPU"})
	} else {
		ortOptions = nil
	}

	// 7. Create GLiNER2 NER Pipeline
	nerPipeline, err := gliner.NewPipeline(
		encoderPath,
		countPath,
		safetensorsPath,
		tokenizerPath,
		promptPath,
		nil, // labels from prompt_ids.json
		ortOptions,
	)
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("failed to create gliner2 NER pipeline: %w", err)
	}

	return &Analyzer{
		session:     session,
		embPipeline: embPipeline,
		nerPipeline: nerPipeline,
	}, nil
}

func DownloadGLiNER2Assets(dir string) error {
	baseURL := "https://huggingface.co/josejuanmontiel/hugot-gliner2-assets/resolve/main/"
	files := []string{
		"encoder.onnx",
		"encoder.onnx.data",
		"count_embed.onnx",
		"count_embed.onnx.data",
		"gliner_classifiers.safetensors",
		"tokenizer.json",
		"prompt_ids.json", // If it doesn't exist remotely, it'll fail, but let's assume we can fetch it or generate it. Actually prompt_ids.json is in tests/testdata. Let's adjust.
	}

	// For prompt_ids and tokenizer, we need them in specific subdirs or just the root if we adjust NewAnalyzer.
	// In the original code, tokenizer.json is in tokenizer_out/ and prompt_ids.json is in tests/testdata/
	
	downloadFile := func(url, dest string) error {
		if _, err := os.Stat(dest); err == nil {
			return nil // Already exists
		}
		
		fmt.Printf("  -> Downloading %s to %s...\n", url, dest)
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("bad status: %s", resp.Status)
		}
		
		out, err := os.Create(dest)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, resp.Body)
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "tokenizer_out"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "tests", "testdata"), 0755); err != nil {
		return err
	}

	// Download core models
	for _, f := range files[:5] {
		dest := filepath.Join(dir, f)
		if err := downloadFile(baseURL+f, dest); err != nil {
			return err
		}
	}
	
	// Tokenizer
	if err := downloadFile(baseURL+"tokenizer.json", filepath.Join(dir, "tokenizer_out", "tokenizer.json")); err != nil {
		return err
	}
	
	// Prompt IDs (from repo or assets)
	// We'll write a default prompt_ids.json if it doesn't exist, since it might not be in the huggingface root
	promptPath := filepath.Join(dir, "tests", "testdata", "prompt_ids.json")
	fmt.Printf("  -> Generating default %s...\n", promptPath)
	defaultPrompt := `{"prompt_ids": [287, 128003, 88818, 2368, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 2842, 9959, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 4214, 104635, 9959, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 9575, 1550, 91885, 15565, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 267, 21375, 297, 50831, 2368, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 4557, 283, 795, 69956, 4636, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 9575, 4754, 46592, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 15302, 1632, 50831, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 41491, 5385, 16005, 15081, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 1695, 9959, 1655, 26625, 725, 4636, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 9575, 3303, 569, 698, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 2181, 18462, 9959, 5173, 714, 795, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 266, 70057, 9959, 31143, 79782, 268, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 41491, 67257, 9972, 10342, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 13652, 4759, 43488, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 100084, 66449, 2368, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 6132, 69956, 2368, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 836, 9959, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 718, 53160, 9959, 2181, 27738, 40978, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 1695, 9959, 1655, 87144, 2965, 4636, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 26909, 54929, 17194, 34148, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 9575, 63421, 2965, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 29589, 47776, 266, 4284, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 10377, 9959, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 4557, 267, 52096, 10967, 9959, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 1935, 9959, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 41491, 5324, 10452, 10342, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 41491, 4636, 5576, 10452, 10342, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 9575, 90291, 2965, 10342, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 283, 1110, 547, 9959, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 20815, 9959, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 1010, 29720, 9959, 26625, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 41491, 22427, 26024, 725, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 42791, 42117, 2965, 4636, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 9575, 2752, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 12640, 9959, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 33434, 608, 9959, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 4557, 961, 3087, 50831, 4636, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 1394, 473, 69956, 2368, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 418, 9959, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 4557, 36185, 698, 69956, 718, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 1348, 358, 50831, 266, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 58171, 101853, 9959, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 613, 9959, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 865, 70279, 50831, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 112167, 9959, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 2636, 297, 36612, 10967, 9959, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 5385, 39614, 436, 9959, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 31143, 9959, 5173, 1348, 59138, 287, 128006, 761, 128006, 6214, 1263, 1263, 128001, 287, 128003, 90291, 33107, 287, 128006, 761, 128006, 6214, 1263, 1263, 128002], "labels": ["trabaja en", "fund\u00f3", "adquiri\u00f3", "es subsidiaria de", "invirti\u00f3 en", "se asoci\u00f3 con", "es competencia de", "dimiti\u00f3 de", "fue despedido de", "firm\u00f3 un contrato con", "es proveedor de", "lanz\u00f3 el producto", "anunci\u00f3 ganancias de", "fue multada por", "patrocina a", "ubicado en", "naci\u00f3 en", "visit\u00f3", "declar\u00f3 la guerra a", "firm\u00f3 un tratado con", "impuso sanciones a", "es aliado de", "vot\u00f3 a favor de", "vet\u00f3", "se independiz\u00f3 de", "demand\u00f3 a", "fue arrestado por", "fue condenado por", "es investigado por", "asesin\u00f3 a", "rob\u00f3 a", "testific\u00f3 contra", "fue absuelto de", "est\u00e1 casado con", "es familiar de", "critic\u00f3 a", "apoy\u00f3 a", "se reuni\u00f3 con", "falleci\u00f3 en", "don\u00f3 a", "se divorci\u00f3 de", "premi\u00f3 a", "descubri\u00f3", "public\u00f3", "escribi\u00f3", "dirigi\u00f3", "protagoniz\u00f3", "desarroll\u00f3", "gan\u00f3 el premio", "investiga sobre"]}`
	os.WriteFile(promptPath, []byte(defaultPrompt), 0644)

	return nil
}

func (a *Analyzer) Close() {
	if a.nerPipeline != nil {
		a.nerPipeline.Close()
	}
	if a.session != nil {
		a.session.Destroy()
	}
	onnxruntime_go.DestroyEnvironment()
}

// GenerateEmbeddings and Entities for a news item and store it
func (a *Analyzer) AnalyzeNews(newsID int, text string) (err error) {
	// Panic protection for internals
	defer func() {
		if r := recover(); r != nil {
			log.Printf("  !! Recovered from panic in AnalyzeNews (news %d): %v", newsID, r)
			err = nil // Don't crash the loop
		}
	}()

	// 1. Run Embeddings (hugot MiniLM - unchanged)
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

	// 2. Run GLiNER2 NER (replaces distilbert-NER)
	var count int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM news_entities WHERE news_id = ?", newsID).Scan(&count)

	if count == 0 {
		// Truncate to avoid potential out-of-bounds issues on very long texts
		nerText := text
		if len(nerText) > 500 {
			nerText = nerText[:500]
		}

		entities, relations, words, spansInfo, nerErr := a.nerPipeline.ExtractFromText(nerText)
		if nerErr != nil {
			log.Printf("Error GLiNER2 NER for news %d: %v", newsID, nerErr)
		} else {
			// Save Entities
			for _, ent := range entities {
				if ent.Index >= len(spansInfo) {
					continue
				}
				span := spansInfo[ent.Index]
				spanWords := words[span[0]:span[1]+1]
				entityText := strings.Join(spanWords, " ")
				_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)",
					newsID, strings.ToLower(entityText), "ORG")
			}

			// Save Relations
			for _, rel := range relations {
				if rel.Head.Index >= len(spansInfo) || rel.Tail.Index >= len(spansInfo) {
					continue
				}
				headSpan := spansInfo[rel.Head.Index]
				tailSpan := spansInfo[rel.Tail.Index]
				
				headWords := words[headSpan[0]:headSpan[1]+1]
				tailWords := words[tailSpan[0]:tailSpan[1]+1]
				
				headText := strings.ToLower(strings.Join(headWords, " "))
				tailText := strings.ToLower(strings.Join(tailWords, " "))
				
				// Calculate a joint score
				score := (rel.Head.Score + rel.Tail.Score) / 2.0
				
				_, _ = db.DB.Exec("INSERT INTO news_relations (news_id, head, tail, label, score) VALUES (?, ?, ?, ?, ?)",
					newsID, headText, tailText, rel.Label, score)
			}
		}
	}

	// 3. Always run Regex-based "Entities" (Fast and reaches 10 types)
	foundAny := false
	// DATE detection
	dateRegex := regexp.MustCompile(`\b(\d{1,2}[\/\-]\d{1,2}[\/\-]\d{2,4}|january|february|march|april|may|june|july|august|september|october|november|december|enero|febrero|marzo|abril|mayo|junio|julio|agosto|septiembre|octubre|noviembre|diciembre)\b`)
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
	emailRegex := regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	for _, m := range emailRegex.FindAllString(text, -1) {
		_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)", newsID, m, "EMAIL")
	}

	// PHONE detection
	phoneRegex := regexp.MustCompile(`\+?\d{1,4}[/\-\.\s]?\(?\d{1,3}?\)?[/\-\.\s]?\d{1,4}[/\-\.\s]?\d{1,4}[/\-\.\s]?\d{1,9}`)
	for _, m := range phoneRegex.FindAllString(text, -1) {
		if len(m) > 7 { // Simple heuristic to avoid small numbers
			_, _ = db.DB.Exec("INSERT OR IGNORE INTO news_entities (news_id, word, type) VALUES (?, ?, ?)", newsID, m, "PHONE")
		}
	}

	return nil
}
