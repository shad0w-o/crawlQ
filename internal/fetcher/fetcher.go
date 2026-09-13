package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
	"golang.org/x/time/rate"
)

type Result struct {
	URL        string
	StatusCode int
	Body       []byte
	Headers    http.Header
	Error      error
}

type Fetcher struct {
	client    *http.Client
	userAgent string
	limiters  map[string]*rate.Limiter
	mu        sync.Mutex
	robots    map[string]*robotstxt.RobotsData
	delay     time.Duration
}

func New(userAgent string, delay time.Duration) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{	
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 5,
			},
		},
		userAgent: userAgent,
		limiters:  make(map[string]*rate.Limiter),
		robots:    make(map[string]*robotstxt.RobotsData),
		delay:     delay,
	}
}

func (f *Fetcher) getLimiter(host string) *rate.Limiter {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l, ok := f.limiters[host]; ok {
		return l
	}
	// 1 request per delay interval
	var limiter *rate.Limiter
	if f.delay > 0 {
		limiter = rate.NewLimiter(rate.Every(f.delay), 1)
	} else {
		limiter = rate.NewLimiter(rate.Inf, 1)
	}
	f.limiters[host] = limiter
	return limiter
}

func (f *Fetcher) isAllowed(ctx context.Context, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Scheme + "://" + u.Host

	f.mu.Lock()
	robots, ok := f.robots[host]
	f.mu.Unlock()
	if !ok {
		// fetch robots.txt
		robotsURL := host + "/robots.txt"
		req, _ := http.NewRequestWithContext(ctx, "GET", robotsURL, nil)
		req.Header.Set("User-Agent", f.userAgent)
		resp, err := f.client.Do(req)
		if err != nil {
			// fail open if cannot fetch robots.txt
			return true
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		data, err := robotstxt.FromBytes(body)
		if err != nil {
			return true
		}
		f.mu.Lock()
		f.robots[host] = data
		f.mu.Unlock()
		robots = data
	}
	return robots.TestAgent(u.Path, f.userAgent)
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string) Result {
	if !f.isAllowed(ctx, rawURL) {
		return Result{URL: rawURL, Error: fmt.Errorf("blocked by robots.txt")}
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return Result{URL: rawURL, Error: err}
	}

	limiter := f.getLimiter(u.Host)
	if err := limiter.Wait(ctx); err != nil {
		return Result{URL: rawURL, Error: err}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return Result{URL: rawURL, Error: err}
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := f.client.Do(req)
	if err != nil {
		return Result{URL: rawURL, Error: err}
	}
	defer resp.Body.Close()

	// only process HTML
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") && ct != "" {
		// still allow if no content-type, but skip obvious binaries
		if strings.Contains(ct, "image/") || strings.Contains(ct, "application/pdf") {
			return Result{URL: rawURL, StatusCode: resp.StatusCode, Error: fmt.Errorf("skipped non-html content-type: %s", ct)}
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5MB limit
	if err != nil {
		return Result{URL: rawURL, StatusCode: resp.StatusCode, Error: err}
	}

	return Result{
		URL:        rawURL,
		StatusCode: resp.StatusCode,
		Body:       body,
		Headers:    resp.Header,
	}
}
