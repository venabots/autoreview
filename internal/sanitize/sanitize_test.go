package sanitize

import "testing"

func TestLine(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain text is kept", "Add retry logic", "Add retry logic"},
		{"an escape sequence loses its escape", "fix \x1b[2Jscreen", "fix [2Jscreen"},
		{"a bell is dropped", "ding\x07", "ding"},
		{"a newline and a tab are dropped", "one\ntwo\tthree", "onetwothree"},
		{"a bidi override is dropped", "abc\u202Edef", "abcdef"},
		{"a bidi embedding is dropped", "a\u202Ab\u202Cc", "abc"},
		{"a bidi isolate is dropped", "a\u2066b\u2069c", "abc"},
		{"zero-width characters are dropped", "a\u200Bb\u200Fc\uFEFFd", "abcd"},
		{"the Arabic letter mark is dropped", "a\u061Cb", "ab"},
		{"a C1 control is dropped", "a\u009Bb", "ab"},
		{"delete is dropped", "a\x7Fb", "ab"},
		{"wide and accented characters are kept", "日本語 café", "日本語 café"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Line(c.in); got != c.want {
				t.Errorf("Line(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestBlock(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"newlines and tabs are kept", "a\n\tb", "a\n\tb"},
		{"an escape is dropped on every line", "a\x1b[1m\nb\x1b[0m", "a[1m\nb[0m"},
		{"a carriage return is dropped", "a\r\nb", "a\nb"},
		{"a bidi override is dropped", "a\u202Eb\nc", "ab\nc"},
		{"one trailing newline is not a line", "a\n", "a"},
		{"blank lines inside are kept", "a\n\nb", "a\n\nb"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Block(c.in); got != c.want {
				t.Errorf("Block(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestStyled(t *testing.T) {
	link := "\x1b]8;id=7;https://example.com/x\x07here\x1b]8;;\x07"
	cases := []struct {
		name, in, want string
	}{
		{"a color sequence is kept", "\x1b[1;38;5;212mbold\x1b[0m", "\x1b[1;38;5;212mbold\x1b[0m"},
		{"a hyperlink is kept", link, link},
		{"a hyperlink that ends in ST is kept", "\x1b]8;;https://example.com\x1b\\x", "\x1b]8;;https://example.com\x1b\\x"},
		{"newlines and tabs are kept", "a\n\tb", "a\n\tb"},
		{"a sequence that clears the screen loses its escape", "a\x1b[2Jb", "a[2Jb"},
		{"a sequence that moves the cursor loses its escape", "a\x1b[10;1Hb", "a[10;1Hb"},
		{"a sequence that sets the title loses its escape and its bell", "\x1b]0;mine\x07a", "]0;minea"},
		{"a hyperlink with no end loses its escape", "\x1b]8;;https://example.com", "]8;;https://example.com"},
		{"a hyperlink with a control inside loses its escapes", "\x1b]8;;a\x1b[2Jb\x07", "]8;;a[2Jb"},
		{"a lone escape is dropped", "a\x1bb", "ab"},
		{"a bidi override is dropped", "a\u202Eb", "ab"},
		{"a zero-width character is dropped", "a\u200Bb", "ab"},
		{"a carriage return is dropped", "a\r\nb", "a\nb"},
		{"wide and accented characters are kept", "日本語 café", "日本語 café"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Styled(c.in); got != c.want {
				t.Errorf("Styled(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
