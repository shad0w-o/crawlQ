DATABASE_URL ?= postgres://crawler:crawler@localhost:5432/crawler?sslmode=disable

.PHONY: up down migrate crawl api vet build

up:
	docker compose up -d

down:
	docker compose down

migrate:
	psql "$(DATABASE_URL)" -f migrations/001_init.sql

crawl:
	go run ./cmd/crawler --seeds https://en.wikipedia.org/wiki/Web_crawler

api:
	go run ./cmd/api

vet:
	go vet ./...

build:
	go build ./...
