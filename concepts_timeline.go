package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// SampleTitleItem stores a representative news article for a daily concept.
type SampleTitleItem struct {
	NewsID int64  `json:"news_id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Portal string `json:"portal"`
	Region string `json:"region"`
}

// ConceptRelationSnippet summarizes a GLiNER2 triple linked to a daily concept.
type ConceptRelationSnippet struct {
	NewsID    int64   `json:"news_id"`
	Head      string  `json:"head"`
	Relation  string  `json:"relation"`
	Tail      string  `json:"tail"`
	HeadScore float64 `json:"head_score"`
	TailScore float64 `json:"tail_score"`
}

// CandidateNewsItem represents an article selected for GLiNER2 relation extraction.
type CandidateNewsItem struct {
	ID        int64
	Title     string
	Summary   string
	EventDate string
}

var (
	reISODate     = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)
	reMonthDayYr  = regexp.MustCompile(`(?i)\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Sept|Oct|Nov|Dec|January|February|March|April|June|July|August|September|October|November|December)\s+(\d{1,2}),?\s+(\d{4})\b`)
	reHaceDias    = regexp.MustCompile(`(?i)hace\s+(\d+)\s+d[ií]as?`)
	reDaysAgo     = regexp.MustCompile(`(?i)(\d+)\s*d\s+ago`)
	reTimeOnly    = regexp.MustCompile(`^\d{1,2}[h:]\d{2}(\s*(am|pm))?$`)
	reToken       = regexp.MustCompile(`[\p{L}\p{N}]+(?:[.\-'][\p{L}\p{N}]+)*\.?`)
	reMultiSpace  = regexp.MustCompile(`\s+`)
)

// NormalizeEventDate normalizes heterogeneous published_date strings into YYYY-MM-DD,
// capping invalid future timestamps using createdAt as ceiling and fallback.
func NormalizeEventDate(publishedDate, createdAt string) string {
	now := time.Now().In(time.Local)
	ceiling := parseCreatedCeiling(createdAt, now)
	fallbackDay := ceiling.Format("2006-01-02")

	raw := strings.TrimSpace(publishedDate)
	if raw == "" || raw == "<nil>" {
		return fallbackDay
	}

	// Relative date forms
	low := strings.ToLower(raw)
	if strings.Contains(low, "minuto") || strings.Contains(low, "hora") ||
		strings.Contains(low, "mins ago") || strings.Contains(low, "hours ago") ||
		strings.Contains(low, "hour ago") || strings.Contains(low, "hoy") ||
		strings.Contains(low, "today") || reTimeOnly.MatchString(low) {
		return fallbackDay
	}
	if strings.Contains(low, "ayer") || strings.Contains(low, "yesterday") {
		return ceiling.AddDate(0, 0, -1).Format("2006-01-02")
	}
	if m := reHaceDias.FindStringSubmatch(low); len(m) == 2 {
		if d, err := strconv.Atoi(m[1]); err == nil && d >= 0 && d < 3650 {
			return ceiling.AddDate(0, 0, -d).Format("2006-01-02")
		}
	}
	if m := reDaysAgo.FindStringSubmatch(low); len(m) == 2 {
		if d, err := strconv.Atoi(m[1]); err == nil && d >= 0 && d < 3650 {
			return ceiling.AddDate(0, 0, -d).Format("2006-01-02")
		}
	}

	var parsed time.Time
	var ok bool

	// Direct YYYY-MM-DD prefix
	if m := reISODate.FindStringSubmatch(raw); len(m) == 2 {
		if t, err := time.Parse("2006-01-02", m[1]); err == nil {
			parsed = t
			ok = true
		}
	}

	// Standard formats
	if !ok {
		layouts := []string{
			time.RFC3339,
			time.RFC1123Z,
			time.RFC1123,
			time.RFC822Z,
			time.RFC822,
			"2006-01-02 15:04:05 -0700 MST",
			"2006-01-02 15:04:05",
			"02 Jan 2006 15:04:05 -0700",
			"Mon, 2 Jan 2006 15:04:05 -0700",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, raw); err == nil {
				parsed = t
				ok = true
				break
			}
		}
	}

	// English month embedded strings like "Technology Nov 6, 2026"
	if !ok {
		if m := reMonthDayYr.FindStringSubmatch(raw); len(m) == 4 {
			candidate := fmt.Sprintf("%s %s %s", m[1], m[2], m[3])
			for _, layout := range []string{"Jan 2 2006", "January 2 2006"} {
				if t, err := time.Parse(layout, candidate); err == nil {
					parsed = t
					ok = true
					break
				}
			}
		}
	}

	if !ok {
		return fallbackDay
	}

	// Reject clearly bogus ancient (<1995) or future (> ceiling + 1 day) timestamps
	maxAllowed := ceiling.AddDate(0, 0, 1)
	if parsed.Year() < 1995 || parsed.After(maxAllowed) {
		return fallbackDay
	}

	return parsed.Format("2006-01-02")
}

