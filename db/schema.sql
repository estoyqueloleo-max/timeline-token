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
