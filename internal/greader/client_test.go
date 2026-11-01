package greader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer imitates the parts of FreshRSS's greader.php that the client uses.
type fakeServer struct {
	t *testing.T

	mu        sync.Mutex
	password  string
	authToken string // currently valid Auth value
	wToken    string // currently valid write token
	logins    int
	tokens    int
	requests  []string
	forms     []map[string][]string
	handlers  map[string]string // path suffix -> JSON body
}

func newFake(t *testing.T) (*fakeServer, *httptest.Server) {
	t.Helper()
	f := &fakeServer{t: t, password: "secret", authToken: "alice/abc123", wToken: "wtok1", handlers: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	path := strings.TrimPrefix(r.URL.Path, "/api/greader.php")

	if path == "/accounts/ClientLogin" {
		f.logins++
		_ = r.ParseForm()
		if r.PostForm.Get("Passwd") != f.password {
			http.Error(w, "Error=BadAuthentication", http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprintf(w, "SID=alice/x\nLSID=null\nAuth=%s\n", f.authToken)
		return
	}
	if r.Header.Get("Authorization") != "GoogleLogin auth="+f.authToken {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if path == "/reader/api/0/token" {
		f.tokens++
		_, _ = fmt.Fprint(w, f.wToken)
		return
	}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		f.forms = append(f.forms, r.PostForm)
		if r.PostForm.Get("T") != f.wToken {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(w, "OK")
		return
	}
	for suffix, body := range f.handlers {
		if strings.HasSuffix(r.URL.Path, suffix) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, body)
			return
		}
	}
	http.NotFound(w, r)
}

func newClient(t *testing.T, srv *httptest.Server, password string) *Client {
	t.Helper()
	c, err := New(srv.URL, "alice", password)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{"https://rss.example.com", "https://rss.example.com/api/greader.php", false},
		{"https://rss.example.com/", "https://rss.example.com/api/greader.php", false},
		{"https://rss.example.com/api/greader.php", "https://rss.example.com/api/greader.php", false},
		{"https://example.com/freshrss", "https://example.com/freshrss/api/greader.php", false},
		{"http://localhost:8080/api/greader.php/", "http://localhost:8080/api/greader.php", false},
		{" https://rss.example.com?x=1#frag ", "https://rss.example.com/api/greader.php", false},
		{"https://user:pw@rss.example.com", "https://rss.example.com/api/greader.php", false},
		{"ftp://rss.example.com", "", true},
		{"rss.example.com", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizeBaseURL(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("NormalizeBaseURL(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNewRequiresCredentials(t *testing.T) {
	if _, err := New("https://x.example", "", "pw"); err == nil {
		t.Error("expected error for empty username")
	}
	if _, err := New("https://x.example", "u", ""); err == nil {
		t.Error("expected error for empty password")
	}
}

func TestLogin(t *testing.T) {
	_, srv := newFake(t)

	if err := newClient(t, srv, "secret").Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	err := newClient(t, srv, "wrong").Login(context.Background())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("Login with bad password: got %v, want ErrAuth", err)
	}
}

func TestLoginWithoutAuthToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "API is disabled")
	}))
	defer srv.Close()
	err := newClient(t, srv, "x").Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "API access") {
		t.Fatalf("got %v, want hint about enabling API access", err)
	}
}

func TestSubscriptionsAndCounts(t *testing.T) {
	f, srv := newFake(t)
	f.handlers["/subscription/list"] = `{"subscriptions":[
		{"id":"feed/1","title":"Hacker News","url":"https://n.ycombinator.com/rss","htmlUrl":"https://news.ycombinator.com/",
		 "categories":[{"id":"user/-/label/Tech","label":"Tech"}]},
		{"id":"feed/2","title":"Blog","categories":[]}]}`
	f.handlers["/unread-count"] = `{"max":1000,"unreadcounts":[
		{"id":"user/-/state/com.google/reading-list","count":12},
		{"id":"user/-/label/Tech","count":7},{"id":"feed/1","count":7}]}`

	c := newClient(t, srv, "secret")
	subs, err := c.Subscriptions(context.Background())
	if err != nil {
		t.Fatalf("Subscriptions: %v", err)
	}
	if len(subs) != 2 || subs[0].Title != "Hacker News" || subs[0].Categories[0].Label != "Tech" {
		t.Fatalf("unexpected subscriptions: %+v", subs)
	}
	counts, err := c.UnreadCounts(context.Background())
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if counts[StreamReadingList] != 12 || counts["user/-/label/Tech"] != 7 || counts["feed/1"] != 7 {
		t.Fatalf("unexpected counts: %v", counts)
	}
}

