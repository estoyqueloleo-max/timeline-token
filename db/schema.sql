-- Portals table
CREATE TABLE IF NOT EXISTS portals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    domain TEXT UNIQUE,
    base_url TEXT NOT NULL,
    rss_url TEXT,
    language TEXT,
    region TEXT,
    has_archive BOOLEAN DEFAULT NULL,
    archive_url TEXT,
    last_crawled DATETIME,
    last_home_scrape DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- News items table
CREATE TABLE IF NOT EXISTS news (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    portal_id INTEGER,
    title TEXT NOT NULL,
    summary TEXT,
    url TEXT UNIQUE NOT NULL,
    news_hash TEXT UNIQUE,
    published_date DATETIME,
    content TEXT,
    found_at_depth INTEGER DEFAULT 0,
    media_type TEXT DEFAULT 'rss',
    language TEXT DEFAULT 'es',
    region TEXT DEFAULT 'global',
    is_historical BOOLEAN DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(portal_id) REFERENCES portals(id)
);

-- Embeddings table
CREATE TABLE IF NOT EXISTS news_embeddings (
    news_id INTEGER PRIMARY KEY,
    embedding BLOB NOT NULL,
    FOREIGN KEY(news_id) REFERENCES news(id)
);

-- Similarity history table
CREATE TABLE IF NOT EXISTS similarity_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    news_a_id INTEGER,
    news_b_id INTEGER,
    similarity REAL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(news_a_id) REFERENCES news(id),
    FOREIGN KEY(news_b_id) REFERENCES news(id)
);

-- News clusters table
CREATE TABLE IF NOT EXISTS news_clusters (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    centroid_news_id INTEGER,
    member_count INTEGER,
    main_topics TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(centroid_news_id) REFERENCES news(id)
);

-- News entities table (NER)
CREATE TABLE IF NOT EXISTS news_entities (
    news_id INTEGER,
    word TEXT,
    type TEXT, -- PER, LOC, ORG, VERB, ADJ
    PRIMARY KEY(news_id, word, type),
    FOREIGN KEY(news_id) REFERENCES news(id)
);

-- GLiNER2 Extracted Relations table
CREATE TABLE IF NOT EXISTS news_relations (
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
);
CREATE INDEX IF NOT EXISTS idx_news_relations_date ON news_relations(event_date);
CREATE INDEX IF NOT EXISTS idx_news_relations_head ON news_relations(head_entity);
CREATE INDEX IF NOT EXISTS idx_news_relations_tail ON news_relations(tail_entity);

-- Chronological Daily Concepts Aggregation
CREATE TABLE IF NOT EXISTS daily_concepts (
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
);
CREATE INDEX IF NOT EXISTS idx_daily_concepts_date ON daily_concepts(event_date);

-- YouTube Analytics & Talkback Outliers per Concept
CREATE TABLE IF NOT EXISTS youtube_concept_videos (
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
);
CREATE INDEX IF NOT EXISTS idx_yt_concept ON youtube_concept_videos(concept);

-- Daily Automation Runs Log
CREATE TABLE IF NOT EXISTS daily_runs (
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
);

