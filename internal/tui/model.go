// Package tui implements the Bubble Tea user interface: a FreshRSS-style
// layout with a folder/feed sidebar on the left and an article list / reader on
// the right.
package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vrotrss/vrotrss/internal/greader"
)

const (
	requestTimeout = 60 * time.Second
	loadMoreMargin = 5
	compactBelow   = 70
)

// API is everything the UI needs from the backend. *greader.Client implements
// it, as does the in-memory demo backend.
type API interface {
	Subscriptions(ctx context.Context) ([]greader.Subscription, error)
	UnreadCounts(ctx context.Context) (map[string]int, error)
	Stream(ctx context.Context, q greader.StreamQuery) (greader.StreamPage, error)
	SetRead(ctx context.Context, ids []string, read bool) error
	SetStarred(ctx context.Context, ids []string, starred bool) error
	MarkAllRead(ctx context.Context, stream string, before time.Time) error
}

// Options configures the UI.
type Options struct {
	MarkReadOnOpen bool
	UnreadOnly     bool
	PageSize       int
	RefreshEvery   time.Duration // 0 disables periodic count refresh
	Version        string
	Location       *time.Location
	Now            func() time.Time
	OpenURL        func(string) error
}

func (o Options) withDefaults() Options {
	if o.PageSize <= 0 {
		o.PageSize = 50
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.OpenURL == nil {
		o.OpenURL = OpenBrowser
	}
	return o
}

type pane int

const (
	paneSidebar pane = iota
	paneList
	paneReader
)

type nodeKind int

const (
	kindAll nodeKind = iota
	kindStarred
	kindFolder
	kindFeed
)

type node struct {
	kind   nodeKind
	id     string // stream ID ("" for the synthetic Uncategorized folder)
	label  string
	parent string // folder ID for feeds
}

func (n node) key() string { return fmt.Sprintf("%d|%s|%s", n.kind, n.id, n.parent) }

type streamRef struct {
	ID    string
	Title string
}

// Messages produced by asynchronous commands.
type (
	subsMsg struct {
		subs []greader.Subscription
		err  error
	}
	countsMsg struct {
		counts map[string]int
		err    error
	}
	streamMsg struct {
		gen  int
		more bool
		page greader.StreamPage
		err  error
	}
	markMsg struct {
		id   string
		read bool
		err  error
	}
	starMsg struct {
		id      string
		starred bool
		err     error
	}
	markAllMsg struct {
		err error
	}
	tickMsg time.Time
)

// Model is the Bubble Tea model.
type Model struct {
	api  API
	opts Options

	width, height int
	focus         pane
	help          bool
	confirm       bool
	status        string
	statusErr     bool

	// sidebar
	subs      []greader.Subscription
	subByID   map[string]greader.Subscription
	counts    map[string]int
	collapsed map[string]bool
	nodes     []node
	sbCursor  int
	sbOffset  int

	// article list
	stream     streamRef
	gen        int
	items      []greader.Item
	cont       string
	loading    bool
	loadedAt   time.Time
	unreadOnly bool
	listCursor int
	listOffset int

	// reader
	reading int // index into items, -1 when no article is open
	vp      viewport.Model
}

// New creates the model.
func New(api API, opts Options) *Model {
	opts = opts.withDefaults()
	m := &Model{
		api:        api,
		opts:       opts,
		counts:     map[string]int{},
		collapsed:  map[string]bool{},
		subByID:    map[string]greader.Subscription{},
		unreadOnly: opts.UnreadOnly,
		reading:    -1,
		focus:      paneList,
		stream:     streamRef{ID: greader.StreamReadingList, Title: "Main stream"},
		vp:         viewport.New(0, 0),
	}
	m.rebuildNodes()
	return m
}

func tickCmd(d time.Duration) tea.Cmd {
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init starts the initial loads.
func (m *Model) Init() tea.Cmd {
	m.gen++
	m.loading = true
	m.loadedAt = m.opts.Now()
	return tea.Batch(m.cmdSubs(), m.cmdCounts(), m.cmdStream(false), tickCmd(m.opts.RefreshEvery))
}

// ---- commands ---------------------------------------------------------------

func (m *Model) cmdSubs() tea.Cmd {
	api := m.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		subs, err := api.Subscriptions(ctx)
		return subsMsg{subs: subs, err: err}
	}
}

func (m *Model) cmdCounts() tea.Cmd {
	api := m.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		counts, err := api.UnreadCounts(ctx)
		return countsMsg{counts: counts, err: err}
	}
}

