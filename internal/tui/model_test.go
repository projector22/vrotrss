package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/projector22/vrotrss/internal/demo"
	"github.com/projector22/vrotrss/internal/greader"
)

var testNow = time.Date(2026, 11, 1, 14, 30, 0, 0, time.UTC)

const (
	item0 = "tag:google.com,2005:reader/item/0000000000000001" // unread, Gopher Gazette (Tech), starred
	item3 = "tag:google.com,2005:reader/item/0000000000000004" // already read
)

// flaky wraps the demo backend and can inject failures.
type flaky struct {
	*demo.Client
	failRead, failStar, failSubs, failCounts, failStream, failAll bool
}

var errBoom = errors.New("boom")

func (f *flaky) Subscriptions(ctx context.Context) ([]greader.Subscription, error) {
	if f.failSubs {
		return nil, errBoom
	}
	return f.Client.Subscriptions(ctx)
}

func (f *flaky) UnreadCounts(ctx context.Context) (map[string]int, error) {
	if f.failCounts {
		return nil, errBoom
	}
	return f.Client.UnreadCounts(ctx)
}

func (f *flaky) Stream(ctx context.Context, q greader.StreamQuery) (greader.StreamPage, error) {
	if f.failStream {
		return greader.StreamPage{}, errBoom
	}
	return f.Client.Stream(ctx, q)
}

func (f *flaky) SetRead(ctx context.Context, ids []string, read bool) error {
	if f.failRead {
		return errBoom
	}
	return f.Client.SetRead(ctx, ids, read)
}

func (f *flaky) SetStarred(ctx context.Context, ids []string, s bool) error {
	if f.failStar {
		return errBoom
	}
	return f.Client.SetStarred(ctx, ids, s)
}

func (f *flaky) MarkAllRead(ctx context.Context, s string, t time.Time) error {
	if f.failAll {
		return errBoom
	}
	return f.Client.MarkAllRead(ctx, s, t)
}

// drain runs a command and feeds every resulting message back into the model.
func drain(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			drain(m, c)
		}
	default:
		_, next := m.Update(msg)
		drain(m, next)
	}
}

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// press sends keys and drains the resulting commands.
func press(m *Model, keys ...string) {
	for _, k := range keys {
		_, cmd := m.Update(keyMsg(k))
		drain(m, cmd)
	}
}

type harness struct {
	m    *Model
	api  *flaky
	open []string
}

func newHarness(t *testing.T, mutate func(*Options), w, h int) *harness {
	t.Helper()
	hs := &harness{api: &flaky{Client: demo.New(testNow)}}
	opts := Options{
		MarkReadOnOpen: true, UnreadOnly: true, PageSize: 20,
		Location: time.UTC, Now: func() time.Time { return testNow },
		OpenURL: func(u string) error { hs.open = append(hs.open, u); return nil },
	}
	if mutate != nil {
		mutate(&opts)
	}
	hs.m = New(hs.api, opts)
	hs.m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	drain(hs.m, hs.m.Init())
	return hs
}

func (h *harness) item(t *testing.T, id string) greader.Item {
	t.Helper()
	it, ok := h.api.Item(id)
	if !ok {
		t.Fatalf("item %s not found", id)
	}
	return it
}

func TestInitialLoad(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	v := h.m.View()
	for _, want := range []string{
		"vrotrss", "Main stream (51)", "unread only", "Favourites",
		"▾ Tech", "Gopher Gazette", "Why your goroutines are leaking", "14:26", "31 Oct",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	if len(h.m.items) != 20 || h.m.cont == "" {
		t.Errorf("items=%d cont=%q, want first page of 20 with continuation", len(h.m.items), h.m.cont)
	}
	if h.m.focus != paneList {
		t.Errorf("initial focus = %v, want list", h.m.focus)
	}
}

func TestSidebarShowsCountsAndIndentsFeeds(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	v := h.m.View()
	var tech, feed string
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "▾ Tech") {
			tech = l
		}
		if side, _, ok := strings.Cut(l, "│"); ok && strings.Contains(side, "Kernel Notes") {
			feed = side
		}
	}
	if !strings.Contains(tech, "18") {
		t.Errorf("Tech folder row should show unread count 18: %q", tech)
	}
	if !strings.HasPrefix(feed, "     Kernel") {
		t.Errorf("feed row should be indented under its folder: %q", feed)
	}
}