func TestStream(t *testing.T) {
	f, srv := newFake(t)
	f.handlers["/stream/contents/user/-/label/My Tech"] = `{"continuation":"next123","items":[
		{"id":"tag:google.com,2005:reader/item/0000000000000001","title":"One","author":"Ann","published":1700000000,
		 "categories":["user/-/state/com.google/reading-list","user/-/state/com.google/read","user/-/label/My Tech"],
		 "canonical":[{"href":"https://example.com/1"}],
		 "summary":{"content":"<p>Hello</p>"},
		 "origin":{"streamId":"feed/1","title":"Feed One"}},
		{"id":"tag:google.com,2005:reader/item/0000000000000002","title":"Two","published":1700000100,
		 "categories":["user/-/state/com.google/starred"],
		 "alternate":[{"href":"https://example.com/2"}],
		 "content":{"content":"<p>Body</p>"},
		 "origin":{"streamId":"feed/2","title":"Feed Two"}}]}`

	c := newClient(t, srv, "secret")
	page, err := c.Stream(context.Background(), StreamQuery{
		Stream: "user/-/label/My Tech", ExcludeRead: true, Count: 25, Continuation: "abc", Oldest: true,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	last := f.requests[len(f.requests)-1]
	for _, want := range []string{"/stream/contents/user/-/label/My%20Tech", "n=25", "xt=user%2F-%2Fstate%2Fcom.google%2Fread", "c=abc", "r=o", "output=json"} {
		if !strings.Contains(last, want) {
			t.Errorf("request %q missing %q", last, want)
		}
	}
	if page.Continuation != "next123" || len(page.Items) != 2 {
		t.Fatalf("unexpected page: %+v", page)
	}
	a, b := page.Items[0], page.Items[1]
	if a.Title != "One" || !a.Read || a.Starred || a.URL != "https://example.com/1" ||
		a.FeedID != "feed/1" || a.FeedTitle != "Feed One" || a.Content != "<p>Hello</p>" || a.Author != "Ann" ||
		!a.Published.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("item a decoded wrong: %+v", a)
	}
	if b.Read || !b.Starred || b.URL != "https://example.com/2" || b.Content != "<p>Body</p>" {
		t.Errorf("item b decoded wrong: %+v", b)
	}
}

func TestStreamDefaultsToReadingList(t *testing.T) {
	f, srv := newFake(t)
	f.handlers["/stream/contents/user/-/state/com.google/reading-list"] = `{"items":[]}`
	page, err := newClient(t, srv, "secret").Stream(context.Background(), StreamQuery{})
	if err != nil || len(page.Items) != 0 || page.Continuation != "" {
		t.Fatalf("got %+v, %v", page, err)
	}
}

func TestSetReadSendsTokenAndTags(t *testing.T) {
	f, srv := newFake(t)
	c := newClient(t, srv, "secret")

	if err := c.SetRead(context.Background(), []string{"id1", "id2"}, true); err != nil {
		t.Fatalf("SetRead: %v", err)
	}
	if err := c.SetRead(context.Background(), []string{"id1"}, false); err != nil {
		t.Fatalf("SetRead unread: %v", err)
	}
	if err := c.SetStarred(context.Background(), []string{"id3"}, true); err != nil {
		t.Fatalf("SetStarred: %v", err)
	}
	if f.tokens != 1 {
		t.Errorf("write token fetched %d times, want 1 (cached)", f.tokens)
	}
	if len(f.forms) != 3 {
		t.Fatalf("got %d POSTs, want 3", len(f.forms))
	}
	if got := f.forms[0]["i"]; len(got) != 2 || got[0] != "id1" || got[1] != "id2" {
		t.Errorf("ids = %v", got)
	}
	if f.forms[0]["a"][0] != TagRead || f.forms[0]["r"] != nil {
		t.Errorf("mark read form wrong: %v", f.forms[0])
	}
	if f.forms[1]["r"][0] != TagRead || f.forms[1]["a"] != nil {
		t.Errorf("mark unread form wrong: %v", f.forms[1])
	}
	if f.forms[2]["a"][0] != TagStarred {
		t.Errorf("star form wrong: %v", f.forms[2])
	}
}

func TestSetReadBatches(t *testing.T) {
	f, srv := newFake(t)
	ids := make([]string, 250)
	for i := range ids {
		ids[i] = fmt.Sprintf("id%d", i)
	}
	if err := newClient(t, srv, "secret").SetRead(context.Background(), ids, true); err != nil {
		t.Fatal(err)
	}
	if len(f.forms) != 3 {
		t.Fatalf("got %d batches, want 3", len(f.forms))
	}
}

func TestReauthOn401(t *testing.T) {
	f, srv := newFake(t)
	f.handlers["/subscription/list"] = `{"subscriptions":[]}`
	c := newClient(t, srv, "secret")
	if _, err := c.Subscriptions(context.Background()); err != nil {
		t.Fatal(err)
	}

	f.mu.Lock()
	f.authToken = "alice/rotated" // server invalidates our session
	f.mu.Unlock()

	if _, err := c.Subscriptions(context.Background()); err != nil {
		t.Fatalf("expected transparent re-login, got %v", err)
	}
	if f.logins != 2 {
		t.Errorf("logins = %d, want 2", f.logins)
	}
}

func TestRefreshesRejectedWriteToken(t *testing.T) {
	f, srv := newFake(t)
	c := newClient(t, srv, "secret")
	if err := c.SetRead(context.Background(), []string{"a"}, true); err != nil {
		t.Fatal(err)
	}

	f.mu.Lock()
	f.wToken = "wtok2" // cached token is now stale
	f.mu.Unlock()

	if err := c.SetRead(context.Background(), []string{"b"}, true); err != nil {
		t.Fatalf("expected token refresh and retry, got %v", err)
	}
	if f.tokens != 2 {
		t.Errorf("tokens fetched = %d, want 2", f.tokens)
	}
}

func TestMarkAllRead(t *testing.T) {
	f, srv := newFake(t)
	when := time.Unix(1700000000, 0)
	if err := newClient(t, srv, "secret").MarkAllRead(context.Background(), "feed/9", when); err != nil {
		t.Fatal(err)
	}
	form := f.forms[0]
	if form["s"][0] != "feed/9" || form["ts"][0] != "1700000000000000" || form["T"][0] != "wtok1" {
		t.Errorf("form = %v", form)
	}
}

func TestHTTPErrorAndBadJSON(t *testing.T) {
	f, srv := newFake(t)
	c := newClient(t, srv, "secret")

	_, err := c.Subscriptions(context.Background()) // no handler registered -> 404
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusNotFound {
		t.Fatalf("got %v, want APIError 404", err)
	}

	f.handlers["/subscription/list"] = `not json`
	if _, err := c.Subscriptions(context.Background()); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("got %v, want decode error", err)
	}
}

func TestContextCancelled(t *testing.T) {
	_, srv := newFake(t)
	c := newClient(t, srv, "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Subscriptions(ctx); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestEscapeStream(t *testing.T) {
	tests := map[string]string{
		"feed/12":                      "feed/12",
		"user/-/label/Tech News":       "user/-/label/Tech%20News",
		"user/-/label/R&D/日本":          "user/-/label/R&D/%E6%97%A5%E6%9C%AC",
		StreamReadingList:              StreamReadingList,
		"user/-/label/100% sure?#hash": "user/-/label/100%25%20sure%3F%23hash",
	}
	for in, want := range tests {
		if got := escapeStream(in); got != want {
			t.Errorf("escapeStream(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSnippetTruncates(t *testing.T) {
	long := strings.Repeat("x", 500)
	if got := snippet([]byte(long)); len([]rune(got)) != 201 {
		t.Errorf("snippet length = %d", len([]rune(got)))
	}
}
