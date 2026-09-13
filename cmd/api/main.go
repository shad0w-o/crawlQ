package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crawler/internal/crawl"
	"crawler/internal/config"
	"crawler/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type crawlRequest struct {
	Seeds       []string `json:"seeds"`
	Recursive   *bool    `json:"recursive"`
	MaxDepth    *int     `json:"maxDepth"`
	MaxPages    *int     `json:"maxPages"`
	Concurrency *int     `json:"concurrency"`
	DelayMs     *int     `json:"delayMs"`
	UserAgent   string   `json:"userAgent"`
}

func main() {
	cfg := config.Load()
	ctx := context.Background()

	store, err := storage.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres connect: %v", err)
	}
	defer store.Close()

	runner := crawl.NewRunner(store)
	service := crawl.NewService(runner)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/stats", func(w http.ResponseWriter, r *http.Request) {
		count, err := store.Stats(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"pages": count})
	})

	r.Get("/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			http.Error(w, "missing ?q=", http.StatusBadRequest)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit == 0 {
			limit = 20
		}
		results, err := store.Search(r.Context(), q, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if results == nil {
			results = []storage.SearchResult{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"query":   q,
			"count":   len(results),
			"results": results,
		})
	})

	r.Route("/api", func(api chi.Router) {
		api.Get("/crawls", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"jobs": service.List()})
		})

		api.Post("/crawls", func(w http.ResponseWriter, r *http.Request) {
			var req crawlRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}

			opts, err := crawlOptionsFromRequest(req, cfg)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			job, err := service.Start(opts)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusAccepted, job)
		})

		api.Get("/crawls/{id}", func(w http.ResponseWriter, r *http.Request) {
			job, ok := service.Get(chi.URLParam(r, "id"))
			if !ok {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, http.StatusOK, job)
		})

		api.Post("/crawls/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
			if !service.Cancel(chi.URLParam(r, "id")) {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
		})
	})

	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>CrawlQ API</title></head>
<body>
<h1>CrawlQ API</h1>
<p>Use <code>POST /api/crawls</code> to start a crawl, <code>GET /api/crawls</code> to list jobs, <code>GET /search?q=...</code> for search, and <code>/stats</code> for indexed pages.</p>
</body>
</html>`))
	})

	addr := ":" + cfg.APIPort
	log.Printf("api listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

func crawlOptionsFromRequest(req crawlRequest, cfg config.Config) (crawl.Options, error) {
	if len(req.Seeds) == 0 {
		return crawl.Options{}, errors.New("at least one seed URL is required")
	}

	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}
	maxDepth := cfg.MaxDepth
	if req.MaxDepth != nil {
		maxDepth = *req.MaxDepth
	}
	maxPages := cfg.MaxPages
	if req.MaxPages != nil {
		maxPages = *req.MaxPages
	}
	concurrency := cfg.Concurrency
	if req.Concurrency != nil {
		concurrency = *req.Concurrency
	}
	delayMs := cfg.DelayMs
	if req.DelayMs != nil {
		delayMs = *req.DelayMs
	}
	userAgent := strings.TrimSpace(req.UserAgent)
	if userAgent == "" {
		userAgent = cfg.UserAgent
	}

	return crawl.Options{
		Seeds:       req.Seeds,
		Recursive:   recursive,
		MaxDepth:    maxDepth,
		MaxPages:    maxPages,
		Concurrency: concurrency,
		Delay:       time.Duration(delayMs) * time.Millisecond,
		UserAgent:   userAgent,
	}, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}