// Package greader is a small client for the Google Reader compatible API that
// FreshRSS exposes at api/greader.php. It covers only what vrotrss needs:
// listing subscriptions and unread counts, reading streams, and changing the
// read/starred state of items.
package greader

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Well-known stream and tag identifiers.
const (
	StreamReadingList = "user/-/state/com.google/reading-list"
	StreamStarred     = "user/-/state/com.google/starred"
	TagRead           = "user/-/state/com.google/read"
	TagStarred        = StreamStarred

	apiPrefix = "/reader/api/0"
	maxBody   = 32 << 20
	editBatch = 100
)

// ErrAuth is returned when the server rejects the credentials.
var ErrAuth = errors.New("authentication failed: check the username and API password " +
	"(FreshRSS needs a separate API password, set under Profile)")

// APIError is returned for unexpected HTTP statuses.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("freshrss API returned HTTP %d: %s", e.Status, e.Body)
}

// Category is a folder (label) a subscription belongs to.
type Category struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Subscription is a feed.
type Subscription struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	URL        string     `json:"url"`
	HTMLURL    string     `json:"htmlUrl"`
	Categories []Category `json:"categories"`
}

// Item is a single article.
type Item struct {
	ID        string
	Title     string
	Author    string
	URL       string
	Content   string
	FeedID    string
	FeedTitle string
	Published time.Time
	Read      bool
	Starred   bool
}

// StreamQuery describes a request for articles.
type StreamQuery struct {
	Stream       string // feed/<id>, user/-/label/<name>, or one of the Stream* constants
	ExcludeRead  bool
	Oldest       bool
	Count        int
	Continuation string
}

// StreamPage is one page of articles.
type StreamPage struct {
	Items        []Item
	Continuation string // empty when there are no more pages
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client (useful for tests and proxies).
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.hc = hc } }

// Client talks to a FreshRSS instance. It is safe for concurrent use.
type Client struct {
	base string
	user string
	pass string
	hc   *http.Client

	mu    sync.Mutex
	auth  string
	token string
}

// NormalizeBaseURL accepts either the FreshRSS root URL or the full
// api/greader.php URL and returns the latter.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid URL %q: expected something like https://rss.example.com", raw)
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	p := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(p, "/greader.php") {
		p += "/api/greader.php"
	}
	u.Path, u.RawPath = p, ""
	return u.String(), nil
}

// New creates a client. The URL may be the FreshRSS root or the greader.php URL.
func New(rawURL, user, password string, opts ...Option) (*Client, error) {
	base, err := NormalizeBaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	if user == "" || password == "" {
		return nil, errors.New("username and API password are required")
	}
	c := &Client{base: base, user: user, pass: password, hc: &http.Client{Timeout: 30 * time.Second}}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Login authenticates eagerly. Other calls log in lazily, so calling Login is
// only needed to fail fast with a clear error.
func (c *Client) Login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginLocked(ctx)
}

func (c *Client) loginLocked(ctx context.Context) error {
	form := url.Values{"Email": {c.user}, "Passwd": {c.pass}}
	body, status, err := c.roundTrip(ctx, http.MethodPost, "/accounts/ClientLogin", nil, form, "")
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ErrAuth
	}
	if status != http.StatusOK {
		return &APIError{Status: status, Body: snippet(body)}
	}
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "Auth="); ok && v != "" {
			c.auth = v
			return nil
		}
	}
	return errors.New("login response had no Auth token (is API access enabled in FreshRSS?)")
}

func (c *Client) authHeader(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.auth == "" {
		if err := c.loginLocked(ctx); err != nil {
			return "", err
		}
	}
	return c.auth, nil
}

func (c *Client) resetAuth(old string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.auth == old {
		c.auth, c.token = "", ""
	}
}

func (c *Client) clearToken(old string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == old {
		c.token = ""
	}
}

func (c *Client) roundTrip(ctx context.Context, method, endpoint string, query, form url.Values, auth string) (data []byte, status int, err error) {
	target := c.base + endpoint
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reqBody io.Reader
	if form != nil {
		reqBody = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if auth != "" {
		req.Header.Set("Authorization", "GoogleLogin auth="+auth)
	}
	req.Header.Set("User-Agent", "vrotrss")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}
	if len(data) > maxBody {
		return nil, 0, errors.New("response too large")
	}
	return data, resp.StatusCode, nil
}

// do performs an authenticated request, re-authenticating once on 401.
func (c *Client) do(ctx context.Context, method, endpoint string, query, form url.Values) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		auth, err := c.authHeader(ctx)
		if err != nil {
			return nil, err
		}
		body, status, err := c.roundTrip(ctx, method, endpoint, query, form, auth)
		switch {
		case err != nil:
			return nil, err
		case status == http.StatusUnauthorized && attempt == 0:
			c.resetAuth(auth)
			continue
		case status == http.StatusUnauthorized:
			return nil, ErrAuth
		case status != http.StatusOK:
			return nil, &APIError{Status: status, Body: snippet(body)}
		}
		return body, nil
	}
}

func (c *Client) writeToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	tok := c.token
	c.mu.Unlock()
	if tok != "" {
		return tok, nil
	}
	body, err := c.do(ctx, http.MethodGet, apiPrefix+"/token", nil, nil)
	if err != nil {
		return "", err
	}
	tok = strings.TrimSpace(string(body))
	if tok == "" {
		return "", errors.New("server returned an empty write token")
	}
	c.mu.Lock()
	c.token = tok
	c.mu.Unlock()
	return tok, nil
}

