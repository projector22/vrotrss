package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/projector22/vrotrss/internal/greader"
	"github.com/projector22/vrotrss/internal/render"
)

const (
	minSidebar = 22
	maxSidebar = 34
	maxReading = 100
	dateWidth  = 10
)

func (m *Model) compact() bool { return m.width < compactBelow }

func (m *Model) bodyHeight() int { return max(m.height-2, 1) }

// dims returns the sidebar and main pane widths.
func (m *Model) dims() (sidebar, main int) {
	if m.compact() {
		return m.width, m.width
	}
	sidebar = clamp(m.width/4, minSidebar, maxSidebar)
	return sidebar, m.width - sidebar - 1
}

func (m *Model) layout() {
	_, mw := m.dims()
	m.vp.Width, m.vp.Height = mw, m.bodyHeight()
	m.sbOffset = scrollTo(m.sbCursor, m.sbOffset, m.bodyHeight(), len(m.nodes))
	m.listOffset = scrollTo(m.listCursor, m.listOffset, m.bodyHeight(), len(m.items))
	m.refreshReader()
}

// View renders the whole screen: header, body and footer, always exactly
// height lines of width columns.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	lines := []string{m.viewHeader()}
	lines = append(lines, m.viewBody()...)
	lines = append(lines, m.viewFooter())
	return strings.Join(lines, "\n")
}

func (m *Model) viewHeader() string {
	filter := "all articles"
	if m.unreadOnly {
		filter = "unread only"
	}
	left := " vrotrss  │  " + render.SingleLine(m.stream.Title)
	if n := m.counts[m.stream.ID]; n > 0 && m.stream.ID != greader.StreamStarred {
		left += fmt.Sprintf(" (%d)", n)
	}
	right := filter + " "
	if m.loading {
		right = "loading…  " + right
	}
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return styHeader.Render(fit(left, m.width))
	}
	return styHeader.Render(left + strings.Repeat(" ", gap) + right)
}

func (m *Model) viewFooter() string {
	if m.confirm {
		return styAccentB.Render(fit(fmt.Sprintf(" Mark all loaded articles in “%s” as read? (y/n)",
			render.SingleLine(m.stream.Title)), m.width))
	}
	if m.status != "" {
		st := styFooter
		if m.statusErr {
			st = styError
		}
		return st.Render(fit(" "+render.SingleLine(m.status), m.width))
	}
	var hint string
	switch {
	case m.help:
		hint = "press any key to close help"
	case m.focus == paneSidebar:
		hint = "j/k move · enter open · h/l fold · u unread/all · r refresh · tab switch · ? help · q quit"
	case m.focus == paneList:
		hint = "j/k move · enter read · m read/unread · s star · A all read · u unread/all · o browser · ? help"
	default:
		hint = fmt.Sprintf("j/k scroll · n/p next/prev · m read · s star · o browser · esc back · %d%%",
			int(m.vp.ScrollPercent()*100))
	}
	return styFooter.Render(fit(" "+hint, m.width))
}

func (m *Model) viewBody() []string {
	h := m.bodyHeight()
	sw, mw := m.dims()
	if m.compact() {
		switch {
		case m.help:
			return m.viewHelp(mw, h)
		case m.focus == paneSidebar:
			return m.viewSidebar(sw, h)
		case m.focus == paneReader:
			return m.viewReader(mw, h)
		default:
			return m.viewList(mw, h)
		}
	}
	var main []string
	switch {
	case m.help:
		main = m.viewHelp(mw, h)
	case m.focus == paneReader:
		main = m.viewReader(mw, h)
	default:
		main = m.viewList(mw, h)
	}
	side := m.viewSidebar(sw, h)
	sep := styBorder.Render("│")
	out := make([]string, h)
	for i := range out {
		out[i] = side[i] + sep + main[i]
	}
	return out
}

// ---- sidebar ----------------------------------------------------------------

