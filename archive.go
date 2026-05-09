package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"timeline-token/db"

	"github.com/PuerkitoBio/goquery"
)

type OpenAILinkResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func DetectArchives(llmURL string, modelName string) {
	fmt.Println("Step 1.5: Detecting Archives (Hemerotecas) via LLM...")

	// Get portals that haven't been checked for archives
	rows, err := db.DB.Query("SELECT id, base_url FROM portals WHERE has_archive IS NULL")
	if err != nil {
		log.Printf("Error querying portals for archives: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var baseURL string
		if err := rows.Scan(&id, &baseURL); err != nil {
			continue
		}

		// Normalize to homepage to find the Hemeroteca better
		u, _ := url.Parse(baseURL)
		homeURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

		fmt.Printf("Analyzing portal %s (home: %s) for archives...\n", baseURL, homeURL)
		archiveURL := analyzePortalForArchive(homeURL, llmURL, modelName)

		if archiveURL != "" && archiveURL != "NONE" {
			db.DB.Exec("UPDATE portals SET has_archive = 1, archive_url = ? WHERE id = ?", archiveURL, id)
			fmt.Printf(" found archive: %s\n", archiveURL)
		} else {
			db.DB.Exec("UPDATE portals SET has_archive = 0 WHERE id = ?", id)
			fmt.Println(" no archive found.")
		}
	}
}

func analyzePortalForArchive(portalURL string, llmURL string, modelName string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	reqPortal, _ := http.NewRequest("GET", portalURL, nil)
	reqPortal.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	reqPortal.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")

	res, err := client.Do(reqPortal)
	if err != nil {
		fmt.Printf("    [Error]: Failed to fetch portal URL %s: %v\n", portalURL, err)
		return "NONE"
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		fmt.Printf("    [Error]: Portal returned status %d\n", res.StatusCode)
		return "NONE"
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return "NONE"
	}

	var links []string
	baseParsed, _ := url.Parse(portalURL)

	// Keywords to pre-filter links to avoid huge prompts
	keywords := []string{"archive", "hemeroteca", "historico", "histórico", "edicion", "edición", "buscar", "fechas", "past", "back", "index", "sitemap", "archivo"}

	seen := make(map[string]bool)

	var totalLinks int
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		totalLinks++
		href, ok := s.Attr("href")
		text := strings.TrimSpace(s.Text())
		if ok && (text != "" || strings.Contains(strings.ToLower(href), "archive") || strings.Contains(strings.ToLower(href), "hemeroteca")) {
			lowerText := strings.ToLower(text)
			lowerHref := strings.ToLower(href)

			// Fast match if it seems relevant
			relevant := false
			matchedKW := ""
			for _, kw := range keywords {
				if strings.Contains(lowerText, kw) || strings.Contains(lowerHref, kw) {
					relevant = true
					matchedKW = kw
					break
				}
			}

			// Accept more links: either they match keywords OR they have 'index' or 'html' in URL
			if relevant || strings.Contains(lowerHref, "index") || strings.Contains(lowerHref, ".html") {
				if strings.HasPrefix(href, "/") {
					href = fmt.Sprintf("%s://%s%s", baseParsed.Scheme, baseParsed.Host, href)
				}
				if !seen[href] && strings.HasPrefix(href, "http") {
					links = append(links, fmt.Sprintf("- [%s](%s)", text, href))
					seen[href] = true
					if relevant {
						fmt.Printf("      * Coincidencia (KW: %s): %q -> %s\n", matchedKW, text, href)
					}
				}
			}
		}
	})

	fmt.Printf("    [Scraper]: Encontrados %d enlaces totales, %d pasaron el filtro inicial.\n", totalLinks, len(links))

	if len(links) == 0 {
		return "NONE"
	}

	// Heuristic Fallback: If we can't reach the LLM, we can pick the best link
	// A link containing both 'hemeroteca/archivo' and 'buscador/index' is a great candidate
	var bestFallback string
	for _, l := range links {
		lowL := strings.ToLower(l)
		if (strings.Contains(lowL, "hemeroteca") || strings.Contains(lowL, "archivo")) &&
			(strings.Contains(lowL, "buscador") || strings.Contains(lowL, "index") || strings.Contains(lowL, "his")) {
			// Extract URL from markdown format "- [text](url)"
			parts := strings.Split(l, "](")
			if len(parts) > 1 {
				bestFallback = strings.TrimSuffix(parts[1], ")")
				break
			}
		}
	}

	// Limit to top 100 links to avoid excessive token usage while being permissive
	if len(links) > 100 {
		links = links[:100]
	}

	linksText := strings.Join(links, "\n")
	prompt := fmt.Sprintf(`Analyze the following list of links from a news portal and identify the one that leads to the news ARCHIVE (Hemeroteca).
Look for links related to searching by date, older editions, or a general news repository.

INSTRUCTIONS:
1. If you find a direct URL, reply ONLY with the URL.
2. If you notice a date-based pattern (e.g., results are organized like /YYYY/MM/DD/), reply ONLY with: {"pattern": "https://.../YYYY/MM/DD/", "type": "date-based"}
3. If no archive link is found, reply 'NONE'.

DO NOT provide any reasoning, justification, or markdown code blocks. Reply with plain text.

Links:
%s`, linksText)

	fmt.Printf("  -> LLM Prompt: Enviando %d enlaces de %s para evaluar...\n", len(links), portalURL)

	payload := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0.1,
	}

	payloadBytes, _ := json.Marshal(payload)

	resp, err := callLLMWithRetry(llmURL+"/v1/chat/completions", payloadBytes, client, 2)
	if err != nil {
		fmt.Printf("    [Error Conexión LLM Final]: %v\n", err)
		if bestFallback != "" {
			fmt.Printf("    [Fallback]: Usando mejor candidato encontrado por reglas: %s\n", bestFallback)
			return bestFallback
		}
		return "NONE"
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("    [LLM Respuesta Cruda]: %s\n", string(body))

	var apiResp OpenAILinkResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		fmt.Printf("    [Error]: %v (body: %s)\n", err, string(body))
		return "NONE"
	}

	if len(apiResp.Choices) > 0 {
		ans := strings.TrimSpace(apiResp.Choices[0].Message.Content)

		fmt.Printf("    [LLM Texto Extraído]: %q\n", ans)

		// Clean up any potential markdown formatting the LLM might reply with
		ans = strings.Trim(ans, "`\"'")

		if strings.HasPrefix(ans, "{") && strings.Contains(ans, "pattern") {
			// It looks like a JSON pattern
			return ans
		}

		if strings.HasPrefix(ans, "http") || ans == "NONE" {
			return ans
		} else {
			fmt.Printf("    [Alerta]: LLM devolvió formato no válido.\n")
		}
	} else {
		fmt.Printf("    [Alerta]: LLM no devolvió 'Choices'.\n")
	}
	return "NONE"
}

func callLLMWithRetry(apiURL string, payload []byte, client *http.Client, retries int) (*http.Response, error) {
	client.Timeout = 90 * time.Second
	var lastErr error
	for i := 0; i <= retries; i++ {
		if i > 0 {
			fmt.Printf("    [Reintento LLM %d/%d]...\n", i, retries)
			time.Sleep(2 * time.Second)
		}

		req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		// Only retry on potential transient errors like connection reset or EOF
		errMsg := err.Error()
		if !strings.Contains(errMsg, "EOF") && !strings.Contains(errMsg, "reset") && !strings.Contains(errMsg, "timeout") {
			break
		}
	}
	return nil, lastErr
}