// post sends a write request, refreshing the write token once if it is rejected.
func (c *Client) post(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		tok, err := c.writeToken(ctx)
		if err != nil {
			return nil, err
		}
		form.Set("T", tok)
		body, err := c.do(ctx, http.MethodPost, apiPrefix+endpoint, nil, form)
		var ae *APIError
		if attempt == 0 && errors.As(err, &ae) && ae.Status == http.StatusBadRequest {
			c.clearToken(tok)
			continue
		}
		return body, err
	}
}

// Subscriptions lists all feeds with their folders.
func (c *Client) Subscriptions(ctx context.Context) ([]Subscription, error) {
	body, err := c.do(ctx, http.MethodGet, apiPrefix+"/subscription/list", url.Values{"output": {"json"}}, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Subscriptions []Subscription `json:"subscriptions"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode subscriptions: %w", err)
	}
	return resp.Subscriptions, nil
}

// UnreadCounts returns unread counts keyed by stream ID (feeds, folders and
// the reading list).
func (c *Client) UnreadCounts(ctx context.Context) (map[string]int, error) {
	body, err := c.do(ctx, http.MethodGet, apiPrefix+"/unread-count", url.Values{"output": {"json"}}, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Unread []struct {
			ID    string `json:"id"`
			Count int    `json:"count"`
		} `json:"unreadcounts"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode unread counts: %w", err)
	}
	out := make(map[string]int, len(resp.Unread))
	for _, u := range resp.Unread {
		out[u.ID] = u.Count
	}
	return out, nil
}

type rawItem struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	Published  int64    `json:"published"`
	Categories []string `json:"categories"`
	Canonical  []struct {
		Href string `json:"href"`
	} `json:"canonical"`
	Alternate []struct {
		Href string `json:"href"`
	} `json:"alternate"`
	Summary struct {
		Content string `json:"content"`
	} `json:"summary"`
	Content struct {
		Content string `json:"content"`
	} `json:"content"`
	Origin struct {
		StreamID string `json:"streamId"`
		Title    string `json:"title"`
	} `json:"origin"`
}

func (r rawItem) item() Item {
	it := Item{
		ID:        r.ID,
		Title:     r.Title,
		Author:    r.Author,
		Content:   r.Summary.Content,
		FeedID:    r.Origin.StreamID,
		FeedTitle: r.Origin.Title,
		Published: time.Unix(r.Published, 0),
	}
	if it.Content == "" {
		it.Content = r.Content.Content
	}
	switch {
	case len(r.Canonical) > 0:
		it.URL = r.Canonical[0].Href
	case len(r.Alternate) > 0:
		it.URL = r.Alternate[0].Href
	}
	for _, c := range r.Categories {
		switch c {
		case TagRead:
			it.Read = true
		case TagStarred:
			it.Starred = true
		}
	}
	return it
}

// escapeStream escapes each path segment of a stream ID but keeps the slashes.
func escapeStream(stream string) string {
	parts := strings.Split(stream, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// Stream fetches one page of articles.
func (c *Client) Stream(ctx context.Context, q StreamQuery) (StreamPage, error) {
	if q.Stream == "" {
		q.Stream = StreamReadingList
	}
	v := url.Values{"output": {"json"}}
	if q.Count > 0 {
		v.Set("n", strconv.Itoa(q.Count))
	}
	if q.Oldest {
		v.Set("r", "o")
	}
	if q.ExcludeRead {
		v.Set("xt", TagRead)
	}
	if q.Continuation != "" {
		v.Set("c", q.Continuation)
	}
	body, err := c.do(ctx, http.MethodGet, apiPrefix+"/stream/contents/"+escapeStream(q.Stream), v, nil)
	if err != nil {
		return StreamPage{}, err
	}
	var resp struct {
		Continuation string    `json:"continuation"`
		Items        []rawItem `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return StreamPage{}, fmt.Errorf("decode stream: %w", err)
	}
	page := StreamPage{Continuation: resp.Continuation, Items: make([]Item, 0, len(resp.Items))}
	for _, r := range resp.Items {
		page.Items = append(page.Items, r.item())
	}
	return page, nil
}

// SetRead marks items read or unread.
func (c *Client) SetRead(ctx context.Context, ids []string, read bool) error {
	return c.tag(ctx, ids, TagRead, read)
}

// SetStarred stars or unstars items.
func (c *Client) SetStarred(ctx context.Context, ids []string, starred bool) error {
	return c.tag(ctx, ids, TagStarred, starred)
}

func (c *Client) tag(ctx context.Context, ids []string, tag string, on bool) error {
	for start := 0; start < len(ids); start += editBatch {
		end := min(start+editBatch, len(ids))
		form := url.Values{"i": ids[start:end]}
		if on {
			form.Set("a", tag)
		} else {
			form.Set("r", tag)
		}
		body, err := c.post(ctx, "/edit-tag", form)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(strings.TrimSpace(string(body)), "OK") {
			return fmt.Errorf("edit-tag: unexpected response %q", snippet(body))
		}
	}
	return nil
}

// MarkAllRead marks everything in a stream older than before as read.
func (c *Client) MarkAllRead(ctx context.Context, stream string, before time.Time) error {
	if stream == "" {
		stream = StreamReadingList
	}
	form := url.Values{"s": {stream}, "ts": {strconv.FormatInt(before.UnixMicro(), 10)}}
	body, err := c.post(ctx, "/mark-all-as-read", form)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "OK") {
		return fmt.Errorf("mark-all-as-read: unexpected response %q", snippet(body))
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
