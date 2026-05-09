package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"sort"
	"timeline-token/db"
)

type SimilarityResult struct {
	NewsA      string
	NewsB      string
	NewsAID    int
	NewsBID    int
	Similarity float32
}

func GetSimilarityHeatmap(limit int) ([]SimilarityResult, error) {
	rows, err := db.DB.Query(`
		SELECT n1.id, n1.title, n2.id, n2.title, e1.embedding, e2.embedding
		FROM news_embeddings e1
		JOIN news n1 ON e1.news_id = n1.id
		JOIN news_embeddings e2 ON e1.news_id < e2.news_id
		JOIN news n2 ON e2.news_id = n2.id
		ORDER BY e1.news_id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SimilarityResult
	for rows.Next() {
		var id1, id2 int
		var title1, title2 string
		var emb1Raw, emb2Raw []byte
		err := rows.Scan(&id1, &title1, &id2, &title2, &emb1Raw, &emb2Raw)
		if err != nil {
			continue
		}

		emb1 := bytesToFloats(emb1Raw)
		emb2 := bytesToFloats(emb2Raw)

		sim := db.CosineSimilarity(emb1, emb2)
		results = append(results, SimilarityResult{
			NewsAID:    id1,
			NewsA:      title1,
			NewsBID:    id2,
			NewsB:      title2,
			Similarity: sim,
		})
	}
	return results, nil
}

func bytesToFloats(b []byte) []float32 {
	f := make([]float32, len(b)/4)
	for i := range f {
		f[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return f
}

func PrintTopSimilarities(limit int) {
	results, err := GetSimilarityHeatmap(200000) // Much larger range with DESC ordering
	if err != nil {
		log.Printf("Error generating heatmap: %v", err)
		return
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	fmt.Println("\n--- Top Similitudes Semánticas ---")
	count := 0
	for _, res := range results {
		if res.Similarity > 0.6 {
			fmt.Printf("%.2f | %s <=> %s\n", res.Similarity, res.NewsA, res.NewsB)

			// Persist to history
			db.DB.Exec("INSERT OR IGNORE INTO similarity_history (news_a_id, news_b_id, similarity) VALUES (?, ?, ?)",
				res.NewsAID, res.NewsBID, res.Similarity)

			count++
			if count >= limit {
				break
			}
		}
	}
	if count == 0 {
		fmt.Println("No se encontraron similitudes significativas (>0.6) en la muestra analizada.")
	}

	computeAndStoreClusters(results)
}

func computeAndStoreClusters(results []SimilarityResult) {
	fmt.Println("\n--- Clusterización de Noticias (Top 10 Tendencias Distintas) ---")

	parent := make(map[int]int)
	find := func(i int) int {
		for parent[i] != 0 && parent[i] != i {
			i = parent[i]
		}
		return i
	}
	union := func(i, j int) {
		rootI := find(i)
		rootJ := find(j)
		if rootI != rootJ {
			if rootI == 0 {
				rootI = i
			}
			if rootJ == 0 {
				rootJ = j
			}
			parent[rootJ] = rootI
		}
	}

	for _, res := range results {
		if res.Similarity > 0.6 {
			union(res.NewsAID, res.NewsBID)
		}
	}

	clusters := make(map[int][]int)
	processed := make(map[int]bool)
	for id := range parent {
		root := find(id)
		clusters[root] = append(clusters[root], id)
		processed[id] = true
	}

	// Fill with other news that were embedded but not clustered
	embRows, _ := db.DB.Query("SELECT news_id FROM news_embeddings LIMIT 200")
	for embRows.Next() {
		var id int
		embRows.Scan(&id)
		if !processed[id] {
			clusters[id] = []int{id}
			processed[id] = true
		}
	}
	embRows.Close()

	type clusterInfo struct {
		centroidID int
		count      int
	}
	var clusterList []clusterInfo
	for root, members := range clusters {
		clusterList = append(clusterList, clusterInfo{root, len(members)})
	}

	sort.Slice(clusterList, func(i, j int) bool {
		if clusterList[i].count != clusterList[j].count {
			return clusterList[i].count > clusterList[j].count
		}
		return clusterList[i].centroidID > clusterList[j].centroidID
	})

	displayed := 0
	for i := 0; i < len(clusterList) && displayed < 10; i++ {
		c := clusterList[i]
		var title, mediaType string
		err := db.DB.QueryRow("SELECT title, media_type FROM news WHERE id = ?", c.centroidID).Scan(&title, &mediaType)
		if err != nil {
			continue
		}

		if c.count > 1 {
			fmt.Printf("Tendencia %d (%d noticias) [%s]: %s\n", displayed+1, c.count, mediaType, title)
		} else {
			fmt.Printf("Tema Distinto %d [%s]: %s\n", displayed+1, mediaType, title)
		}

		db.DB.Exec("INSERT INTO news_clusters (centroid_news_id, member_count, main_topics) VALUES (?, ?, ?)",
			c.centroidID, c.count, title)
		displayed++
	}
}
