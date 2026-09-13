package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"crawler/internal/crawl"
	"crawler/internal/config"
	"crawler/internal/storage"
)

func main() {
	var seedsFlag string
	var recursive bool
	var maxDepth int
	var maxPages int
	var concurrency int
	var delayMs int
	var userAgent string

	cfg := config.Load()
	flag.StringVar(&seedsFlag, "seeds", "https://en.wikipedia.org/wiki/Web_crawler", "comma-separated seed URLs")
	flag.BoolVar(&recursive, "recursive", true, "follow discovered links recursively")
	flag.IntVar(&maxDepth, "max-depth", cfg.MaxDepth, "maximum crawl depth")
	flag.IntVar(&maxPages, "max-pages", cfg.MaxPages, "maximum number of pages to crawl")
	flag.IntVar(&concurrency, "concurrency", cfg.Concurrency, "number of crawl workers")
	flag.IntVar(&delayMs, "delay-ms", cfg.DelayMs, "delay between requests per host in milliseconds")
	flag.StringVar(&userAgent, "user-agent", cfg.UserAgent, "crawler user agent")
	flag.Parse()

	seeds := parseSeeds(seedsFlag)
	if len(seeds) == 0 {
		log.Fatal("no valid seed URLs provided")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("starting crawler seeds=%v recursive=%t concurrency=%d maxPages=%d maxDepth=%d", seeds, recursive, concurrency, maxPages, maxDepth)

	var store *storage.Store
	if st, err := storage.New(ctx, cfg.DatabaseURL); err != nil {
		log.Printf("WARN: postgres not available (%v), running without persistence", err)
	} else {
		defer st.Close()
		store = st
		log.Println("connected to postgres")
	}

	runner := crawl.NewRunner(store)
	final, err := runner.Run(ctx, crawl.Options{
		Seeds:       seeds,
		Recursive:   recursive,
		MaxDepth:    maxDepth,
		MaxPages:    maxPages,
		Concurrency: concurrency,
		Delay:       time.Duration(delayMs) * time.Millisecond,
		UserAgent:   userAgent,
	}, func(progress crawl.Progress) {
		log.Printf("status=%s processed=%d discovered=%d failed=%d queued=%d current=%s depth=%d", progress.Status, progress.Processed, progress.Discovered, progress.Failed, progress.Queued, progress.CurrentURL, progress.CurrentDepth)
	})

	if err != nil && err != context.Canceled {
		log.Fatalf("crawl failed: %v", err)
	}
	log.Printf("done status=%s processed=%d discovered=%d failed=%d", final.Status, final.Processed, final.Discovered, final.Failed)
	if final.LastError != "" {
		log.Printf("last error: %s", final.LastError)
	}
}

func parseSeeds(raw string) []string {
	parts := strings.Split(raw, ",")
	seeds := make([]string, 0, len(parts))
	for _, part := range parts {
		seed := strings.TrimSpace(part)
		if seed != "" {
			seeds = append(seeds, seed)
		}
	}
	return seeds
}