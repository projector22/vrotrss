// Package demo provides an in-memory, FreshRSS-like backend with sample data.
// It powers `vrot -demo` (try the UI without a server) and the TUI tests.
package demo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/projector22/vrotrss/internal/greader"
)

const (
	feedCount = 9
	itemCount = 63
	labelPref = "user/-/label/"
)

// Client is a thread-safe in-memory implementation of the TUI's API.
type Client struct {
	mu    sync.Mutex
	subs  []greader.Subscription
	items []greader.Item // newest first
}

func cat(name string) []greader.Category {
	return []greader.Category{{ID: labelPref + name, Label: name}}
}

var titles = []string{
	"Why your goroutines are leaking",
	"A gentle introduction to terminal escape codes",
	"Benchmarking the new scheduler",
	"Ten years of running a home server",
	"Notes from the kernel mailing list",
	"The case for boring technology",
	"Rovers find ice beneath the dust",
	"Local council approves cycle lane plan",
	"Markets steady ahead of rate decision",
	"What we learned from a failed launch",
	"Understanding backpressure in pipelines",
	"A field guide to lichens",
	"Reading old code is a skill",
	"Quarterly results beat expectations",
	"How we cut our build times by 70%",
	"The quiet revival of RSS",
	"Telescope captures a distant merger",
	"Designing APIs that age well",
	"Storm season arrives early",
	"Small tools, sharp edges",
	"Proof assistants for working programmers",
}

func body(n int, url string) string {
	switch n % 4 {
	case 0:
		return `<h2>Overview</h2><p>This is a sample article rendered inside <strong>vrotrss</strong>. ` +
			`Paragraphs wrap to the width of the reading pane, and <a href="` + url + `">links</a> are numbered ` +
			`so you can see where they go.</p><ul><li>First point worth noting</li><li>Second point, with a ` +
			`<a href="https://example.com/more">second link</a></li><li>Third point</li></ul>` +
			`<p>Press <code>o</code> to open the original in your browser.</p>`
	case 1:
		return `<p>Short and sweet. Sometimes a feed gives you one paragraph and nothing more.</p>` +
			`<blockquote><p>Simplicity is prerequisite for reliability.</p></blockquote><p>— an old saying</p>`
	case 2:
		return `<h2>Code</h2><p>Here is a snippet:</p><pre><code>func main() {
	fmt.Println("hello, feed")
}</code></pre><p>And an image: <img src="x.png" alt="a diagram of the pipeline"></p>` +
			`<ol><li>Read the article</li><li>Mark it read</li><li>Move on</li></ol>`
	default:
		return `<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore ` +
			`et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut ` +
			`aliquip ex ea commodo consequat.</p><p>Duis aute irure dolor in reprehenderit in voluptate velit esse ` +
			`cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident.</p>`
	}
}

// New builds a demo backend whose newest article is dated just before now.
func New(now time.Time) *Client {
	c := &Client{subs: []greader.Subscription{
		{ID: "feed/1", Title: "Gopher Gazette", Categories: cat("Tech")},
		{ID: "feed/2", Title: "Hacker Digest", Categories: cat("Tech")},
		{ID: "feed/3", Title: "Kernel Notes", Categories: cat("Tech")},
		{ID: "feed/4", Title: "World Wire", Categories: cat("News")},
		{ID: "feed/5", Title: "Local Ledger", Categories: cat("News")},
		{ID: "feed/6", Title: "Space Weekly", Categories: cat("Science")},
		{ID: "feed/7", Title: "Lab Notes", Categories: cat("Science")},
		{ID: "feed/8", Title: "Daily Strip", Categories: cat("Comics")},
		{ID: "feed/9", Title: "Odds and Ends", Categories: cat("Comics")},
	}}
	for n := 0; n < itemCount; n++ {
		sub := c.subs[n%feedCount]
		url := fmt.Sprintf("https://example.com/%d", n)
		c.items = append(c.items, greader.Item{
			ID:        fmt.Sprintf("tag:google.com,2005:reader/item/%016x", n+1),
			Title:     titles[n%len(titles)],
			Author:    []string{"Ann Author", "Bo Writer", ""}[n%3],
			URL:       url,
			Content:   body(n, url),
			FeedID:    sub.ID,
			FeedTitle: sub.Title,
			Published: now.Add(-time.Duration(n)*53*time.Minute - 4*time.Minute),
			Read:      n%5 == 3,
			Starred:   n%11 == 0,
		})
	}
	return c
}