func TestLayoutInvariants(t *testing.T) {
	sizes := [][2]int{{200, 50}, {110, 26}, {80, 24}, {70, 20}, {69, 20}, {60, 20}, {40, 12}, {24, 8}, {20, 5}}
	setups := map[string]func(*harness){
		"list":    func(*harness) {},
		"sidebar": func(h *harness) { press(h.m, "h") },
		"reader":  func(h *harness) { press(h.m, "enter") },
		"help":    func(h *harness) { press(h.m, "?") },
		"confirm": func(h *harness) { press(h.m, "A") },
		"error":   func(h *harness) { h.m.fail("Something", errBoom) },
	}
	for _, sz := range sizes {
		for name, setup := range setups {
			t.Run(fmt.Sprintf("%dx%d/%s", sz[0], sz[1], name), func(t *testing.T) {
				h := newHarness(t, nil, sz[0], sz[1])
				setup(h)
				lines := strings.Split(h.m.View(), "\n")
				if len(lines) != sz[1] {
					t.Fatalf("got %d lines, want %d", len(lines), sz[1])
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w != sz[0] {
						t.Errorf("line %d is %d columns, want %d: %q", i, w, sz[0], l)
					}
				}
			})
		}
	}
}

func TestViewBeforeFirstResize(t *testing.T) {
	m := New(demo.New(testNow), Options{})
	if m.View() != "" {
		t.Error("View before WindowSizeMsg should be empty")
	}
}

func TestOpenFeedFromSidebar(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	press(h.m, "h") // focus sidebar
	if h.m.focus != paneSidebar {
		t.Fatal("expected sidebar focus")
	}
	for h.m.nodes[h.m.sbCursor].label != "Lab Notes" {
		press(h.m, "j")
		if h.m.sbCursor == len(h.m.nodes)-1 {
			t.Fatal("Lab Notes not found")
		}
	}
	press(h.m, "enter")
	if h.m.focus != paneList || h.m.stream.ID != "feed/7" || h.m.stream.Title != "Lab Notes" {
		t.Fatalf("focus=%v stream=%+v", h.m.focus, h.m.stream)
	}
	if len(h.m.items) != 6 { // 7 items, 1 already read, unread-only view
		t.Fatalf("got %d items, want 6", len(h.m.items))
	}
	for _, it := range h.m.items {
		if it.FeedID != "feed/7" {
			t.Errorf("foreign item %q in feed view", it.FeedTitle)
		}
	}
	if !strings.Contains(h.m.View(), "Lab Notes (6)") {
		t.Errorf("header should show stream and count:\n%s", h.m.View())
	}
}

func TestOpenFolderAndFavourites(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	press(h.m, "h", "j") // Favourites
	press(h.m, "enter")
	if h.m.stream.ID != greader.StreamStarred || len(h.m.items) == 0 {
		t.Fatalf("stream=%+v items=%d", h.m.stream, len(h.m.items))
	}
	for _, it := range h.m.items {
		if !it.Starred {
			t.Errorf("non-starred item in favourites: %q", it.Title)
		}
	}

	press(h.m, "h", "j") // Comics folder (alphabetical first)
	press(h.m, "enter")
	if h.m.stream.Title != "Comics" {
		t.Fatalf("stream = %+v", h.m.stream)
	}
	for _, it := range h.m.items {
		if it.FeedID != "feed/8" && it.FeedID != "feed/9" {
			t.Errorf("item from %q in Comics", it.FeedTitle)
		}
	}
}

