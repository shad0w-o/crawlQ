-- Initial schema for crawler
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS pages (
    id SERIAL PRIMARY KEY,
    url TEXT UNIQUE NOT NULL,
    normalized_url TEXT UNIQUE NOT NULL,
    title TEXT,
    content TEXT,
    content_hash TEXT,
    status_code INT,
    error_text TEXT,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Full text search vector, auto-generated
    content_tsv tsvector GENERATED ALWAYS AS (
        to_tsvector('english', coalesce(title,'') || ' ' || coalesce(content,''))
    ) STORED
);

CREATE INDEX IF NOT EXISTS idx_pages_tsv ON pages USING GIN(content_tsv);
CREATE INDEX IF NOT EXISTS idx_pages_url ON pages(url);
CREATE INDEX IF NOT EXISTS idx_pages_hash ON pages(content_hash);
CREATE INDEX IF NOT EXISTS idx_pages_fetched_at ON pages(fetched_at);

-- Trigram index for fallback LIKE / similarity search
CREATE INDEX IF NOT EXISTS idx_pages_title_trgm ON pages USING GIN(title gin_trgm_ops);

CREATE TABLE IF NOT EXISTS links (
    from_url TEXT NOT NULL,
    to_url TEXT NOT NULL,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (from_url, to_url)
);

CREATE TABLE IF NOT EXISTS crawl_queue (
    url TEXT PRIMARY KEY,
    depth INT NOT NULL DEFAULT 0,
    retries INT NOT NULL DEFAULT 0,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