func parseCreatedCeiling(createdAt string, fallback time.Time) time.Time {
	c := strings.TrimSpace(createdAt)
	if m := reISODate.FindStringSubmatch(c); len(m) == 2 {
		if t, err := time.ParseInLocation("2006-01-02", m[1], time.Local); err == nil {
			return t
		}
	}
	return fallback
}

var conceptStopwords = map[string]bool{
	// Spanish
	"el": true, "la": true, "los": true, "las": true, "un": true, "una": true, "unos": true, "unas": true,
	"de": true, "del": true, "al": true, "en": true, "con": true, "sin": true, "por": true, "para": true,
	"sobre": true, "tras": true, "entre": true, "hacia": true, "desde": true, "hasta": true, "contra": true,
	"durante": true, "mediante": true, "según": true, "segun": true, "ante": true, "bajo": true,
	"que": true, "qué": true, "quien": true, "quién": true, "como": true, "cómo": true, "cuando": true,
	"cuándo": true, "donde": true, "dónde": true, "cual": true, "cuál": true, "cuyo": true,
	"es": true, "son": true, "fue": true, "fueron": true, "era": true, "han": true, "has": true, "hay": true,
	"ser": true, "está": true, "esta": true, "están": true, "estan": true, "este": true, "esto": true,
	"ese": true, "esa": true, "eso": true, "aquel": true, "sus": true, "su": true, "mis": true, "mi": true,
	"nos": true, "les": true, "mas": true, "más": true, "pero": true, "sino": true, "aunque": true,
	"porque": true, "ya": true, "muy": true, "todo": true, "toda": true, "todos": true, "todas": true,
	"otro": true, "otra": true, "otros": true, "otras": true, "mismo": true, "misma": true, "cada": true,
	"nuevo": true, "nueva": true, "nuevos": true, "nuevas": true, "gran": true, "grande": true, "grandes": true,
	"primer": true, "primera": true, "último": true, "ultima": true, "última": true, "ultimas": true,
	"hoy": true, "ayer": true, "mañana": true, "ahora": true, "siempre": true, "nunca": true, "solo": true,
	"sólo": true, "hace": true, "va": true, "van": true, "puede": true, "pueden": true, "tiene": true,
	"tienen": true, "dice": true, "dicen": true, "asegura": true, "anuncia": true, "pide": true,
	"video": true, "vídeo": true, "foto": true, "directo": true, "noticias": true, "noticia": true,
	"portada": true, "editorial": true, "opinión": true, "opinion": true, "análisis": true, "analisis": true,
	"mundo": true, "país": true, "pais": true, "año": true, "años": true, "día": true, "días": true,
	"dias": true, "semana": true, "mes": true, "meses": true, "horas": true, "hora": true,
	"cmo": true, "as": true, "qu": true, "por qu": true, "fallo": true, "buque": true, "clave": true,
	// English
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true, "in": true, "on": true,
	"at": true, "to": true, "for": true, "of": true, "with": true, "by": true, "from": true, "into": true,
	"after": true, "before": true, "over": true, "under": true, "about": true, "against": true,
	"between": true, "during": true, "without": true, "within": true, "through": true, "across": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "had": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "must": true, "can": true, "do": true, "does": true, "did": true,
	"this": true, "that": true, "these": true, "those": true, "it": true, "its": true, "his": true,
	"her": true, "their": true, "our": true, "your": true, "my": true, "we": true, "they": true,
	"who": true, "what": true, "when": true, "where": true, "why": true, "how": true, "which": true,
	"new": true, "more": true, "most": true, "top": true, "best": true, "first": true, "last": true,
	"latest": true, "breaking": true, "live": true, "news": true, "today": true, "world": true,
	"year": true, "years": true, "day": true, "days": true, "time": true, "times": true, "man": true,
	"woman": true, "people": true, "says": true, "said": true, "gets": true, "get": true, "make": true,
	"makes": true, "here": true, "there": true, "all": true, "not": true, "no": true, "yes": true,
	// French / German common starters
	"le": true, "des": true, "du": true, "une": true, "et": true, "dans": true, "pour": true,
	"sur": true, "avec": true, "par": true, "est": true, "sont": true, "aux": true, "der": true, "die": true,
	"das": true, "und": true, "ist": true, "sind": true, "ein": true, "eine": true, "mit": true, "von": true,
	"für": true, "auf": true, "im": true, "am": true, "zum": true, "zur": true, "wie": true, "warum": true,
}

