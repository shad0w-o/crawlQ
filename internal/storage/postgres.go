package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Page struct {
	ID            int       `json:"id"`
	URL           string    `json:"url"`
	NormalizedURL string    `json:"normalized_url"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	ContentHash   string    `json:"content_hash"`
	StatusCode    int       `json:"status_code"`
	ErrorText     string    `json:"error_text"`
	FetchedAt     time.Time `json:"fetched_at"`
}

type SearchResult struct {
	URL   string  `json:"url"`
	Title string  `json:"title"`
	Rank  float32 `json:"rank"`
	Snippet string `json:"snippet"`
}

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// HashContent returns sha256 hex of content for dedupe.
func HashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

func (s *Store) SavePage(ctx context.Context, p Page) error {
	p.ContentHash = HashContent(p.Content)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pages (url, normalized_url, title, content, content_hash, status_code, error_text, fetched_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		ON CONFLICT (normalized_url) DO UPDATE SET
			url = EXCLUDED.url,
			title = EXCLUDED.title,
			content = EXCLUDED.content,
			content_hash = EXCLUDED.content_hash,
			status_code = EXCLUDED.status_code,
			error_text = EXCLUDED.error_text,
			fetched_at = NOW()
	`, p.URL, p.NormalizedURL, p.Title, p.Content, p.ContentHash, p.StatusCode, p.ErrorText)
	return err
}

func (s *Store) IsVisited(ctx context.Context, normalizedURL string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pages WHERE normalized_url=$1)`, normalizedURL).Scan(&exists)
	return exists, err
}

func (s *Store) IsHashSeen(ctx context.Context, hash string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pages WHERE content_hash=$1)`, hash).Scan(&exists)
	return exists, err
}

func (s *Store) SaveLink(ctx context.Context, fromURL, toURL string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO links (from_url, to_url) VALUES ($1,$2) ON CONFLICT DO NOTHING`, fromURL, toURL)
	return err
}

func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT url, title,
		       ts_rank(content_tsv, plainto_tsquery('english', $1)) AS rank,
		       ts_headline('english', coalesce(content,''), plainto_tsquery('english', $1), 'StartSel=<b>, StopSel=</b>, MaxWords=30, MinWords=15, ShortWord=3') AS snippet
		FROM pages
		WHERE content_tsv @@ plainto_tsquery('english', $1)
		ORDER BY rank DESC
		LIMIT $2
	`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.URL, &r.Title, &r.Rank, &r.Snippet); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *Store) Stats(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM pages`).Scan(&count)
	return count, err
}