func (m *Model) viewSidebar(w, h int) []string {
	lines := make([]string, 0, h)
	for i := m.sbOffset; i < len(m.nodes) && len(lines) < h; i++ {
		lines = append(lines, m.sidebarRow(i, w))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return lines
}

func (m *Model) sidebarRow(i, w int) string {
	n := m.nodes[i]
	cnt := ""
	if n.kind != kindStarred {
		cnt = count(m.counts[n.id])
	}
	name := render.SingleLine(n.label)
	var label string
	switch n.kind {
	case kindAll:
		label = "≡ " + name
	case kindStarred:
		label = "★ " + name
	case kindFolder:
		arrow := "▾"
		if m.collapsed[n.id] {
			arrow = "▸"
		}
		label = arrow + " " + name
	default:
		label = "    " + name
	}

	room := w - 2 - ansi.StringWidth(cnt)
	if cnt != "" {
		room--
	}
	text := fit(label, max(room, 0))
	plain := " " + text
	if cnt != "" {
		plain += " " + cnt
	}
	plain = fit(plain+" ", w)

	selected := i == m.sbCursor
	current := n.id == m.stream.ID && n.id != ""
	switch {
	case selected && m.focus == paneSidebar:
		return styCursor.Render(plain)
	case selected:
		return styCursorBg.Render(plain)
	case current:
		return styAccentB.Render(plain)
	case n.kind == kindFeed && m.counts[n.id] == 0:
		return styMuted.Render(plain)
	case n.kind == kindFolder || n.kind == kindAll:
		return styBold.Render(plain)
	}
	return plain
}

// ---- article list -----------------------------------------------------------

func (m *Model) viewList(w, h int) []string {
	lines := make([]string, 0, h)
	if len(m.items) == 0 {
		msg := " No articles"
		switch {
		case m.loading:
			msg = " Loading…"
		case m.unreadOnly:
			msg = " No unread articles — press u to show all"
		}
		lines = append(lines, styMuted.Render(fit(msg, w)))
	}
	for i := m.listOffset; i < len(m.items) && len(lines) < h; i++ {
		lines = append(lines, m.listRow(i, w))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return lines
}

func (m *Model) listRow(i, w int) string {
	it := m.items[i]
	feedW, dateW := 0, 0
	if w >= 60 {
		feedW = clamp(w/5, 12, 22)
	}
	if w >= 40 {
		dateW = dateWidth
	}
	titleW := w - 4
	if feedW > 0 {
		titleW -= feedW + 1
	}
	if dateW > 0 {
		titleW -= dateW + 1
	}
	titleW = max(titleW, 1)

	marker, star := "  ", "  "
	if !it.Read {
		marker = "● "
	}
	if it.Starred {
		star = "★ "
	}
	feed, date := "", ""
	if feedW > 0 {
		feed = fit(render.SingleLine(it.FeedTitle), feedW) + " "
	}
	if dateW > 0 {
		date = " " + padLeft(shortDate(it.Published, m.opts.Now(), m.opts.Location), dateW)
	}
	title := fit(render.SingleLine(it.Title), titleW)

	if i == m.listCursor {
		st := styCursor
		if m.focus == paneSidebar {
			st = styCursorBg
		}
		return st.Render(fit(marker+star+feed+title+date, w))
	}
	var b strings.Builder
	if it.Read {
		b.WriteString(styMuted.Render(marker))
		b.WriteString(starStyled(star, it.Starred))
		b.WriteString(styMuted.Render(feed + title + date))
	} else {
		b.WriteString(styAccent.Render(marker))
		b.WriteString(starStyled(star, it.Starred))
		b.WriteString(styMuted.Render(feed))
		b.WriteString(styBold.Render(title))
		b.WriteString(styMuted.Render(date))
	}
	return fit(b.String(), w)
}

func starStyled(star string, on bool) string {
	if on {
		return styStar.Render(star)
	}
	return star
}

// ---- reader -----------------------------------------------------------------

func (m *Model) readerWidth() int {
	_, mw := m.dims()
	return clamp(mw-4, 20, maxReading)
}

// refreshReader re-renders the open article (e.g. after a read-state change)
// while preserving the scroll position.
func (m *Model) refreshReader() {
	if m.reading >= 0 && m.reading < len(m.items) {
		m.renderArticle(false)
	}
}

func (m *Model) renderArticle(resetTop bool) {
	if m.reading < 0 || m.reading >= len(m.items) {
		return
	}
	it := m.items[m.reading]
	w := m.readerWidth()
	const margin = "  "

	var out []string
	for _, l := range strings.Split(ansi.Wrap(render.SingleLine(it.Title), w, ""), "\n") {
		out = append(out, margin+styBold.Render(l))
	}

	meta := []string{render.SingleLine(it.FeedTitle)}
	if a := render.SingleLine(it.Author); a != "" {
		meta = append(meta, a)
	}
	if !it.Published.IsZero() {
		meta = append(meta, it.Published.In(m.opts.Location).Format("Mon, 2 Jan 2006 15:04"))
	}
	out = append(out, margin+styAccent.Render(ansi.Truncate(strings.Join(meta, " · "), w, "…")))

	flags := []string{"read"}
	if !it.Read {
		flags = []string{"unread"}
	}
	if it.Starred {
		flags = append(flags, "★ favourite")
	}
	out = append(out, margin+styMuted.Render(strings.Join(flags, " · ")))
	if it.URL != "" {
		for _, l := range strings.Split(ansi.Hardwrap(render.SingleLine(it.URL), w, true), "\n") {
			out = append(out, margin+styMuted.Render(l))
		}
	}
	out = append(out, margin+styBorder.Render(strings.Repeat("─", w)), "")

	body := render.HTML(it.Content, w).Text
	if strings.TrimSpace(body) == "" {
		body = styMuted.Render("(no content — press o to open the original in your browser)")
	}
	for _, l := range strings.Split(body, "\n") {
		out = append(out, margin+l)
	}

	top := m.vp.YOffset
	m.vp.SetContent(strings.Join(out, "\n"))
	if resetTop {
		m.vp.GotoTop()
	} else {
		m.vp.SetYOffset(top)
	}
}

func (m *Model) viewReader(w, h int) []string {
	lines := strings.Split(m.vp.View(), "\n")
	out := make([]string, h)
	for i := range out {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = fit(l, w)
	}
	return out
}

// ---- help -------------------------------------------------------------------

var helpText = []string{
	"vrotrss — FreshRSS in your terminal",
	"",
	"Navigation",
	"  tab               switch between sidebar, list and reader",
	"  j / k, ↓ / ↑      move down / up (scroll in the reader)",
	"  g / G             jump to top / bottom",
	"  h / l, ← / →      sidebar: fold / unfold folder; list: back / open",
	"  enter             sidebar: open feed or folder; list: read article",
	"  esc               go back one level",
	"",
	"Articles",
	"  m                 toggle read / unread",
	"  s                 toggle favourite",
	"  A                 mark everything in this stream as read",
	"  n / p             next / previous article (reader)",
	"  o                 open in your browser",
	"  u                 toggle unread-only / all articles",
	"  r                 refresh feeds, counts and articles",
	"",
	"  ?                 this help        q / ctrl+c   quit",
}

func (m *Model) viewHelp(w, h int) []string {
	out := make([]string, h)
	for i := range out {
		l := ""
		if i < len(helpText) {
			l = " " + helpText[i]
		}
		if i == 0 {
			out[i] = styAccentB.Render(fit(l, w))
		} else {
			out[i] = fit(l, w)
		}
	}
	return out
}