var articleStarters = map[string]bool{
	"el": true, "la": true, "los": true, "las": true, "un": true, "una": true, "unos": true, "unas": true,
	"de": true, "del": true, "al": true, "en": true, "con": true, "sin": true, "por": true, "para": true,
	"sobre": true, "tras": true, "entre": true, "hacia": true, "desde": true, "hasta": true, "contra": true,
	"como": true, "cómo": true, "cuando": true, "donde": true, "que": true, "qué": true, "quien": true, "quién": true,
	"the": true, "a": true, "an": true, "in": true, "on": true, "at": true, "to": true, "for": true,
	"of": true, "with": true, "by": true, "from": true, "after": true, "before": true, "over": true, "why": true, "how": true,
	"le": true, "les": true, "des": true, "du": true, "une": true, "der": true, "die": true, "das": true,
}

var bridgeConnectors = map[string]bool{
	"de": true, "del": true, "la": true, "las": true, "los": true,
	"of": true, "the": true, "for": true, "in": true, "en": true,
}

var aliasCanon = map[string]struct {
	Display     string
	Type        string
	IsPolitical bool
}{
	"ee.uu.":                   {Display: "Estados Unidos", Type: "LOC", IsPolitical: true},
	"ee.uu":                    {Display: "Estados Unidos", Type: "LOC", IsPolitical: true},
	"estados unidos":           {Display: "Estados Unidos", Type: "LOC", IsPolitical: true},
	"usa":                      {Display: "Estados Unidos", Type: "LOC", IsPolitical: true},
	"u.s.":                     {Display: "Estados Unidos", Type: "LOC", IsPolitical: true},
	"u.s":                      {Display: "Estados Unidos", Type: "LOC", IsPolitical: true},
	"ue":                       {Display: "Unión Europea", Type: "ORG", IsPolitical: true},
	"unión europea":            {Display: "Unión Europea", Type: "ORG", IsPolitical: true},
	"union europea":            {Display: "Unión Europea", Type: "ORG", IsPolitical: true},
	"european union":           {Display: "Unión Europea", Type: "ORG", IsPolitical: true},
	"ia":                       {Display: "Inteligencia Artificial", Type: "TECH", IsPolitical: false},
	"ai":                       {Display: "Inteligencia Artificial", Type: "TECH", IsPolitical: false},
	"inteligencia artificial":  {Display: "Inteligencia Artificial", Type: "TECH", IsPolitical: false},
	"artificial intelligence":  {Display: "Inteligencia Artificial", Type: "TECH", IsPolitical: false},
	"espana":                   {Display: "España", Type: "LOC", IsPolitical: true},
	"españa":                   {Display: "España", Type: "LOC", IsPolitical: true},
	"spain":                    {Display: "España", Type: "LOC", IsPolitical: true},
	"iran":                     {Display: "Irán", Type: "LOC", IsPolitical: true},
	"irán":                     {Display: "Irán", Type: "LOC", IsPolitical: true},
	"ucrania":                  {Display: "Ucrania", Type: "LOC", IsPolitical: true},
	"ukraine":                  {Display: "Ucrania", Type: "LOC", IsPolitical: true},
	"rusia":                    {Display: "Rusia", Type: "LOC", IsPolitical: true},
	"russia":                   {Display: "Rusia", Type: "LOC", IsPolitical: true},
	"china":                    {Display: "China", Type: "LOC", IsPolitical: true},
	"israel":                   {Display: "Israel", Type: "LOC", IsPolitical: true},
	"gaza":                     {Display: "Gaza", Type: "LOC", IsPolitical: true},
	"otan":                     {Display: "OTAN", Type: "ORG", IsPolitical: true},
	"nato":                     {Display: "OTAN", Type: "ORG", IsPolitical: true},
	"onu":                      {Display: "ONU", Type: "ORG", IsPolitical: true},
	"un":                       {Display: "ONU", Type: "ORG", IsPolitical: true},
}