func TestOpeningArticleMarksReadEverywhere(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	if h.item(t, item0).Read {
		t.Fatal("precondition: item0 unread")
	}
	press(h.m, "enter")

	if h.m.focus != paneReader || h.m.reading != 0 {
		t.Fatalf("focus=%v reading=%d", h.m.focus, h.m.reading)
	}
	if !h.item(t, item0).Read {
		t.Error("server state not updated: web UI would still show it unread")
	}
	if !h.m.items[0].Read {
		t.Error("local state not updated")
	}
	if got := h.m.counts[greader.StreamReadingList]; got != 50 {
		t.Errorf("main count = %d, want 50", got)
	}
	if got := h.m.counts["feed/1"]; got != 5 {
		t.Errorf("feed count = %d, want 5", got)
	}
	if got := h.m.counts["user/-/label/Tech"]; got != 17 {
		t.Errorf("folder count = %d, want 17", got)
	}
	v := h.m.View()
	for _, want := range []string{"Why your goroutines are leaking", "Gopher Gazette · Ann Author", "Overview", "Links", "[1] https://example.com/0"} {
		if !strings.Contains(v, want) {
			t.Errorf("reader view missing %q:\n%s", want, v)
		}
	}
	// Local counts must agree with what the server now reports.
	srv, _ := h.api.UnreadCounts(context.Background())
	for k, v := range srv {
		if h.m.counts[k] != v {
			t.Errorf("count[%s]: local %d != server %d", k, h.m.counts[k], v)
		}
	}
}

func TestMarkReadOnOpenCanBeDisabled(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.MarkReadOnOpen = false }, 110, 26)
	press(h.m, "enter")
	if h.item(t, item0).Read || h.m.items[0].Read {
		t.Error("article was marked read although mark_read_on_open is off")
	}
	press(h.m, "m")
	if !h.item(t, item0).Read {
		t.Error("explicit m should mark read")
	}
}

func TestToggleReadFromList(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	press(h.m, "m")
	if !h.item(t, item0).Read || h.m.counts[greader.StreamReadingList] != 50 {
		t.Fatalf("mark read failed: server=%v count=%d", h.item(t, item0).Read, h.m.counts[greader.StreamReadingList])
	}
	if !strings.Contains(h.m.View(), "Why your goroutines are leaking") {
		t.Error("read article should stay visible until refresh, like the web UI")
	}
	press(h.m, "m")
	if h.item(t, item0).Read || h.m.counts[greader.StreamReadingList] != 51 || h.m.counts["feed/1"] != 6 {
		t.Fatalf("mark unread failed: server=%v counts=%v", h.item(t, item0).Read, h.m.counts)
	}
}

func TestToggleStar(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	if !h.item(t, item0).Starred {
		t.Fatal("precondition: item0 starred")
	}
	press(h.m, "s")
	if h.item(t, item0).Starred || h.m.items[0].Starred {
		t.Error("unstar failed")
	}
	press(h.m, "s")
	if !h.item(t, item0).Starred {
		t.Error("star failed")
	}
}

func TestFailedReadIsRolledBack(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	h.api.failRead = true
	press(h.m, "m")
	if h.m.items[0].Read {
		t.Error("optimistic change should be reverted after a failed write")
	}
	if h.m.counts[greader.StreamReadingList] != 51 || h.m.counts["feed/1"] != 6 {
		t.Errorf("counts not restored: %v", h.m.counts)
	}
	if !h.m.statusErr || !strings.Contains(h.m.status, "boom") {
		t.Errorf("status = %q (err=%v)", h.m.status, h.m.statusErr)
	}
	if !strings.Contains(h.m.View(), "boom") {
		t.Error("error must be visible in the footer")
	}
}

func TestFailedStarIsRolledBack(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	h.api.failStar = true
	press(h.m, "s")
	if !h.m.items[0].Starred || !h.m.statusErr {
		t.Errorf("starred=%v statusErr=%v", h.m.items[0].Starred, h.m.statusErr)
	}
}

