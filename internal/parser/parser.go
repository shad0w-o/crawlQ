package parser

import (
	"bytes"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Parsed struct {
	Title   string
	Content string
	Links   []string
}

func Parse(baseURL string, body []byte) (*Parsed, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(doc.Find("title").First().Text())

	// Remove script, style, nav, footer
	doc.Find("script, style, noscript, nav, footer, header").Remove()

	// Extract main text
	var sb strings.Builder
	doc.Find("p, h1, h2, h3, h4, li, article").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len(text) > 20 {
			sb.WriteString(text)
			sb.WriteString("\n")
		}
	})
	content := sb.String()
	if content == "" {
		// fallback to body text
		content = strings.TrimSpace(doc.Find("body").Text())
	}
	// collapse whitespace
	content = strings.Join(strings.Fields(content), " ")
	if len(content) > 20000 {
		content = content[:20000]
	}

	// Extract links
	base, _ := url.Parse(baseURL)
	seen := make(map[string]struct{})
	var links []string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}
		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") || strings.HasPrefix(href, "javascript:") {
			return
		}
		u, err := url.Parse(href)
		if err != nil {
			return
		}
		resolved := base.ResolveReference(u)
		// only http/https
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return
		}
		resolved.Fragment = ""
		normalized := NormalizeURL(resolved.String())
		if _, ok := seen[normalized]; !ok {
			seen[normalized] = struct{}{}
			links = append(links, normalized)
		}
	})

	return &Parsed{Title: title, Content: content, Links: links}, nil
}

// NormalizeURL lowercases host, removes trailing slash, sorts query not needed for MVP
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	// remove default ports
	if u.Scheme == "http" && strings.HasSuffix(u.Host, ":80") {
		u.Host = strings.TrimSuffix(u.Host, ":80")
	}
	if u.Scheme == "https" && strings.HasSuffix(u.Host, ":443") {
		u.Host = strings.TrimSuffix(u.Host, ":443")
	}
	cleanedPath := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
	if cleanedPath == "." {
		cleanedPath = "/"
	}
	if cleanedPath != "/" && strings.HasSuffix(cleanedPath, "/") {
		cleanedPath = strings.TrimSuffix(cleanedPath, "/")
	}
	u.Path = cleanedPath
	if u.RawQuery != "" {
		values := u.Query()
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		ordered := make(url.Values, len(values))
		for _, key := range keys {
			entries := append([]string(nil), values[key]...)
			sort.Strings(entries)
			for _, value := range entries {
				ordered.Add(key, value)
			}
		}
		u.RawQuery = ordered.Encode()
	}
	return u.String()
}