// curatedPoliticalTerms ensures lowercase or slug-based headlines about key political/social
// issues still extract concepts useful for the Democracia / Talkback radar.
var curatedConceptKeywords = []struct {
	Pattern     string
	Display     string
	Type        string
	IsPolitical bool
}{
	{"democracia", "Democracia", "POL", true},
	{"partitocracia", "Partitocracia", "POL", true},
	{"constitución", "Constitución", "POL", true},
	{"constitucion", "Constitución", "POL", true},
	{"separación de poderes", "Separación de Poderes", "POL", true},
	{"separacion de poderes", "Separación de Poderes", "POL", true},
	{"ley electoral", "Ley Electoral", "POL", true},
	{"congreso de los diputados", "Congreso de los Diputados", "ORG", true},
	{"congreso", "Congreso", "ORG", true},
	{"senado", "Senado", "ORG", true},
	{"parlamento", "Parlamento", "ORG", true},
	{"gobierno", "Gobierno", "POL", true},
	{"elecciones", "Elecciones", "POL", true},
	{"election", "Elecciones", "POL", true},
	{"elections", "Elecciones", "POL", true},
	{"tribunal constitucional", "Tribunal Constitucional", "ORG", true},
	{"tribunal supremo", "Tribunal Supremo", "ORG", true},
	{"corrupción", "Corrupción", "POL", true},
	{"corrupcion", "Corrupción", "POL", true},
	{"presupuestos", "Presupuestos", "POL", true},
	{"pensiones", "Pensiones", "POL", true},
	{"vivienda", "Vivienda", "POL", true},
	{"inmigración", "Inmigración", "POL", true},
	{"inmigracion", "Inmigración", "POL", true},
	{"aranceles", "Aranceles", "ECON", true},
	{"tariffs", "Aranceles", "ECON", true},
	{"inflación", "Inflación", "ECON", true},
	{"inflacion", "Inflación", "ECON", true},
	{"inflation", "Inflación", "ECON", true},
	{"sanciones", "Sanciones", "POL", true},
	{"alto el fuego", "Alto el Fuego", "POL", true},
	{"ceasefire", "Alto el Fuego", "POL", true},
	{"huelga", "Huelga", "POL", true},
	{"protestas", "Protestas", "POL", true},
	{"cambio climático", "Cambio Climático", "TOP", false},
	{"cambio climatico", "Cambio Climático", "TOP", false},
	{"inteligencia artificial", "Inteligencia Artificial", "TECH", false},
	{"ciberseguridad", "Ciberseguridad", "TECH", false},
	{"impuestos", "Impuestos", "ECON", true},
	{"reforma", "Reforma", "POL", true},
	{"amnistía", "Amnistía", "POL", true},
	{"amnistia", "Amnistía", "POL", true},
	{"pedro sánchez", "Pedro Sánchez", "PER", true},
	{"pedro sanchez", "Pedro Sánchez", "PER", true},
	{"donald trump", "Donald Trump", "PER", true},
	{"javier milei", "Javier Milei", "PER", true},
	{"feijóo", "Feijóo", "PER", true},
	{"feijoo", "Feijóo", "PER", true},
	{"abascal", "Abascal", "PER", true},
	{"psoe", "PSOE", "ORG", true},
	{"vox", "VOX", "ORG", true},
	{"sumar", "Sumar", "ORG", true},
	{"podemos", "Podemos", "ORG", true},
	{"pp", "PP", "ORG", true},
}

type extractedConcept struct {
	Canonical   string
	Display     string
	EntityType  string
	IsPolitical bool
}

func stripMediaSuffix(title, portalName string) string {
	// Cut trailing media attribution " - El País", " | BBC News"
	for _, sep := range []string{" | ", " - ", " — "} {
		if idx := strings.LastIndex(title, sep); idx > len(title)/2 {
			tail := strings.TrimSpace(title[idx+len(sep):])
			if len([]rune(tail)) <= 30 {
				title = title[:idx]
				break
			}
		}
	}
	if portalName != "" && len(portalName) >= 3 {
		lowTitle := strings.ToLower(title)
		lowPortal := strings.ToLower(portalName)
		if strings.HasSuffix(lowTitle, lowPortal) && len(title) > len(portalName)+3 {
			title = strings.TrimSpace(title[:len(title)-len(portalName)])
		}
	}
	return strings.TrimSpace(title)
}

func isCapitalizedWord(w string) bool {
	if len(w) == 0 {
		return false
	}
	runes := []rune(w)
	if !unicode.IsUpper(runes[0]) {
		return false
	}
	// Accept acronyms (len >= 2 all upper) or normal TitleCase words
	return len(runes) >= 2
}

