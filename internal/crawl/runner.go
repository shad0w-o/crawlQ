package crawl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"crawler/internal/fetcher"
	"crawler/internal/parser"
	"crawler/internal/storage"
)

const (
	StatusQueued    = "queued"
	StatusCrawling  = "crawling"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCanceled  = "cancelled"
)

type Options struct {
	Seeds       []string
	Recursive   bool
	MaxDepth    int
	MaxPages    int
	Concurrency int
	Delay       time.Duration
	UserAgent   string
}

type Progress struct {
	Status       string    `json:"status"`
	Discovered   int       `json:"discovered"`
	Processed    int       `json:"processed"`
	Failed       int       `json:"failed"`
	Queued       int       `json:"queued"`
	CurrentURL   string    `json:"currentUrl,omitempty"`
	CurrentDepth int       `json:"currentDepth,omitempty"`
	LastError    string    `json:"lastError,omitempty"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
	CompletedAt  time.Time `json:"completedAt,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

type Runner struct {
	store *storage.Store
}

func NewRunner(store *storage.Store) *Runner {
	return &Runner{store: store}
}

func (r *Runner) Run(ctx context.Context, opts Options, onUpdate func(Progress)) (Progress, error) {
	if len(opts.Seeds) == 0 {
		return Progress{}, errors.New("at least one seed URL is required")
	}
	if opts.MaxPages <= 0 {
		return Progress{}, errors.New("max pages must be greater than zero")
	}
	if opts.MaxDepth < 0 {
		return Progress{}, errors.New("max depth cannot be negative")
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if strings.TrimSpace(opts.UserAgent) == "" {
		opts.UserAgent = "CrawlQ/1.0"
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	fetcherClient := fetcher.New(opts.UserAgent, opts.Delay)
	queue := newQueue()
	go func() {
		<-ctx.Done()
		queue.close()
	}()

	progress := Progress{Status: StatusQueued, StartedAt: time.Now(), UpdatedAt: time.Now()}
	var progressMu sync.Mutex
	emit := func() {
		if onUpdate == nil {
			return
		}
		progressMu.Lock()
		snapshot := progress
		progressMu.Unlock()
		onUpdate(snapshot)
	}
	mutate := func(fn func(*Progress)) {
		progressMu.Lock()
		fn(&progress)
		progress.UpdatedAt = time.Now()
		progressMu.Unlock()
		emit()
	}

	acceptedSeeds := 0
	for _, seed := range opts.Seeds {
		if queue.enqueue(seed, 0) {
			acceptedSeeds++
		}
	}
	if acceptedSeeds == 0 {
		return Progress{}, errors.New("no valid seed URLs were provided")
	}

	mutate(func(p *Progress) {
		p.Discovered = acceptedSeeds
		p.Queued = queue.pendingCount()
		p.Status = StatusCrawling
	})

	var started atomic.Int64
	var workers sync.WaitGroup

	claimPage := func() bool {
		for {
			current := started.Load()
			if current >= int64(opts.MaxPages) {
				return false
			}
			if started.CompareAndSwap(current, current+1) {
				return true
			}
		}
	}

	for i := 0; i < opts.Concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				item, ok := queue.pop()
				if !ok {
					return
				}

				mutate(func(p *Progress) {
					p.CurrentURL = item.URL
					p.CurrentDepth = item.Depth
					p.Queued = queue.pendingCount()
				})

				if !claimPage() {
					mutate(func(p *Progress) {
						p.LastError = "crawl page limit reached"
						p.Status = StatusCanceled
					})
					cancel()
					queue.done()
					return
				}

				func() {
					defer queue.done()

					select {
					case <-ctx.Done():
						mutate(func(p *Progress) {
							p.Status = StatusCanceled
							p.LastError = ctx.Err().Error()
						})
						return
					default:
					}

					result := fetcherClient.Fetch(ctx, item.URL)
					if result.Error != nil {
						mutate(func(p *Progress) {
							p.Failed++
							p.LastError = result.Error.Error()
						})
						if r.store != nil {
							_ = r.store.SavePage(ctx, storage.Page{
								URL:           item.URL,
								NormalizedURL: parser.NormalizeURL(item.URL),
								StatusCode:    result.StatusCode,
								ErrorText:     result.Error.Error(),
							})
						}
						return
					}

					if result.StatusCode != http.StatusOK {
						message := fmt.Sprintf("http status %d", result.StatusCode)
						mutate(func(p *Progress) {
							p.Failed++
							p.LastError = message
						})
						if r.store != nil {
							_ = r.store.SavePage(ctx, storage.Page{
								URL:           item.URL,
								NormalizedURL: parser.NormalizeURL(item.URL),
								StatusCode:    result.StatusCode,
								ErrorText:     message,
							})
						}
						return
					}

					parsed, err := parser.Parse(item.URL, result.Body)
					if err != nil {
						mutate(func(p *Progress) {
							p.Failed++
							p.LastError = err.Error()
						})
						if r.store != nil {
							_ = r.store.SavePage(ctx, storage.Page{
								URL:           item.URL,
								NormalizedURL: parser.NormalizeURL(item.URL),
								StatusCode:    result.StatusCode,
								ErrorText:     err.Error(),
							})
						}
						return
					}

					mutate(func(p *Progress) {
						p.Processed++
						p.LastError = ""
					})

					if r.store != nil {
						page := storage.Page{
							URL:           item.URL,
							NormalizedURL: parser.NormalizeURL(item.URL),
							Title:         parsed.Title,
							Content:       parsed.Content,
							StatusCode:    result.StatusCode,
						}
						if err := r.store.SavePage(ctx, page); err != nil {
							mutate(func(p *Progress) {
								p.Failed++
								p.LastError = err.Error()
							})
						}
						for _, link := range parsed.Links {
							_ = r.store.SaveLink(ctx, parser.NormalizeURL(item.URL), link)
						}
					}

					if opts.Recursive && item.Depth < opts.MaxDepth {
						for _, link := range parsed.Links {
							if !queue.enqueue(link, item.Depth+1) {
								continue
							}
							mutate(func(p *Progress) {
								p.Discovered++
								p.Queued = queue.pendingCount()
							})
						}
					}
				}()
			}
		}()
	}

	workers.Wait()

	final := snapshotProgress(&progressMu, &progress)
	final.CompletedAt = time.Now()
	final.UpdatedAt = time.Now()
	if ctx.Err() != nil {
		final.Status = StatusCanceled
		if final.LastError == "" {
			final.LastError = ctx.Err().Error()
		}
		return final, ctx.Err()
	}
	if final.Status == StatusQueued || final.Status == StatusCrawling {
		final.Status = StatusCompleted
	}
	return final, nil
}

type crawlQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	items   []queueItem
	visited map[string]struct{}
	pending int
	closed  bool
}

type queueItem struct {
	URL   string
	Depth int
}

func newQueue() *crawlQueue {
	q := &crawlQueue{visited: make(map[string]struct{})}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *crawlQueue) enqueue(rawURL string, depth int) bool {
	normalized := parser.NormalizeURL(rawURL)
	if normalized == "" {
		return false
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	if _, ok := q.visited[normalized]; ok {
		return false
	}
	q.visited[normalized] = struct{}{}
	q.items = append(q.items, queueItem{URL: normalized, Depth: depth})
	q.pending++
	q.cond.Signal()
	return true
}

func (q *crawlQueue) pop() (queueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed && q.pending > 0 {
		q.cond.Wait()
	}
	if q.closed || (len(q.items) == 0 && q.pending == 0) {
		return queueItem{}, false
	}
	item := q.items[0]
	q.items = q.items[1:]
	return item, true
}

func (q *crawlQueue) done() {
	q.mu.Lock()
	if q.pending > 0 {
		q.pending--
	}
	if q.pending == 0 && len(q.items) == 0 {
		q.closed = true
	}
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *crawlQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *crawlQueue) pendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pending
}

func snapshotProgress(mu *sync.Mutex, progress *Progress) Progress {
	mu.Lock()
	snapshot := *progress
	mu.Unlock()
	return snapshot
}