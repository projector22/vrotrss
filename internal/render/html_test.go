package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSanitizeRemovesEscapesAndBidi(t *testing.T) {
	in := "ok\x1b[31mred\x1b[0m\x1b]0;pwned\x07 \u202eevil\u2069 a\rb\u2028c\ttab\nnl\x00"
	got := Sanitize(in)
	for _, bad := range []string{"\x1b", "\x07", "\u202e", "\u2069", "\r", "\x00", "\u2028"} {
		if strings.Contains(got, bad) {
			t.Errorf("Sanitize left %q in %q", bad, got)
		}
	}
	if !strings.Contains(got, "\ttab\nnl") {
		t.Errorf("Sanitize should keep tab/newline: %q", got)
	}
}

func TestSingleLine(t *testing.T) {
	got := SingleLine("  Hello \n\t\x1bWorld  ")
	if got != "Hello World" {
		t.Errorf("SingleLine = %q", got)
	}
}

func TestParagraphsAndWrapping(t *testing.T) {
	res := HTML("<p>The quick brown fox jumps over the lazy dog and keeps running.</p><p>Second.</p>", 30)
	lines := strings.Split(res.Text, "\n")
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 30 {
			t.Errorf("line %q is %d wide", l, w)
		}
	}
	if !strings.Contains(res.Text, "\n\nSecond.") {
		t.Errorf("paragraphs should be separated by a blank line:\n%s", res.Text)
	}
}

func TestLinksAreNumberedAndDeduplicated(t *testing.T) {
	res := HTML(`<p>See <a href="https://a.example/x">this</a> and <a href="https://a.example/x">that</a>,
		<a href="https://b.example/">other</a>, <a href="javascript:alert(1)">bad</a>, <a href="/relative">rel</a>.</p>`, 80)
	if len(res.Links) != 2 || res.Links[0] != "https://a.example/x" || res.Links[1] != "https://b.example/" {
		t.Fatalf("links = %v", res.Links)
	}
	for _, want := range []string{"this [1]", "that [1]", "other [2]", "[1] https://a.example/x", "[2] https://b.example/"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%s", want, res.Text)
		}
	}
	if strings.Contains(res.Text, "javascript") || strings.Contains(res.Text, "relative") {
		t.Errorf("unsafe/relative links must not be listed:\n%s", res.Text)
	}
}

func TestLists(t *testing.T) {
	res := HTML(`<ul><li>one</li><li>two<ol><li>deep a</li><li>deep b</li></ol></li></ul><p>after</p>`, 40)
	want := []string{"• one", "• two", "  1. deep a", "  2. deep b", "", "after"}
	got := strings.Split(res.Text, "\n")
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestBlockquoteAndHeadingAndRule(t *testing.T) {
	res := HTML(`<h2>Title</h2><blockquote><p>quoted text</p></blockquote><hr><p>end</p>`, 40)
	for _, want := range []string{"Title", "│ quoted text", "────", "end"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%s", want, res.Text)
		}
	}
}

func TestPreformattedKeepsLayout(t *testing.T) {
	res := HTML("<pre><code>func main() {\n\tprintln(\"hi\")\n}</code></pre>", 40)
	for _, want := range []string{"    func main() {", "        println(\"hi\")", "    }"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%q", want, res.Text)
		}
	}
}

func TestPreWithNestedBlockDoesNotDropText(t *testing.T) {
	res := HTML("<pre><div>kept</div></pre>", 40)
	if !strings.Contains(res.Text, "kept") {
		t.Errorf("text lost: %q", res.Text)
	}
}

func TestImagesAndMedia(t *testing.T) {
	res := HTML(`<p>a<img src="x.png" alt="A cat"> b <img src="p.gif" width="1" height="1"> c <img src="y.png"></p>
		<p><iframe src="https://video.example/embed/1"></iframe></p><p><video><source src="https://v.example/a.mp4"></video></p>`, 80)
	for _, want := range []string{"[image: A cat]", "[image]", "[embedded content] [1]", "[video] [2]"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%s", want, res.Text)
		}
	}
	if strings.Count(res.Text, "[image") != 2 {
		t.Errorf("tracking pixel should be dropped:\n%s", res.Text)
	}
}

func TestScriptsAndStylesAreDropped(t *testing.T) {
	res := HTML(`<p>visible</p><script>alert('x')</script><style>p{color:red}</style><noscript>nope</noscript>`, 40)
	if res.Text != "visible" {
		t.Errorf("got %q", res.Text)
	}
}

func TestTerminalEscapesInContentAreNeutralised(t *testing.T) {
	res := HTML("<p>hello \x1b[2J\x1b]52;c;ZXZpbA==\x07 <a href=\"https://e.example/\x1b[31m\">link</a></p>", 60)
	if strings.Contains(res.Text, "\x1b") || strings.Contains(res.Text, "\x07") {
		t.Errorf("escape sequences survived: %q", res.Text)
	}
	for _, l := range res.Links {
		if strings.Contains(l, "\x1b") {
			t.Errorf("escape in link %q", l)
		}
	}
}

func TestTablesAndLineBreaks(t *testing.T) {
	res := HTML(`<table><tr><td>a</td><td>b</td></tr><tr><td>c</td><td>d</td></tr></table>line1<br>line2`, 40)
	for _, want := range []string{"a | b", "c | d", "line1", "line2"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("missing %q in:\n%s", want, res.Text)
		}
	}
}

func TestEmptyAndPlainInput(t *testing.T) {
	if got := HTML("", 40).Text; got != "" {
		t.Errorf("empty input -> %q", got)
	}
	if got := HTML("just text, no tags", 40).Text; got != "just text, no tags" {
		t.Errorf("plain text -> %q", got)
	}
}

func TestHugeInputIsTruncated(t *testing.T) {
	res := HTML("<p>"+strings.Repeat("word ", 1_000_000)+"</p>", 80)
	if res.Text == "" {
		t.Error("expected some output")
	}
}

func TestVeryNarrowWidthDoesNotPanic(t *testing.T) {
	res := HTML("<ul><li>a rather long list item that must wrap</li></ul><pre>xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx</pre>", 1)
	if res.Text == "" {
		t.Error("expected output")
	}
}
