// Package render turns untrusted feed HTML into plain, wrapped terminal text.
//
// Feed content is attacker-controlled, so everything that reaches the terminal
// goes through Sanitize, which removes control characters (and with them any
// ANSI/OSC escape sequences) and bidirectional-override characters.
package render

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/net/html"
)

const (
	maxInput  = 2 << 20 // bytes of HTML we are willing to parse
	minWidth  = 20
	ruleWidth = 40
)

var headingStyle = lipgloss.NewStyle().Bold(true)

// Result is rendered article text plus the links it references.
type Result struct {
	Text  string
	Links []string
}

// Sanitize removes control characters (except newline and tab), bidi
// overrides and Unicode line/paragraph separators.
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\u2028' || r == '\u2029':
			return ' '
		case unicode.IsControl(r):
			return -1
		case r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069', r == '\u200e', r == '\u200f':
			return -1
		}
		return r
	}, s)
}

// SingleLine sanitizes s and collapses all whitespace to single spaces.
func SingleLine(s string) string {
	return strings.Join(strings.Fields(Sanitize(s)), " ")
}

// HTML converts an HTML fragment to wrapped plain text of at most width columns.
// Links are replaced by numbered markers and returned in Result.Links; the
// list is also appended to the text.
func HTML(src string, width int) Result {
	width = max(width, minWidth)
	if len(src) > maxInput {
		src = src[:maxInput]
	}
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return Result{Text: ansi.Wrap(Sanitize(src), width, "")}
	}
	c := &converter{width: width, linkIdx: map[string]int{}}
	c.walk(doc)
	c.flush()
	c.trimBlank()

	if len(c.links) > 0 {
		c.blank()
		c.out = append(c.out, headingStyle.Render("Links"))
		for i, l := range c.links {
			line := fmt.Sprintf("[%d] %s", i+1, l)
			c.out = append(c.out, strings.Split(ansi.Hardwrap(line, width, true), "\n")...)
		}
	}
	return Result{Text: strings.Join(c.out, "\n"), Links: c.links}
}

type listState struct {
	ordered bool
	n       int
	bw      int // display width of the current item's bullet
}

type converter struct {
	width   int
	out     []string
	cur     strings.Builder
	links   []string
	linkIdx map[string]int
	quote   int
	lists   []*listState
	bullet  string
	pre     bool
	heading bool
}

var skipTags = map[string]bool{
	"script": true, "style": true, "head": true, "noscript": true, "template": true,
	"svg": true, "canvas": true, "object": true, "embed": true, "math": true,
}

var blockTags = map[string]bool{
	"div": true, "section": true, "article": true, "header": true, "footer": true,
	"main": true, "aside": true, "nav": true, "figure": true, "figcaption": true,
	"address": true, "details": true, "summary": true, "dl": true, "dt": true,
	"dd": true, "tr": true, "thead": true, "tbody": true, "tfoot": true, "form": true,
	"fieldset": true, "center": true,
}

func (c *converter) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		c.text(n.Data)
		return
	case html.ElementNode:
		if c.element(n) {
			return
		}
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.walk(ch)
	}
}

// element handles an element; it returns true if it already walked the children.
func (c *converter) element(n *html.Node) bool {
	tag := n.Data
	switch {
	case skipTags[tag]:
		return true
	case tag == "br":
		c.cur.WriteString("\n")
		return true
	case tag == "hr":
		c.flush()
		c.blank()
		c.out = append(c.out, c.quotePrefix()+strings.Repeat("─", min(ruleWidth, c.width)))
		c.blank()
		return true
	case tag == "img":
		c.image(n)
		return true
	case tag == "iframe" || tag == "video" || tag == "audio":
		c.media(n, tag)
		return true
	case tag == "a":
		c.anchor(n)
		return true
	case len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6':
		c.block(n, true, func() { c.heading = true })
		return true
	case tag == "p" || tag == "table":
		c.block(n, true, nil)
		return true
	case tag == "blockquote":
		c.flush()
		c.blank()
		c.quote++
		c.children(n)
		c.flush()
		c.quote--
		c.blank()
		return true
	case tag == "pre":
		c.flush()
		c.blank()
		c.pre = true
		c.children(n)
		c.pre = false
		c.flushPre()
		c.blank()
		return true
	case tag == "ul" || tag == "ol":
		c.flush()
		if len(c.lists) == 0 {
			c.blank()
		}
		c.lists = append(c.lists, &listState{ordered: tag == "ol"})
		c.children(n)
		c.flush()
		c.lists = c.lists[:len(c.lists)-1]
		if len(c.lists) == 0 {
			c.blank()
		}
		return true
	case tag == "li":
		c.listItem(n)
		return true
	case tag == "td" || tag == "th":
		if strings.TrimSpace(c.cur.String()) != "" {
			c.cur.WriteString(" | ")
		}
		return false
	case blockTags[tag]:
		c.block(n, false, nil)
		return true
	}
	return false
}

func (c *converter) children(n *html.Node) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.walk(ch)
	}
}

