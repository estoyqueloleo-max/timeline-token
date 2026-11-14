package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// YouTubeConceptVideo holds a YouTube video match and Talkback Outlier metrics for a concept.
type YouTubeConceptVideo struct {
	ID              int64   `json:"id"`
	Concept         string  `json:"concept"`
	EventDate       string  `json:"event_date"`
	VideoID         string  `json:"video_id"`
	Title           string  `json:"title"`
	ChannelID       string  `json:"channel_id"`
	ChannelTitle    string  `json:"channel_title"`
	PublishedAt     string  `json:"published_at"`
	ThumbnailURL    string  `json:"thumbnail_url"`
	ViewCount       uint64  `json:"view_count"`
	LikeCount       uint64  `json:"like_count"`
	CommentCount    uint64  `json:"comment_count"`
	SubscriberCount uint64  `json:"subscriber_count"`
	OutlierRatio    float64 `json:"outlier_ratio"`
	UpdatedAt       string  `json:"updated_at"`
}

type persistingTokenSource struct {
	mu        sync.Mutex
	base      oauth2.TokenSource
	tokenPath string
	lastToken string
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tok, err := p.base.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != "" && tok.AccessToken != p.lastToken {
		if b, mErr := json.MarshalIndent(tok, "", "  "); mErr == nil {
			_ = os.WriteFile(p.tokenPath, b, 0600)
		}
		p.lastToken = tok.AccessToken
	}
	return tok, nil
}

// NewYouTubeServiceFromDemocracia initializes the native YouTube Data API v3 client
// using OAuth2 credentials stored in democraciaDir.
func NewYouTubeServiceFromDemocracia(ctx context.Context, democraciaDir string) (*youtube.Service, error) {
	if democraciaDir == "" {
		democraciaDir = "/home/jose/workspace/sandbox/youtube/democracia"
	}
	secretPath := filepath.Join(democraciaDir, "client_secret.json")
	tokenPath := filepath.Join(democraciaDir, "youtube.token.json")

	secretBytes, err := os.ReadFile(secretPath)
	if err != nil {
		return nil, fmt.Errorf("read YouTube client_secret.json (%s): %w", secretPath, err)
	}
	cfg, err := google.ConfigFromJSON(secretBytes, youtube.YoutubeReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("parse OAuth2 config: %w", err)
	}

	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("read YouTube token (%s): %w", tokenPath, err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(tokenBytes, &tok); err != nil {
		return nil, fmt.Errorf("parse YouTube token: %w", err)
	}

	ts := &persistingTokenSource{
		base:      cfg.TokenSource(ctx, &tok),
		tokenPath: tokenPath,
		lastToken: tok.AccessToken,
	}

	// Verify / refresh token upfront cleanly
	if _, err := ts.Token(); err != nil {
		return nil, fmt.Errorf("refresh YouTube OAuth2 token: %w", err)
	}

	svc, err := youtube.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("init YouTube API service: %w", err)
	}
	return svc, nil
}

