package frontier

import (
	"sync"
)

// Frontier is an in-memory BFS queue with dedupe and depth tracking.
type Item struct {
	URL   string
	Depth int
}

type Frontier struct {
	mu      sync.Mutex
	queue   []Item
	visited map[string]struct{}
}

func New(seeds []string) *Frontier {
	f := &Frontier{
		visited: make(map[string]struct{}),
	}
	for _, s := range seeds {
		f.visited[s] = struct{}{}
		f.queue = append(f.queue, Item{URL: s, Depth: 0})
	}
	return f
}

func (f *Frontier) Push(url string, depth int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.visited[url]; ok {
		return false
	}
	f.visited[url] = struct{}{}
	f.queue = append(f.queue, Item{URL: url, Depth: depth})
	return true
}

func (f *Frontier) Pop() (Item, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return Item{}, false
	}
	item := f.queue[0]
	f.queue = f.queue[1:]
	return item, true
}

func (f *Frontier) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queue)
}

func (f *Frontier) VisitedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.visited)
}

func (f *Frontier) HasVisited(url string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.visited[url]
	return ok
}