func isAllUpperWord(w string) bool {
	letters := 0
	for _, r := range w {
		if unicode.IsLetter(r) {
			if !unicode.IsUpper(r) {
				return false
			}
			letters++
		}
	}
	return letters >= 2
}

func isPoliticalConcept(lower string) bool {
	polHints := []string{
		"sánchez", "sanchez", "feijóo", "feijoo", "abascal", "trump", "milei", "putin", "zelenski",
		"macron", "starmer", "merz", "netanyahu", "maduro", "lula", "sheinbaum", "pp", "psoe", "vox",
		"podemos", "sumar", "junts", "erc", "congreso", "senado", "parlamento", "gobierno", "moncloa",
		"eleccion", "election", "voto", "vote", "democracia", "democracy", "partitocracia",
		"constitucion", "constitución", "tribunal", "supremo", "cgpj", "juez", "fiscal", "justicia",
		"corrupci", "amnist", "ley ", "reforma", "presupuesto", "impuesto", "arancel", "tariff",
		"sanci", "otan", "nato", "onu", "unión europea", "union europea", "bruselas", "washington",
		"kremlin", "kiev", "ucrania", "ukraine", "rusia", "russia", "irán", "iran", "israel", "gaza",
		"china", "taiwán", "estados unidos", "espana", "españa", "cataluña", "cataluna", "euskadi",
		"madrid", "buenos aires", "vivienda", "huelga", "protesta", "pension", "inmigraci", "migra",
		"ministro", "minister", "presidente", "president", "alcalde", "diputad", "senador", "partido",
	}
	for _, h := range polHints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	return false
}

