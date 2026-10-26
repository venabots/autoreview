package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/engine"
)

func withHealth(t *testing.T, h *engine.Health) Model {
	t.Helper()
	m, hs := newModel(t, 160, 30)
	hs.eng.snap.Health = h
	return send(m, snapshotMsg{snap: hs.eng.snap})
}

// The footer says what autoreview doctor would say, so nobody has to run
// it to learn the panel is down: the count of panelists that answer, and
// the problems when the panel is whole.
func TestTheFooterStartsWithWhatTheDoctorWouldSay(t *testing.T) {
	cases := []struct {
		name string
		h    *engine.Health
		want string
	}{
		{"before the first answer", nil, "2 waiting · next look in 1m30s"},
		{"all clear", &engine.Health{Seated: 4, Answering: 4}, "● panel 4/4 · 2 waiting"},
		{"one down", &engine.Health{Seated: 4, Answering: 3, Problems: []string{"codex does not answer"}}, "● panel 3/4 · 2 waiting"},
		{"a whole panel with a problem", &engine.Health{Seated: 4, Answering: 4, Problems: []string{"1 PR got no verdict"}}, "● panel 4/4 · 1 problem · 2 waiting"},
		{"no panel", &engine.Health{Blocking: true, Problems: []string{"no panelist"}}, "● no panel · 2 waiting"},
	}
	for _, c := range cases {
		if footer := footerOf(withHealth(t, c.h)); !strings.HasPrefix(footer, c.want) {
			t.Errorf("%s: the footer is %q, want it to start %q", c.name, footer, c.want)
		}
	}
}

// green all clear, yellow a problem that still lets a review approve, red
// a problem that stops reviews: the color is the verdict at a glance.
func TestTheHealthMarkIsGreenYellowOrRed(t *testing.T) {
	cases := []struct {
		name string
		h    engine.Health
		sgr  string
	}{
		{"all clear", engine.Health{Seated: 4, Answering: 4}, "32"},
		{"one down", engine.Health{Seated: 4, Answering: 3, Problems: []string{"codex does not answer"}}, "33"},
		{"too few to approve", engine.Health{Seated: 4, Answering: 2, Problems: []string{"a", "b"}, Blocking: true}, "31"},
	}
	before := regexp.MustCompile(`\x1b\[([0-9;]*)m●`)
	for _, c := range cases {
		h := c.h
		lines := strings.Split(withHealth(t, &h).View().Content, "\n")
		m := before.FindStringSubmatch(lines[len(lines)-1])
		if m == nil || !strings.Contains(";"+m[1]+";", ";"+c.sgr+";") {
			t.Errorf("%s: the mark is drawn with %v, want SGR %s", c.name, m, c.sgr)
		}
	}
}

func TestTheHealthMarkStaysWhileTheFooterFlashes(t *testing.T) {
	m := withHealth(t, &engine.Health{Seated: 4, Answering: 4})
	m = press(t, m, "m")
	if footer := footerOf(m); !strings.HasPrefix(footer, "● panel 4/4 · the mouse is the terminal's") {
		t.Errorf("the footer is %q", footer)
	}
}
