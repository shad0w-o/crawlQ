# CrawlQ

CrawlQ is currently implemented as a phase-1 Go crawler service with Postgres-backed page storage and a small HTTP API for crawl control and search.

## What is included

- Recursive web crawling in Go
- Per-host rate limiting and robots.txt checks
- URL normalization and duplicate suppression
- HTML parsing and content extraction with `goquery`
- Postgres persistence for pages and discovered links
- Full-text search over stored page content
- HTTP API to start crawls and inspect crawl jobs

## Stack

- Go
- Postgres 16
- `net/http`
- `goquery`
- `pgx/v5`
- `chi`
- `robotstxt`

## Quick start

```bash
cp .env.example .env
docker compose up -d
make migrate
go run ./cmd/crawler --seeds https://en.wikipedia.org/wiki/Web_crawler
go run ./cmd/api
```

## Environment

- `DATABASE_URL`
- `CRAWLER_USER_AGENT`
- `CRAWLER_MAX_PAGES`
- `CRAWLER_MAX_DEPTH`
- `CRAWLER_CONCURRENCY`
- `CRAWLER_DELAY_MS`
- `API_PORT`

## Crawler CLI

```bash
go run ./cmd/crawler --seeds https://example.com --recursive=true --max-depth 3 --max-pages 100
```

Flags:

- `--seeds` comma-separated seed URLs
- `--recursive` follow discovered links
- `--max-depth` recursion depth limit
- `--max-pages` page limit
- `--concurrency` worker count
- `--delay-ms` delay per host between requests
- `--user-agent` crawler user agent

## API

- `GET /health` - health check
- `GET /stats` - number of stored pages
- `GET /search?q=...&limit=20` - full-text search across crawled content
- `POST /api/crawls` - start a crawl job
- `GET /api/crawls` - list active and completed crawl jobs
- `GET /api/crawls/:id` - inspect a crawl job
- `POST /api/crawls/:id/cancel` - cancel a crawl job

### Start crawl request

```json
{
  "seeds": ["https://en.wikipedia.org/wiki/Web_crawler"],
  "recursive": true,
  "maxDepth": 2,
  "maxPages": 100,
  "concurrency": 6,
  "delayMs": 500,
  "userAgent": "CrawlQ/1.0"
}
```

### Crawl job response

```json
{
  "id": "...",
  "options": {
    "seeds": ["https://en.wikipedia.org/wiki/Web_crawler"],
    "recursive": true,
    "maxDepth": 2,
    "maxPages": 100,
    "concurrency": 6,
    "delay": 500000000,
    "userAgent": "CrawlQ/1.0"
  },
  "progress": {
    "status": "crawling",
    "discovered": 12,
    "processed": 9,
    "failed": 1,
    "queued": 2
  }
}
```

## Notes

- The crawl jobs are kept in memory by the API process.
- Pages, content, and links are persisted in Postgres.
- The RAG backend and frontend described in the project brief are future phases.