// ExtractConceptsFromTitle extracts normalized chronological concepts from a headline.
func ExtractConceptsFromTitle(title, portalName string) []extractedConcept {
	clean := stripMediaSuffix(title, portalName)
	if clean == "" {
		return nil
	}

	seen := make(map[string]extractedConcept)
	addHit := func(display, etype string, isPol bool) {
		display = strings.TrimSpace(strings.Trim(display, ".,;:!?\"'()[]"))
		if len([]rune(display)) < 2 {
			return
		}
		// Skip pure numbers
		hasLetter := false
		for _, r := range display {
			if unicode.IsLetter(r) {
				hasLetter = true
				break
			}
		}
		if !hasLetter {
			return
		}
		canon := strings.ToLower(display)
		canon = reMultiSpace.ReplaceAllString(canon, " ")
		if conceptStopwords[canon] {
			return
		}
		if portalName != "" && strings.EqualFold(canon, strings.ToLower(portalName)) {
			return
		}
		if alias, ok := aliasCanon[canon]; ok {
			canon = strings.ToLower(alias.Display)
			display = alias.Display
			if etype == "" || etype == "TOP" {
				etype = alias.Type
			}
			isPol = isPol || alias.IsPolitical
		}
		if !isPol {
			isPol = isPoliticalConcept(canon)
		}
		if existing, exists := seen[canon]; exists {
			if isPol {
				existing.IsPolitical = true
			}
			seen[canon] = existing
			return
		}
		seen[canon] = extractedConcept{
			Canonical:   canon,
			Display:     display,
			EntityType:  etype,
			IsPolitical: isPol,
		}
	}

	// Pass 1: Curated political/social/topical concepts (also catches lowercase slugs)
	lowClean := " " + strings.ToLower(clean) + " "
	for _, kw := range curatedConceptKeywords {
		if strings.Contains(lowClean, " "+kw.Pattern+" ") ||
			strings.Contains(lowClean, " "+kw.Pattern+",") ||
			strings.Contains(lowClean, " "+kw.Pattern+".") ||
			strings.Contains(lowClean, " "+kw.Pattern+":") {
			addHit(kw.Display, kw.Type, kw.IsPolitical)
		}
	}

	// Pass 2: Capitalized proper noun spans
	type tokSpan struct {
		text string
		idx  int
	}
	matches := reToken.FindAllStringIndex(clean, -1)
	var toks []tokSpan
	for i, loc := range matches {
		toks = append(toks, tokSpan{text: clean[loc[0]:loc[1]], idx: i})
	}

	for i := 0; i < len(toks); i++ {
		w := toks[i].text
		lowW := strings.ToLower(w)
		if !isCapitalizedWord(w) || articleStarters[lowW] {
			continue
		}
		// Check if previous char before token implies sentence start
		prevGap := clean[0:matches[i][0]]
		atSentenceStart := i == 0 || strings.ContainsAny(prevGap[maxInt(0, len(prevGap)-4):], ".?!:")

		// Try to extend multi-word phrase (allows up to 2 consecutive bridge words such as "de los" / "de la")
		phrase := []string{w}
		j := i + 1
		for j < len(toks) && len(phrase) < 5 {
			between := clean[matches[j-1][1]:matches[j][0]]
			if strings.TrimSpace(between) != "" {
				break
			}
			nextW := toks[j].text
			lowNext := strings.ToLower(nextW)
			if isCapitalizedWord(nextW) && !conceptStopwords[lowNext] {
				phrase = append(phrase, nextW)
				j++
				continue
			}
			// Allow 1 connector followed by capitalized token
			if bridgeConnectors[lowNext] && j+1 < len(toks) &&
				strings.TrimSpace(clean[matches[j][1]:matches[j+1][0]]) == "" {
				w1 := toks[j+1].text
				lowW1 := strings.ToLower(w1)
				if isCapitalizedWord(w1) && !conceptStopwords[lowW1] {
					phrase = append(phrase, lowNext, w1)
					j += 2
					continue
				}
				// Allow 2 connectors (e.g., "de los", "de la") followed by capitalized token
				if bridgeConnectors[lowW1] && j+2 < len(toks) &&
					strings.TrimSpace(clean[matches[j+1][1]:matches[j+2][0]]) == "" {
					w2 := toks[j+2].text
					if isCapitalizedWord(w2) && !conceptStopwords[strings.ToLower(w2)] {
						phrase = append(phrase, lowNext, lowW1, w2)
						j += 3
						continue
					}
				}
			}
			break
		}

		if len(phrase) >= 2 {
			full := strings.Join(phrase, " ")
			addHit(full, "ENT", isPoliticalConcept(strings.ToLower(full)))
			i = j - 1
			continue
		}

		// Single capitalized token:
		// Keep if not stopword AND (not at sentence start OR acronym OR known alias OR political)
		if !conceptStopwords[lowW] {
			_, isAlias := aliasCanon[lowW]
			isAcronym := isAllUpperWord(w) && len([]rune(w)) <= 6
			isPol := isPoliticalConcept(lowW)
			if !atSentenceStart || isAlias || isAcronym || isPol {
				etype := "ENT"
				if isAcronym {
					etype = "ORG"
				}
				addHit(w, etype, isPol)
			}
		}
	}

	out := make([]extractedConcept, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// BuildDailyConcepts aggregates news items chronologically into daily_concepts,
// computes burst scores and joins GLiNER2 relations from news_relations.
func BuildDailyConcepts(database *sql.DB) (int, error) {
	if database == nil {
		return 0, fmt.Errorf("nil database")
	}

	// Ensure existing GLiNER2 relations also map to daily concepts cleanly
	relByDateAndEntity := loadRelationsByDate(database)

	rows, err := database.Query(`
		SELECT n.id, n.title, COALESCE(n.url, ''),
		       COALESCE(CAST(n.published_date AS TEXT), ''),
		       COALESCE(CAST(n.created_at AS TEXT), ''),
		       COALESCE(n.region, 'global'),
		       COALESCE(n.portal_id, 0),
		       COALESCE(p.name, p.domain, '')
		FROM news n
		LEFT JOIN portals p ON p.id = n.portal_id
		WHERE n.title IS NOT NULL AND n.title != ''
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type dayConceptBucket struct {
		EventDate    string
		Canonical    string
		DisplayVotes map[string]int
		EntityType   string
		IsPolitical  bool
		NewsCount    int
		Portals      map[int]bool
		Samples      []SampleTitleItem
		SamplePortal map[string]bool
	}

	// map[eventDate][canonical]*dayConceptBucket
	byDate := make(map[string]map[string]*dayConceptBucket)
	// track total counts across dates per concept for burst calculation
	globalConceptDays := make(map[string]map[string]int)

	for rows.Next() {
		var newsID int64
		var portalID int
		var title, rawURL, pubDate, createdAt, region, portalName string
		if err := rows.Scan(&newsID, &title, &rawURL, &pubDate, &createdAt, &region, &portalID, &portalName); err != nil {
			continue
		}
		evDate := NormalizeEventDate(pubDate, createdAt)
		hits := ExtractConceptsFromTitle(title, portalName)
		if len(hits) == 0 {
			continue
		}

		if byDate[evDate] == nil {
			byDate[evDate] = make(map[string]*dayConceptBucket)
		}

		for _, h := range hits {
			b, ok := byDate[evDate][h.Canonical]
			if !ok {
				b = &dayConceptBucket{
					EventDate:    evDate,
					Canonical:    h.Canonical,
					DisplayVotes: make(map[string]int),
					EntityType:   h.EntityType,
					IsPolitical:  h.IsPolitical,
					Portals:      make(map[int]bool),
					SamplePortal: make(map[string]bool),
				}
				byDate[evDate][h.Canonical] = b
			}
			b.NewsCount++
			b.DisplayVotes[h.Display]++
			if h.IsPolitical {
				b.IsPolitical = true
			}
			if portalID > 0 {
				b.Portals[portalID] = true
			}
			if len(b.Samples) < 6 {
				pKey := strings.ToLower(portalName)
				if pKey == "" || !b.SamplePortal[pKey] || len(b.Samples) < 3 {
					b.Samples = append(b.Samples, SampleTitleItem{
						NewsID: newsID,
						Title:  title,
						URL:    rawURL,
						Portal: portalName,
						Region: region,
					})
					if pKey != "" {
						b.SamplePortal[pKey] = true
					}
				}
			}

			if globalConceptDays[h.Canonical] == nil {
				globalConceptDays[h.Canonical] = make(map[string]int)
			}
			globalConceptDays[h.Canonical][evDate]++
		}
	}

	// Calculate baseline frequency per concept
	conceptAvg := make(map[string]float64, len(globalConceptDays))
	for canon, dayCounts := range globalConceptDays {
		total := 0
		for _, c := range dayCounts {
			total += c
		}
		conceptAvg[canon] = float64(total) / float64(maxInt(1, len(dayCounts)))
	}

	// Sort dates descending
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	tx, err := database.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec("DELETE FROM daily_concepts"); err != nil {
		return 0, err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO daily_concepts (
			event_date, concept, entity_type, news_count, portal_count,
			burst_score, sample_titles, top_relations, is_political, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	totalSaved := 0
	for _, d := range dates {
		dayMap := byDate[d]
		type rankedConcept struct {
			bucket  *dayConceptBucket
			display string
			burst   float64
			score   float64
		}
		var list []rankedConcept
		for canon, b := range dayMap {
			if b.NewsCount < 2 && len(dayMap) > 25 {
				continue
			}
			bestDisplay := canon
			bestVotes := -1
			for disp, v := range b.DisplayVotes {
				if v > bestVotes {
					bestVotes = v
					bestDisplay = disp
				}
			}
			portalCount := maxInt(1, len(b.Portals))
			baseline := math.Max(1.0, conceptAvg[canon])
			burst := float64(b.NewsCount) / baseline
			rankScore := float64(portalCount*4) + float64(b.NewsCount)*1.2 + burst*2.0
			if b.IsPolitical {
				rankScore *= 1.15
			}
			list = append(list, rankedConcept{
				bucket:  b,
				display: bestDisplay,
				burst:   math.Round(burst*100) / 100,
				score:   rankScore,
			})
		}

		sort.Slice(list, func(i, j int) bool {
			return list[i].score > list[j].score
		})

		limit := 80
		if len(list) > limit {
			list = list[:limit]
		}

		for _, item := range list {
			samplesJSON, _ := json.Marshal(item.bucket.Samples)
			rels := matchRelationsForConcept(relByDateAndEntity[d], item.display, item.bucket.Samples)
			relsJSON, _ := json.Marshal(rels)

			_, err = stmt.Exec(
				d,
				item.display,
				item.bucket.EntityType,
				item.bucket.NewsCount,
				maxInt(1, len(item.bucket.Portals)),
				item.burst,
				string(samplesJSON),
				string(relsJSON),
				item.bucket.IsPolitical,
			)
			if err != nil {
				return 0, err
			}
			totalSaved++
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return totalSaved, nil
}

func loadRelationsByDate(database *sql.DB) map[string][]ConceptRelationSnippet {
	out := make(map[string][]ConceptRelationSnippet)
	rows, err := database.Query(`
		SELECT COALESCE(news_id, 0), head_entity, relation, tail_entity,
		       COALESCE(head_score, 0), COALESCE(tail_score, 0), COALESCE(event_date, '')
		FROM news_relations
		ORDER BY (head_score + tail_score) DESC
	`)
	if err != nil {
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var newsID int64
		var head, rel, tail, evDate string
		var hs, ts float64
		if err := rows.Scan(&newsID, &head, &rel, &tail, &hs, &ts, &evDate); err != nil {
			continue
		}
		out[evDate] = append(out[evDate], ConceptRelationSnippet{
			NewsID:    newsID,
			Head:      head,
			Relation:  rel,
			Tail:      tail,
			HeadScore: hs,
			TailScore: ts,
		})
	}
	return out
}

func matchRelationsForConcept(dayRels []ConceptRelationSnippet, concept string, samples []SampleTitleItem) []ConceptRelationSnippet {
	if len(dayRels) == 0 {
		return nil
	}
	lowConcept := strings.ToLower(concept)
	sampleIDs := make(map[int64]bool, len(samples))
	for _, s := range samples {
		sampleIDs[s.NewsID] = true
	}

	var matched []ConceptRelationSnippet
	for _, r := range dayRels {
		if sampleIDs[r.NewsID] ||
			strings.Contains(strings.ToLower(r.Head), lowConcept) ||
			strings.Contains(strings.ToLower(r.Tail), lowConcept) {
			matched = append(matched, r)
			if len(matched) >= 6 {
				break
			}
		}
	}
	return matched
}

// SelectCandidateNewsForGLiNER returns rich, recent, un-extracted news items
// anchored on top chronological concepts so GLiNER2 runs fast and high-signal.
func SelectCandidateNewsForGLiNER(database *sql.DB, limit int) ([]CandidateNewsItem, error) {
	if limit <= 0 {
		limit = 60
	}

	// First prioritize news items referenced in daily_concepts.sample_titles that do not yet have relations
	rows, err := database.Query(`
		SELECT n.id, n.title, COALESCE(n.summary, ''),
		       COALESCE(CAST(n.published_date AS TEXT), ''),
		       COALESCE(CAST(n.created_at AS TEXT), '')
		FROM news n
		WHERE n.id NOT IN (SELECT DISTINCT news_id FROM news_relations WHERE news_id IS NOT NULL)
		  AND n.title IS NOT NULL AND length(n.title) >= 24
		  AND (n.region IN ('es', 'ar', 'latam', 'global') OR n.summary != '')
		ORDER BY
		  CASE WHEN length(COALESCE(n.summary, '')) > 30 THEN 0 ELSE 1 END,
		  CASE WHEN n.region IN ('es', 'ar', 'latam') THEN 0 ELSE 1 END,
		  COALESCE(CAST(n.published_date AS TEXT), CAST(n.created_at AS TEXT)) DESC
		LIMIT ?
	`, limit*3)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect sample IDs from recent daily_concepts so top concepts definitely get GLiNER2 relations
	priorityIDs := make(map[int64]bool)
	cRows, cErr := database.Query(`
		SELECT sample_titles FROM daily_concepts
		ORDER BY event_date DESC, is_political DESC, portal_count DESC
		LIMIT 120
	`)
	if cErr == nil {
		for cRows.Next() {
			var raw string
			if err := cRows.Scan(&raw); err == nil {
				var items []SampleTitleItem
				if json.Unmarshal([]byte(raw), &items) == nil {
					for _, it := range items {
						priorityIDs[it.NewsID] = true
					}
				}
			}
		}
		cRows.Close()
	}

	var priorityList []CandidateNewsItem
	var fallbackList []CandidateNewsItem
	seen := make(map[int64]bool)

	for id := range priorityIDs {
		var title, summary, pubDate, createdAt string
		var exists int
		qErr := database.QueryRow(`
			SELECT n.title, COALESCE(n.summary, ''),
			       COALESCE(CAST(n.published_date AS TEXT), ''),
			       COALESCE(CAST(n.created_at AS TEXT), ''),
			       (SELECT COUNT(1) FROM news_relations r WHERE r.news_id = n.id)
			FROM news n WHERE n.id = ?
		`, id).Scan(&title, &summary, &pubDate, &createdAt, &exists)
		if qErr == nil && exists == 0 && len(title) >= 18 {
			priorityList = append(priorityList, CandidateNewsItem{
				ID:        id,
				Title:     title,
				Summary:   summary,
				EventDate: NormalizeEventDate(pubDate, createdAt),
			})
			seen[id] = true
		}
	}

	for rows.Next() {
		var id int64
		var title, summary, pubDate, createdAt string
		if err := rows.Scan(&id, &title, &summary, &pubDate, &createdAt); err != nil {
			continue
		}
		if seen[id] {
			continue
		}
		fallbackList = append(fallbackList, CandidateNewsItem{
			ID:        id,
			Title:     title,
			Summary:   summary,
			EventDate: NormalizeEventDate(pubDate, createdAt),
		})
	}

	combined := append(priorityList, fallbackList...)
	if len(combined) > limit {
		combined = combined[:limit]
	}
	return combined, nil
}