// FetchVideosForConcept queries YouTube Data API v3 for a concept, enriches video & channel
// statistics, calculates the Talkback Outlier Ratio (views / subscribers), and persists to SQLite.
func FetchVideosForConcept(ctx context.Context, database *sql.DB, democraciaDir, concept, eventDate string, maxResults int64) ([]YouTubeConceptVideo, error) {
	concept = strings.TrimSpace(concept)
	if concept == "" {
		return nil, fmt.Errorf("empty concept")
	}
	if maxResults <= 0 {
		maxResults = 6
	}

	svc, err := NewYouTubeServiceFromDemocracia(ctx, democraciaDir)
	if err != nil {
		return nil, err
	}

	searchCall := svc.Search.List([]string{"id", "snippet"}).
		Q(concept).
		Type("video").
		RelevanceLanguage("es").
		Order("relevance").
		MaxResults(maxResults)

	// Prefer recent content from the last 14 days when an eventDate is given
	if t, err := time.Parse("2006-01-02", eventDate); err == nil {
		publishedAfter := t.AddDate(0, 0, -14).UTC().Format(time.RFC3339)
		searchCall = searchCall.PublishedAfter(publishedAfter)
	}

	searchResp, err := searchCall.Do()
	if err != nil {
		return nil, fmt.Errorf("youtube search.list(%q): %w", concept, err)
	}

	var videoIDs []string
	seenVids := make(map[string]bool)
	for _, item := range searchResp.Items {
		if item.Id != nil && item.Id.VideoId != "" && !seenVids[item.Id.VideoId] {
			seenVids[item.Id.VideoId] = true
			videoIDs = append(videoIDs, item.Id.VideoId)
		}
	}

	if len(videoIDs) == 0 {
		return nil, nil
	}

	vidResp, err := svc.Videos.List([]string{"snippet", "statistics"}).Id(videoIDs...).Do()
	if err != nil {
		return nil, fmt.Errorf("youtube videos.list: %w", err)
	}

	channelSet := make(map[string]bool)
	var channelIDs []string
	for _, v := range vidResp.Items {
		if v.Snippet != nil && v.Snippet.ChannelId != "" && !channelSet[v.Snippet.ChannelId] {
			channelSet[v.Snippet.ChannelId] = true
			channelIDs = append(channelIDs, v.Snippet.ChannelId)
		}
	}

	channelSubs := make(map[string]uint64)
	if len(channelIDs) > 0 {
		chResp, chErr := svc.Channels.List([]string{"statistics"}).Id(channelIDs...).Do()
		if chErr == nil {
			for _, ch := range chResp.Items {
				if ch.Statistics != nil && !ch.Statistics.HiddenSubscriberCount {
					channelSubs[ch.Id] = ch.Statistics.SubscriberCount
				}
			}
		} else {
			log.Printf("[YouTube] warning channels.list: %v", chErr)
		}
	}

	var out []YouTubeConceptVideo
	stmt, err := database.Prepare(`
		INSERT INTO youtube_concept_videos (
			concept, event_date, video_id, title, channel_id, channel_title,
			published_at, thumbnail_url, view_count, like_count, comment_count,
			subscriber_count, outlier_ratio, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(concept, video_id) DO UPDATE SET
			event_date = COALESCE(NULLIF(excluded.event_date, ''), youtube_concept_videos.event_date),
			title = excluded.title,
			channel_id = excluded.channel_id,
			channel_title = excluded.channel_title,
			published_at = excluded.published_at,
			thumbnail_url = excluded.thumbnail_url,
			view_count = excluded.view_count,
			like_count = excluded.like_count,
			comment_count = excluded.comment_count,
			subscriber_count = excluded.subscriber_count,
			outlier_ratio = excluded.outlier_ratio,
			updated_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	for _, v := range vidResp.Items {
		if v.Snippet == nil {
			continue
		}
		var views, likes, comments uint64
		if v.Statistics != nil {
			views = v.Statistics.ViewCount
			likes = v.Statistics.LikeCount
			comments = v.Statistics.CommentCount
		}
		subs := channelSubs[v.Snippet.ChannelId]
		outlier := 0.0
		if subs > 0 {
			outlier = math.Round((float64(views)/float64(subs))*100) / 100
		}
		thumb := pickThumbnail(v.Snippet.Thumbnails)

		_, execErr := stmt.Exec(
			concept, eventDate, v.Id, v.Snippet.Title,
			v.Snippet.ChannelId, v.Snippet.ChannelTitle,
			v.Snippet.PublishedAt, thumb,
			int64(views), int64(likes), int64(comments), int64(subs), outlier,
		)
		if execErr != nil {
			log.Printf("[YouTube] insert error for video %s: %v", v.Id, execErr)
		}

		out = append(out, YouTubeConceptVideo{
			Concept:         concept,
			EventDate:       eventDate,
			VideoID:         v.Id,
			Title:           v.Snippet.Title,
			ChannelID:       v.Snippet.ChannelId,
			ChannelTitle:    v.Snippet.ChannelTitle,
			PublishedAt:     v.Snippet.PublishedAt,
			ThumbnailURL:    thumb,
			ViewCount:       views,
			LikeCount:       likes,
			CommentCount:    comments,
			SubscriberCount: subs,
			OutlierRatio:    outlier,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].OutlierRatio != out[j].OutlierRatio {
			return out[i].OutlierRatio > out[j].OutlierRatio
		}
		return out[i].ViewCount > out[j].ViewCount
	})

	return out, nil
}

func pickThumbnail(t *youtube.ThumbnailDetails) string {
	if t == nil {
		return ""
	}
	if t.High != nil {
		return t.High.Url
	}
	if t.Medium != nil {
		return t.Medium.Url
	}
	if t.Default != nil {
		return t.Default.Url
	}
	return ""
}

// LoadCachedYouTubeVideos returns persisted YouTube videos from SQLite.
// If concept is empty, returns top videos ranked by OutlierRatio and ViewCount across concepts.
func LoadCachedYouTubeVideos(database *sql.DB, concept string, onlyOutliers bool) ([]YouTubeConceptVideo, error) {
	if database == nil {
		return nil, fmt.Errorf("nil database")
	}

	query := `
		SELECT id, concept, COALESCE(event_date, ''), video_id, title,
		       COALESCE(channel_id, ''), COALESCE(channel_title, ''),
		       COALESCE(published_at, ''), COALESCE(thumbnail_url, ''),
		       COALESCE(view_count, 0), COALESCE(like_count, 0),
		       COALESCE(comment_count, 0), COALESCE(subscriber_count, 0),
		       COALESCE(outlier_ratio, 0), COALESCE(CAST(updated_at AS TEXT), '')
		FROM youtube_concept_videos
		WHERE 1=1
	`
	var args []any
	if strings.TrimSpace(concept) != "" {
		query += " AND lower(concept) = lower(?)"
		args = append(args, strings.TrimSpace(concept))
	}
	if onlyOutliers {
		query += " AND outlier_ratio >= 5.0"
	}
	query += " ORDER BY outlier_ratio DESC, view_count DESC LIMIT 120"

	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []YouTubeConceptVideo
	for rows.Next() {
		var item YouTubeConceptVideo
		if err := rows.Scan(
			&item.ID, &item.Concept, &item.EventDate, &item.VideoID, &item.Title,
			&item.ChannelID, &item.ChannelTitle, &item.PublishedAt, &item.ThumbnailURL,
			&item.ViewCount, &item.LikeCount, &item.CommentCount,
			&item.SubscriberCount, &item.OutlierRatio, &item.UpdatedAt,
		); err != nil {
			continue
		}
		list = append(list, item)
	}
	return list, nil
}

// EnrichTopConceptsWithYouTube queries YouTube for the top daily concepts not yet refreshed recently.
func EnrichTopConceptsWithYouTube(ctx context.Context, database *sql.DB, democraciaDir string, topN int) (int, error) {
	if topN <= 0 {
		topN = 8
	}

	rows, err := database.Query(`
		SELECT dc.concept, dc.event_date
		FROM daily_concepts dc
		WHERE dc.event_date = (SELECT MAX(event_date) FROM daily_concepts)
		  AND NOT EXISTS (
		      SELECT 1 FROM youtube_concept_videos yv
		      WHERE lower(yv.concept) = lower(dc.concept)
		        AND yv.updated_at >= datetime('now', '-12 hours')
		  )
		ORDER BY dc.is_political DESC, (dc.portal_count * 4 + dc.news_count) DESC
		LIMIT ?
	`, topN)
	if err != nil {
		return 0, err
	}

	type target struct {
		concept   string
		eventDate string
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.concept, &t.eventDate); err == nil {
			targets = append(targets, t)
		}
	}
	rows.Close()

	if len(targets) == 0 {
		return 0, nil
	}

	totalVideos := 0
	for _, t := range targets {
		vids, fErr := FetchVideosForConcept(ctx, database, democraciaDir, t.concept, t.eventDate, 6)
		if fErr != nil {
			log.Printf("[YouTube] concept=%q error: %v", t.concept, fErr)
			// Stop early if OAuth credential itself failed rather than spamming all concepts
			if strings.Contains(fErr.Error(), "OAuth2") || strings.Contains(fErr.Error(), "client_secret") || strings.Contains(fErr.Error(), "token") {
				return totalVideos, fErr
			}
			continue
		}
		totalVideos += len(vids)
		fmt.Printf("[YouTube] %q (%s) -> %d videos enriched\n", t.concept, t.eventDate, len(vids))
	}
	return totalVideos, nil
}