// Subscriptions implements the TUI API.
func (c *Client) Subscriptions(context.Context) ([]greader.Subscription, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]greader.Subscription(nil), c.subs...), nil
}

func (c *Client) feedCategories(feedID string) []greader.Category {
	for _, s := range c.subs {
		if s.ID == feedID {
			return s.Categories
		}
	}
	return nil
}

// UnreadCounts implements the TUI API.
func (c *Client) UnreadCounts(context.Context) (map[string]int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]int{}
	for _, it := range c.items {
		if it.Read {
			continue
		}
		out[greader.StreamReadingList]++
		out[it.FeedID]++
		for _, cg := range c.feedCategories(it.FeedID) {
			out[cg.ID]++
		}
	}
	return out, nil
}

func (c *Client) inStream(it greader.Item, stream string) bool {
	switch {
	case stream == "" || stream == greader.StreamReadingList:
		return true
	case stream == greader.StreamStarred:
		return it.Starred
	case strings.HasPrefix(stream, labelPref):
		for _, cg := range c.feedCategories(it.FeedID) {
			if cg.ID == stream {
				return true
			}
		}
		return false
	default:
		return it.FeedID == stream
	}
}

// Stream implements the TUI API. Continuation tokens are plain offsets.
func (c *Client) Stream(_ context.Context, q greader.StreamQuery) (greader.StreamPage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var match []greader.Item
	for _, it := range c.items {
		if c.inStream(it, q.Stream) && (!q.ExcludeRead || !it.Read) {
			match = append(match, it)
		}
	}
	if q.Oldest {
		for i, j := 0, len(match)-1; i < j; i, j = i+1, j-1 {
			match[i], match[j] = match[j], match[i]
		}
	}
	start := 0
	if q.Continuation != "" {
		n, err := strconv.Atoi(q.Continuation)
		if err != nil || n < 0 {
			return greader.StreamPage{}, fmt.Errorf("bad continuation %q", q.Continuation)
		}
		start = n
	}
	count := q.Count
	if count <= 0 {
		count = 20
	}
	start = min(start, len(match))
	end := min(start+count, len(match))
	page := greader.StreamPage{Items: append([]greader.Item(nil), match[start:end]...)}
	if end < len(match) {
		page.Continuation = strconv.Itoa(end)
	}
	return page, nil
}

func (c *Client) mutate(ids []string, fn func(*greader.Item)) {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for i := range c.items {
		if set[c.items[i].ID] {
			fn(&c.items[i])
		}
	}
}

// SetRead implements the TUI API.
func (c *Client) SetRead(_ context.Context, ids []string, read bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mutate(ids, func(it *greader.Item) { it.Read = read })
	return nil
}

// SetStarred implements the TUI API.
func (c *Client) SetStarred(_ context.Context, ids []string, starred bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mutate(ids, func(it *greader.Item) { it.Starred = starred })
	return nil
}

// MarkAllRead implements the TUI API.
func (c *Client) MarkAllRead(_ context.Context, stream string, before time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.items {
		if c.inStream(c.items[i], stream) && !c.items[i].Published.After(before) {
			c.items[i].Read = true
		}
	}
	return nil
}

// Item returns a copy of the stored item with the given ID.
func (c *Client) Item(id string) (greader.Item, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, it := range c.items {
		if it.ID == id {
			return it, true
		}
	}
	return greader.Item{}, false
}

// Add prepends a custom item (useful in tests).
func (c *Client) Add(it greader.Item) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = append([]greader.Item{it}, c.items...)
}