func (m *Model) cmdStream(more bool) tea.Cmd {
	api, gen := m.api, m.gen
	q := greader.StreamQuery{Stream: m.stream.ID, ExcludeRead: m.unreadOnly, Count: m.opts.PageSize}
	if more {
		q.Continuation = m.cont
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		page, err := api.Stream(ctx, q)
		return streamMsg{gen: gen, more: more, page: page, err: err}
	}
}

func (m *Model) cmdSetRead(id string, read bool) tea.Cmd {
	api := m.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return markMsg{id: id, read: read, err: api.SetRead(ctx, []string{id}, read)}
	}
}

func (m *Model) cmdSetStar(id string, starred bool) tea.Cmd {
	api := m.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return starMsg{id: id, starred: starred, err: api.SetStarred(ctx, []string{id}, starred)}
	}
}

func (m *Model) cmdMarkAll() tea.Cmd {
	api, stream, before := m.api, m.stream.ID, m.loadedAt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return markAllMsg{err: api.MarkAllRead(ctx, stream, before)}
	}
}

// ---- update -----------------------------------------------------------------

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case subsMsg:
		if msg.err != nil {
			m.fail("Loading feeds", msg.err)
			return m, nil
		}
		m.subs = msg.subs
		m.subByID = make(map[string]greader.Subscription, len(msg.subs))
		for _, s := range msg.subs {
			m.subByID[s.ID] = s
		}
		m.rebuildNodes()
	case countsMsg:
		if msg.err != nil {
			m.fail("Loading unread counts", msg.err)
			return m, nil
		}
		m.counts = msg.counts
		if m.counts == nil {
			m.counts = map[string]int{}
		}
	case streamMsg:
		cmd := m.onStream(msg)
		return m, cmd
	case markMsg:
		if msg.err != nil {
			m.revertRead(msg.id, msg.read)
			m.fail("Could not update read state", msg.err)
		}
	case starMsg:
		if msg.err != nil {
			if i := m.indexOf(msg.id); i >= 0 && m.items[i].Starred == msg.starred {
				m.items[i].Starred = !msg.starred
				m.refreshReader()
			}
			m.fail("Could not update favourite", msg.err)
		}
	case markAllMsg:
		if msg.err != nil {
			m.fail("Could not mark all as read", msg.err)
		}
		cmd := m.cmdCounts() // resync with the server either way
		return m, cmd
	case tickMsg:
		return m, tea.Batch(m.cmdCounts(), tickCmd(m.opts.RefreshEvery))
	}
	return m, nil
}

func (m *Model) onStream(msg streamMsg) tea.Cmd {
	if msg.gen != m.gen {
		return nil // response for a stream we have since left
	}
	m.loading = false
	if msg.err != nil {
		m.fail("Loading articles", msg.err)
		return nil
	}
	if msg.more {
		seen := make(map[string]bool, len(m.items))
		for _, it := range m.items {
			seen[it.ID] = true
		}
		for _, it := range msg.page.Items {
			if !seen[it.ID] {
				m.items = append(m.items, it)
			}
		}
	} else {
		m.items = msg.page.Items
	}
	m.cont = msg.page.Continuation
	return m.maybeLoadMore()
}

func (m *Model) fail(what string, err error) {
	m.status = fmt.Sprintf("%s failed: %v", what, err)
	m.statusErr = true
}