func (c *converter) block(n *html.Node, para bool, before func()) {
	c.flush()
	if para {
		c.blank()
	}
	if before != nil {
		before()
	}
	c.children(n)
	c.flush()
	if para {
		c.blank()
	}
}

func (c *converter) listItem(n *html.Node) {
	c.flush()
	if len(c.lists) == 0 {
		c.lists = append(c.lists, &listState{})
	}
	top := c.lists[len(c.lists)-1]
	top.n++
	marker := "• "
	if top.ordered {
		marker = fmt.Sprintf("%d. ", top.n)
	}
	top.bw = ansi.StringWidth(marker)
	c.bullet = marker
	c.children(n)
	c.flush()
	c.bullet = ""
}

func (c *converter) anchor(n *html.Node) {
	c.children(n)
	href := attr(n, "href")
	if !webURL(href) {
		return
	}
	idx, ok := c.linkIdx[href]
	if !ok {
		c.links = append(c.links, Sanitize(href))
		idx = len(c.links)
		c.linkIdx[href] = idx
	}
	if !strings.HasSuffix(c.cur.String(), " ") && c.cur.Len() > 0 {
		c.cur.WriteString(" ")
	}
	fmt.Fprintf(&c.cur, "[%d]", idx)
}

func (c *converter) image(n *html.Node) {
	if w := attr(n, "width"); w == "0" || w == "1" {
		return // tracking pixel
	}
	if h := attr(n, "height"); h == "0" || h == "1" {
		return
	}
	alt := SingleLine(attr(n, "alt"))
	if alt == "" {
		c.text(" [image] ")
		return
	}
	c.text(" [image: " + alt + "] ")
}

func (c *converter) media(n *html.Node, kind string) {
	src := attr(n, "src")
	if src == "" {
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type == html.ElementNode && ch.Data == "source" {
				src = attr(ch, "src")
				break
			}
		}
	}
	label := " [" + kind + "] "
	if kind == "iframe" {
		label = " [embedded content] "
	}
	c.text(label)
	if webURL(src) {
		idx, ok := c.linkIdx[src]
		if !ok {
			c.links = append(c.links, Sanitize(src))
			idx = len(c.links)
			c.linkIdx[src] = idx
		}
		fmt.Fprintf(&c.cur, "[%d]", idx)
	}
}

func (c *converter) text(s string) {
	s = Sanitize(s)
	if c.pre {
		c.cur.WriteString(strings.ReplaceAll(s, "\t", "    "))
		return
	}
	prevSpace := c.cur.Len() == 0 || strings.HasSuffix(c.cur.String(), " ") || strings.HasSuffix(c.cur.String(), "\n")
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				c.cur.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		c.cur.WriteRune(r)
		prevSpace = false
	}
}

func (c *converter) quotePrefix() string { return strings.Repeat("│ ", c.quote) }

func (c *converter) prefixes() (first, cont string) {
	q := c.quotePrefix()
	if len(c.lists) == 0 {
		return q, q
	}
	var indent strings.Builder
	for _, l := range c.lists[:len(c.lists)-1] {
		indent.WriteString(strings.Repeat(" ", l.bw))
	}
	top := c.lists[len(c.lists)-1]
	cont = q + indent.String() + strings.Repeat(" ", top.bw)
	first = cont
	if c.bullet != "" {
		first = q + indent.String() + c.bullet
	}
	return first, cont
}

func (c *converter) flush() {
	if c.pre {
		return // preformatted text is emitted whole by flushPre
	}
	raw := c.cur.String()
	c.cur.Reset()
	heading := c.heading
	c.heading = false
	first, cont := c.prefixes()
	w := max(c.width-ansi.StringWidth(cont), minWidth/2)
	var lines []string
	for _, seg := range strings.Split(raw, "\n") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		lines = append(lines, strings.Split(ansi.Wrap(seg, w, ""), "\n")...)
	}
	if len(lines) == 0 {
		return
	}
	for i, l := range lines {
		if heading {
			l = headingStyle.Render(l)
		}
		p := cont
		if i == 0 {
			p = first
		}
		c.out = append(c.out, p+l)
	}
	c.bullet = ""
}

func (c *converter) flushPre() {
	raw := strings.TrimRight(c.cur.String(), "\n")
	c.cur.Reset()
	if strings.TrimSpace(raw) == "" {
		return
	}
	_, cont := c.prefixes()
	w := max(c.width-ansi.StringWidth(cont)-4, minWidth/2)
	for _, line := range strings.Split(raw, "\n") {
		for _, part := range strings.Split(ansi.Hardwrap(strings.TrimRight(line, " "), w, true), "\n") {
			c.out = append(c.out, cont+"    "+part)
		}
	}
}

func (c *converter) blank() {
	if len(c.out) > 0 && c.out[len(c.out)-1] != "" {
		c.out = append(c.out, "")
	}
}

func (c *converter) trimBlank() {
	for len(c.out) > 0 && c.out[len(c.out)-1] == "" {
		c.out = c.out[:len(c.out)-1]
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func webURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