func TestMarkAllRead(t *testing.T) {
	h := newHarness(t, nil, 110, 26)

	press(h.m, "A")
	if !h.m.confirm || !strings.Contains(h.m.View(), "as read? (y/n)") {
		t.Fatal("expected confirmation prompt")
	}
	press(h.m, "n")
	if h.m.confirm || h.m.items[0].Read || h.m.counts[greader.StreamReadingList] != 51 {
		t.Fatal("declining must change nothing")
	}

	press(h.m, "A", "y")
	for _, it := range h.m.items {
		if !it.Read {
			t.Fatalf("local item %q still unread", it.Title)
		}
	}
	srv, _ := h.api.UnreadCounts(context.Background())
	if srv[greader.StreamReadingList] != 0 {
		t.Errorf("server still has %d unread", srv[greader.StreamReadingList])
	}
	if h.m.counts[greader.StreamReadingList] != 0 {
		t.Errorf("local main count = %d after resync", h.m.counts[greader.StreamReadingList])
	}
}

func TestMarkAllReadFailureResyncs(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	h.api.failAll = true
	press(h.m, "A", "y")
	if !h.m.statusErr {
		t.Error("expected error status")
	}
	if h.m.counts[greader.StreamReadingList] != 51 {
		t.Errorf("counts should be resynced from the server, got %d", h.m.counts[greader.StreamReadingList])
	}
}

func TestToggleUnreadFilter(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	press(h.m, "u")
	if h.m.unreadOnly || !strings.Contains(h.m.View(), "all articles") {
		t.Fatal("expected all-articles mode")
	}
	hasRead := false
	for _, it := range h.m.items {
		hasRead = hasRead || it.Read
	}
	if !hasRead || len(h.m.items) != 20 {
		t.Errorf("all-articles view should include read items (items=%d)", len(h.m.items))
	}
	press(h.m, "u")
	if !h.m.unreadOnly {
		t.Error("expected unread-only again")
	}
}

func TestEmptyStates(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	press(h.m, "A", "y", "r")
	if len(h.m.items) != 0 || !strings.Contains(h.m.View(), "No unread articles") {
		t.Errorf("expected empty unread state:\n%s", h.m.View())
	}
	press(h.m, "u")
	if strings.Contains(h.m.View(), "No unread articles") || len(h.m.items) == 0 {
		t.Error("showing all should list read articles")
	}
}

func TestPaginationLoadsMoreAndDeduplicates(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	press(h.m, "G")
	if len(h.m.items) != 40 {
		t.Fatalf("after first G got %d items, want 40", len(h.m.items))
	}
	press(h.m, "G")
	if len(h.m.items) != 51 || h.m.cont != "" {
		t.Fatalf("after second G got %d items (cont=%q), want all 51", len(h.m.items), h.m.cont)
	}
	before := len(h.m.items)
	h.m.Update(streamMsg{gen: h.m.gen, more: true, page: greader.StreamPage{Items: h.m.items[:3]}})
	if len(h.m.items) != before {
		t.Error("duplicate items must be ignored")
	}
}

func TestStaleStreamResponseIgnored(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	n := len(h.m.items)
	h.m.Update(streamMsg{gen: h.m.gen - 1, page: greader.StreamPage{Items: []greader.Item{{ID: "x", Title: "stale"}}}})
	if len(h.m.items) != n || strings.Contains(h.m.View(), "stale") {
		t.Error("response from a previous stream must be ignored")
	}
}

func TestNextPreviousArticle(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	press(h.m, "enter", "n")
	if h.m.reading != 1 || h.m.listCursor != 1 {
		t.Fatalf("reading=%d cursor=%d", h.m.reading, h.m.listCursor)
	}
	if h.m.items[1].ID == "" || !h.api.mustItem(h.m.items[1].ID).Read {
		t.Error("next article should be marked read on open")
	}
	press(h.m, "p", "p")
	if h.m.reading != 0 || !strings.Contains(h.m.status, "No more") {
		t.Errorf("reading=%d status=%q", h.m.reading, h.m.status)
	}
	press(h.m, "esc")
	if h.m.focus != paneList {
		t.Error("esc should return to the list")
	}
}

func (f *flaky) mustItem(id string) greader.Item {
	it, _ := f.Item(id)
	return it
}