func (m *Model) setStatus(s string) {
	m.status, m.statusErr = s, false
}

// ---- keys -------------------------------------------------------------------

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if m.confirm {
		m.confirm = false
		if k == "y" || k == "Y" {
			cmd := m.markAllRead()
			return m, cmd
		}
		m.setStatus("Cancelled")
		return m, nil
	}
	if m.help {
		m.help = false
		return m, nil
	}
	switch k {
	case "q":
		return m, tea.Quit
	case "?":
		m.help = true
		return m, nil
	case "tab":
		m.cycleFocus()
		return m, nil
	}
	m.status, m.statusErr = "", false
	switch m.focus {
	case paneSidebar:
		cmd := m.sidebarKey(k)
		return m, cmd
	case paneList:
		cmd := m.listKey(k)
		return m, cmd
	default:
		cmd := m.readerKey(msg)
		return m, cmd
	}
}

func (m *Model) cycleFocus() {
	switch m.focus {
	case paneSidebar:
		m.focus = paneList
	case paneList:
		if m.reading >= 0 {
			m.focus = paneReader
		} else {
			m.focus = paneSidebar
		}
	default:
		m.focus = paneSidebar
	}
}

func (m *Model) sidebarKey(k string) tea.Cmd {
	switch k {
	case "j", "down":
		m.moveSidebar(1)
	case "k", "up":
		m.moveSidebar(-1)
	case "g", "home":
		m.moveSidebar(-len(m.nodes))
	case "G", "end":
		m.moveSidebar(len(m.nodes))
	case "enter", "l", "right":
		n := m.nodes[m.sbCursor]
		if k != "enter" && n.kind == kindFolder && m.collapsed[n.id] {
			m.collapsed[n.id] = false
			m.rebuildNodes()
			return nil
		}
		return m.openNode(n)
	case "h", "left":
		n := m.nodes[m.sbCursor]
		switch {
		case n.kind == kindFolder && !m.collapsed[n.id]:
			m.collapsed[n.id] = true
			m.rebuildNodes()
		case n.kind == kindFeed:
			m.selectNode(func(c node) bool { return c.kind == kindFolder && c.id == n.parent })
		}
	case " ":
		if n := m.nodes[m.sbCursor]; n.kind == kindFolder {
			m.collapsed[n.id] = !m.collapsed[n.id]
			m.rebuildNodes()
		}
	case "u":
		return m.toggleUnread()
	case "r":
		return m.refresh()
	}
	return nil
}

func (m *Model) listKey(k string) tea.Cmd {
	page := max(m.bodyHeight()-1, 1)
	switch k {
	case "j", "down":
		return m.moveList(1)
	case "k", "up":
		return m.moveList(-1)
	case "g", "home":
		return m.moveList(-len(m.items))
	case "G", "end":
		return m.moveList(len(m.items))
	case "ctrl+d", "pgdown":
		return m.moveList(page)
	case "ctrl+u", "pgup":
		return m.moveList(-page)
	case "enter", "l", "right":
		return m.openArticle(m.listCursor)
	case "h", "left", "esc":
		m.focus = paneSidebar
	case "m":
		return m.toggleRead(m.listCursor)
	case "s":
		return m.toggleStar(m.listCursor)
	case "A":
		if len(m.items) > 0 {
			m.confirm = true
		}
	case "u":
		return m.toggleUnread()
	case "r":
		return m.refresh()
	case "o":
		m.openInBrowser(m.listCursor)
	}
	return nil
}

func (m *Model) readerKey(msg tea.KeyMsg) tea.Cmd {
	switch k := msg.String(); k {
	case "esc", "h", "left", "backspace":
		m.focus = paneList
	case "n", "ctrl+n":
		return m.stepArticle(1)
	case "p", "ctrl+p":
		return m.stepArticle(-1)
	case "m":
		return m.toggleRead(m.reading)
	case "s":
		return m.toggleStar(m.reading)
	case "o":
		m.openInBrowser(m.reading)
	case "r":
		return m.refresh()
	default:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return cmd
	}
	return nil
}

