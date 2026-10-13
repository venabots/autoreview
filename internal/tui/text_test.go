package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func plainText(l line) string {
	var b strings.Builder
	for _, s := range l {
		b.WriteString(s.text)
	}
	return b.String()
}

func TestFitMakesALineExactlyAsWideAsAsked(t *testing.T) {
	cases := []struct {
		name  string
		in    line
		width int
		want  string
	}{
		{"a short line is padded", line{text("abc")}, 6, "abc   "},
		{"a line that fits is kept", line{text("abcdef")}, 6, "abcdef"},
		{"a long line is cut with an ellipsis", line{text("abcdefgh")}, 6, "abcde…"},
		{"the cut can fall in a later span", line{text("ab"), text("cdefgh")}, 6, "abcde…"},
		{"the cut can fall on a span boundary", line{text("abcde"), text("fgh")}, 6, "abcde…"},
		{"spans after the cut are dropped", line{text("abcdefgh"), text("ijk")}, 6, "abcde…"},
		{"an empty line is all padding", line{}, 3, "   "},
		{"width one keeps only the ellipsis", line{text("abc")}, 1, "…"},
		{"a wide character counts two columns", line{text("日本語")}, 6, "日本語"},
		{"a wide character that does not fit leaves a space", line{text("日本語")}, 4, "日… "},
		{"no width gives nothing", line{text("abc")}, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := plainText(fit(c.in, c.width))
			if got != c.want {
				t.Errorf("fit = %q, want %q", got, c.want)
			}
			if w := ansi.StringWidth(got); w != max(c.width, 0) {
				t.Errorf("fit is %d columns wide, want %d", w, c.width)
			}
		})
	}
}

func TestFitDoesNotChangeItsArgument(t *testing.T) {
	in := make(line, 1, 4)
	in[0] = text("abc")
	_ = fit(in, 10)
	if len(in) != 1 || in[0].text != "abc" {
		t.Errorf("fit changed its argument: %+v", in)
	}
	if extra := in[:2][1]; extra.text != "" {
		t.Errorf("fit wrote padding into its argument's spare capacity: %q", extra.text)
	}
}

func TestRenderKeepsTheTextAndItsWidth(t *testing.T) {
	l := line{styled("#9", numberStyle), text(" "), styled("ready", plainStyle), text("")}
	for _, reverse := range []bool{false, true} {
		got := l.render(reverse)
		if ansi.Strip(got) != "#9 ready" {
			t.Errorf("render(%v) drew %q, want the text #9 ready", reverse, ansi.Strip(got))
		}
	}
}