func TestReaderScrollsAndTabCycles(t *testing.T) {
	h := newHarness(t, nil, 110, 12)
	press(h.m, "enter", "G")
	if h.m.focus != paneReader {
		t.Fatal("expected reader focus")
	}
	press(h.m, "j", "j", "j")
	if h.m.vp.YOffset == 0 {
		t.Error("j should scroll the article")
	}
	press(h.m, "tab")
	if h.m.focus != paneSidebar {
		t.Errorf("tab from reader should go to sidebar, got %v", h.m.focus)
	}
	press(h.m, "tab")
	if h.m.focus != paneList {
		t.Errorf("tab from sidebar should go to list, got %v", h.m.focus)
	}
	press(h.m, "tab")
	if h.m.focus != paneReader {
		t.Errorf("tab from list with open article should go to reader, got %v", h.m.focus)
	}
}

func TestSidebarFolding(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	press(h.m, "h", "j", "j") // Comics folder
	n := len(h.m.nodes)
	if h.m.nodes[h.m.sbCursor].label != "Comics" {
		t.Fatalf("cursor on %q", h.m.nodes[h.m.sbCursor].label)
	}
	press(h.m, "h")
	if len(h.m.nodes) != n-2 || !strings.Contains(h.m.View(), "▸ Comics") {
		t.Errorf("fold failed: nodes %d -> %d", n, len(h.m.nodes))
	}
	if h.m.nodes[h.m.sbCursor].label != "Comics" {
		t.Error("cursor should stay on the folded folder")
	}
	press(h.m, "l")
	if len(h.m.nodes) != n {
		t.Error("l should unfold")
	}
	press(h.m, " ")
	if len(h.m.nodes) != n-2 {
		t.Error("space should toggle fold")
	}
	press(h.m, " ", "j", "h") // on a feed, h jumps to its folder
	if h.m.nodes[h.m.sbCursor].label != "Comics" {
		t.Errorf("h on a feed should select its folder, got %q", h.m.nodes[h.m.sbCursor].label)
	}
}

func TestUncategorizedFeeds(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	h.m.subs = append(h.m.subs, greader.Subscription{ID: "feed/99", Title: "Loner"})
	h.m.rebuildNodes()
	if !strings.Contains(h.m.View(), "Uncategorized") || !strings.Contains(h.m.View(), "Loner") {
		t.Fatalf("missing uncategorized folder:\n%s", h.m.View())
	}
	press(h.m, "h")
	for h.m.nodes[h.m.sbCursor].label != "Uncategorized" {
		press(h.m, "j")
	}
	stream := h.m.stream.ID
	press(h.m, "enter")
	if h.m.stream.ID != stream || !strings.Contains(h.m.status, "Nothing to show") {
		t.Errorf("synthetic folder must not load a stream; status=%q", h.m.status)
	}
}

func TestHostileFeedContentCannotReachTheTerminal(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	h.api.Add(greader.Item{
		ID:        "evil",
		Title:     "Evil \x1b[2J\x1b]0;owned\x07 title \u202e",
		Author:    "a\x1b[31muthor",
		URL:       "https://example.com/\x1b[2Jx",
		FeedID:    "feed/1",
		FeedTitle: "Feed\x1b[1m",
		Published: testNow,
		Content:   "<p>body \x1b[2J \x1b]52;c;AAAA\x07</p><script>alert(1)</script>",
	})
	press(h.m, "r") // reload; evil item is now first
	if h.m.items[0].ID != "evil" {
		t.Fatalf("setup: first item is %q", h.m.items[0].ID)
	}
	for name, view := range map[string]func() string{
		"list": func() string { return h.m.View() },
		"reader": func() string {
			press(h.m, "enter")
			return h.m.View()
		},
	} {
		if v := view(); strings.ContainsAny(v, "\x1b\x07\u202e") {
			t.Errorf("%s view leaks control characters: %q", name, v)
		}
	}
}