// ---- actions ----------------------------------------------------------------

func (m *Model) openNode(n node) tea.Cmd {
	m.focus = paneList
	if n.id == "" {
		m.setStatus("Nothing to show for this folder")
		return nil
	}
	if n.id == m.stream.ID && !m.loading {
		return nil
	}
	return m.loadStream(streamRef{ID: n.id, Title: n.label})
}

func (m *Model) loadStream(ref streamRef) tea.Cmd {
	m.gen++
	m.stream = ref
	m.items, m.cont = nil, ""
	m.listCursor, m.listOffset = 0, 0
	m.reading = -1
	if m.focus == paneReader {
		m.focus = paneList
	}
	m.loading = true
	m.loadedAt = m.opts.Now()
	return m.cmdStream(false)
}

func (m *Model) refresh() tea.Cmd {
	m.setStatus("Refreshing…")
	return tea.Batch(m.cmdSubs(), m.cmdCounts(), m.loadStream(m.stream))
}

func (m *Model) toggleUnread() tea.Cmd {
	m.unreadOnly = !m.unreadOnly
	if m.unreadOnly {
		m.setStatus("Showing unread articles only")
	} else {
		m.setStatus("Showing all articles")
	}
	return m.loadStream(m.stream)
}

func (m *Model) moveSidebar(delta int) {
	m.sbCursor = clamp(m.sbCursor+delta, 0, len(m.nodes)-1)
	m.sbOffset = scrollTo(m.sbCursor, m.sbOffset, m.bodyHeight(), len(m.nodes))
}

func (m *Model) moveList(delta int) tea.Cmd {
	if len(m.items) == 0 {
		return nil
	}
	m.listCursor = clamp(m.listCursor+delta, 0, len(m.items)-1)
	m.listOffset = scrollTo(m.listCursor, m.listOffset, m.bodyHeight(), len(m.items))
	return m.maybeLoadMore()
}

func (m *Model) maybeLoadMore() tea.Cmd {
	if m.loading || m.cont == "" || len(m.items)-m.listCursor > loadMoreMargin {
		return nil
	}
	m.loading = true
	return m.cmdStream(true)
}

func (m *Model) openArticle(i int) tea.Cmd {
	if i < 0 || i >= len(m.items) {
		return nil
	}
	m.reading = i
	m.listCursor = i
	m.listOffset = scrollTo(i, m.listOffset, m.bodyHeight(), len(m.items))
	m.focus = paneReader
	m.renderArticle(true)
	cmds := []tea.Cmd{m.maybeLoadMore()}
	if m.opts.MarkReadOnOpen && !m.items[i].Read {
		cmds = append(cmds, m.toggleRead(i))
	}
	return tea.Batch(cmds...)
}

func (m *Model) stepArticle(delta int) tea.Cmd {
	i := m.reading + delta
	if i < 0 || i >= len(m.items) {
		m.setStatus("No more articles")
		return m.maybeLoadMore()
	}
	return m.openArticle(i)
}

func (m *Model) toggleRead(i int) tea.Cmd {
	if i < 0 || i >= len(m.items) {
		return nil
	}
	it := &m.items[i]
	it.Read = !it.Read
	m.adjustCounts(it.FeedID, unreadDelta(it.Read))
	m.refreshReader()
	return m.cmdSetRead(it.ID, it.Read)
}

func (m *Model) revertRead(id string, read bool) {
	i := m.indexOf(id)
	if i < 0 || m.items[i].Read != read {
		return
	}
	m.items[i].Read = !read
	m.adjustCounts(m.items[i].FeedID, unreadDelta(!read))
	m.refreshReader()
}

func (m *Model) toggleStar(i int) tea.Cmd {
	if i < 0 || i >= len(m.items) {
		return nil
	}
	it := &m.items[i]
	it.Starred = !it.Starred
	m.refreshReader()
	return m.cmdSetStar(it.ID, it.Starred)
}

func (m *Model) markAllRead() tea.Cmd {
	n := 0
	for i := range m.items {
		if !m.items[i].Read {
			m.items[i].Read = true
			m.adjustCounts(m.items[i].FeedID, -1)
			n++
		}
	}
	m.refreshReader()
	m.setStatus(fmt.Sprintf("Marked %s as read", m.stream.Title))
	return m.cmdMarkAll()
}

func (m *Model) openInBrowser(i int) {
	if i < 0 || i >= len(m.items) {
		return
	}
	if err := m.opts.OpenURL(m.items[i].URL); err != nil {
		m.fail("Open in browser", err)
		return
	}
	m.setStatus("Opened in browser")
}

func (m *Model) indexOf(id string) int {
	for i := range m.items {
		if m.items[i].ID == id {
			return i
		}
	}
	return -1
}

// unreadDelta is the change in unread count when an article becomes read (-1)
// or unread (+1).
func unreadDelta(nowRead bool) int {
	if nowRead {
		return -1
	}
	return 1
}

// adjustCounts applies delta to the unread counters of a feed, its folders and
// the main stream, mirroring what the server will report next refresh.
func (m *Model) adjustCounts(feedID string, delta int) {
	bump := func(id string) {
		if id == "" {
			return
		}
		m.counts[id] = max(m.counts[id]+delta, 0)
	}
	bump(feedID)
	bump(greader.StreamReadingList)
	for _, c := range m.subByID[feedID].Categories {
		bump(c.ID)
	}
}

// ---- sidebar model ----------------------------------------------------------

func (m *Model) rebuildNodes() {
	prev := ""
	if m.sbCursor >= 0 && m.sbCursor < len(m.nodes) {
		prev = m.nodes[m.sbCursor].key()
	}

	type folder struct {
		id, label string
		feeds     []greader.Subscription
	}
	byID := map[string]*folder{}
	var folders []*folder
	for _, s := range m.subs {
		cats := s.Categories
		if len(cats) == 0 {
			cats = []greader.Category{{Label: "Uncategorized"}}
		}
		for _, c := range cats {
			f, ok := byID[c.ID]
			if !ok {
				f = &folder{id: c.ID, label: c.Label}
				byID[c.ID] = f
				folders = append(folders, f)
			}
			f.feeds = append(f.feeds, s)
		}
	}
	sort.Slice(folders, func(i, j int) bool {
		return strings.ToLower(folders[i].label) < strings.ToLower(folders[j].label)
	})

	nodes := []node{
		{kind: kindAll, id: greader.StreamReadingList, label: "Main stream"},
		{kind: kindStarred, id: greader.StreamStarred, label: "Favourites"},
	}
	for _, f := range folders {
		nodes = append(nodes, node{kind: kindFolder, id: f.id, label: f.label})
		if m.collapsed[f.id] {
			continue
		}
		sort.Slice(f.feeds, func(i, j int) bool {
			return strings.ToLower(f.feeds[i].Title) < strings.ToLower(f.feeds[j].Title)
		})
		for _, s := range f.feeds {
			nodes = append(nodes, node{kind: kindFeed, id: s.ID, label: s.Title, parent: f.id})
		}
	}
	m.nodes = nodes
	m.sbCursor = clamp(m.sbCursor, 0, len(nodes)-1)
	m.selectNode(func(n node) bool { return n.key() == prev })
	m.sbOffset = scrollTo(m.sbCursor, m.sbOffset, m.bodyHeight(), len(nodes))
}

// selectNode moves the sidebar cursor to the first node matching pred.
func (m *Model) selectNode(pred func(node) bool) {
	for i, n := range m.nodes {
		if pred(n) {
			m.sbCursor = i
			m.sbOffset = scrollTo(i, m.sbOffset, m.bodyHeight(), len(m.nodes))
			return
		}
	}
}