func TestOpenInBrowser(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	press(h.m, "o")
	if len(h.open) != 1 || h.open[0] != "https://example.com/0" {
		t.Errorf("opened %v", h.open)
	}
	press(h.m, "enter", "o")
	if len(h.open) != 2 {
		t.Errorf("reader o should open too: %v", h.open)
	}

	h2 := newHarness(t, func(o *Options) { o.OpenURL = func(string) error { return errBoom } }, 110, 26)
	press(h2.m, "o")
	if !h2.m.statusErr {
		t.Error("browser failure should be reported")
	}
}

func TestRefreshAndTick(t *testing.T) {
	h := newHarness(t, nil, 110, 26)
	_ = h.api.SetRead(context.Background(), []string{item0}, true) // changed elsewhere, e.g. in the web UI
	press(h.m, "r")
	if h.m.counts[greader.StreamReadingList] != 50 {
		t.Errorf("refresh should pick up outside changes, count=%d", h.m.counts[greader.StreamReadingList])
	}
	if h.m.items[0].ID == item0 {
		t.Error("refreshed unread list should no longer start with the now-read item")
	}

	_ = h.api.SetRead(context.Background(), []string{item3}, false)
	_, cmd := h.m.Update(tickMsg(testNow))
	drain(h.m, cmd)
	if h.m.counts[greader.StreamReadingList] != 51 {
		t.Errorf("tick should refresh counts, got %d", h.m.counts[greader.StreamReadingList])
	}
}

func TestLoadFailuresAreShown(t *testing.T) {
	for name, set := range map[string]func(*flaky){
		"subscriptions": func(f *flaky) { f.failSubs = true },
		"counts":        func(f *flaky) { f.failCounts = true },
		"stream":        func(f *flaky) { f.failStream = true },
	} {
		t.Run(name, func(t *testing.T) {
			api := &flaky{Client: demo.New(testNow)}
			set(api)
			m := New(api, Options{PageSize: 20, UnreadOnly: true, Location: time.UTC, Now: func() time.Time { return testNow }})
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
			drain(m, m.Init())
			if !m.statusErr || !strings.Contains(m.View(), "boom") {
				t.Errorf("failure not surfaced; status=%q", m.status)
			}
			if m.loading {
				t.Error("loading flag must be cleared after a failure")
			}
		})
	}
}

func TestHelpAndQuit(t *testing.T) {
	h := newHarness(t, nil, 110, 30)
	press(h.m, "?")
	if !strings.Contains(h.m.View(), "mark everything in this stream as read") {
		t.Error("help not shown")
	}
	press(h.m, "x")
	if h.m.help {
		t.Error("any key should close help")
	}

	for _, k := range []string{"q", "ctrl+c"} {
		_, cmd := h.m.Update(keyMsg(k))
		if cmd == nil {
			t.Fatalf("%s: no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s should quit", k)
		}
	}
	// ctrl+c quits even while a prompt is showing.
	press(h.m, "A")
	if _, cmd := h.m.Update(keyMsg("ctrl+c")); cmd == nil {
		t.Error("ctrl+c must quit from the confirm prompt")
	}
}

func TestCompactModeShowsOnePane(t *testing.T) {
	h := newHarness(t, nil, 50, 20)
	v := h.m.View()
	if strings.Contains(v, "▾ Tech") || !strings.Contains(v, "Why your goroutines") {
		t.Errorf("compact list view should hide the sidebar:\n%s", v)
	}
	press(h.m, "h")
	v = h.m.View()
	if !strings.Contains(v, "▾ Tech") || strings.Contains(v, "Why your goroutines") {
		t.Errorf("compact sidebar view should hide the list:\n%s", v)
	}
}

func TestInitStartsTicker(t *testing.T) {
	m := New(demo.New(testNow), Options{RefreshEvery: time.Hour})
	if m.Init() == nil {
		t.Error("Init should return commands")
	}
	if tickCmd(0) != nil || tickCmd(time.Minute) == nil {
		t.Error("tickCmd should be disabled only for non-positive durations")
	}
}

func TestOptionsDefaults(t *testing.T) {
	o := Options{}.withDefaults()
	if o.PageSize != 50 || o.Location == nil || o.Now == nil || o.OpenURL == nil {
		t.Errorf("defaults not applied: %+v", o)
	}
}
