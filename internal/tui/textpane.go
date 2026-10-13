package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	glamourstyles "charm.land/glamour/v2/styles"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/sanitize"
)

// reviewText is the text of the selected review, read once and drawn once
// for each width. The view is redrawn after every message, and a review is
// long: reading and rendering it for every frame would make each key slow.
type reviewText struct {
	path string
	// found is false while the file is missing. It is looked for again
	// then, because a review's file appears a moment after its result.
	found    bool
	raw      string
	width    int
	dark     bool
	rendered string
}

// reviewPath is where a finished review's text is.
func reviewPath(job engine.Job) string {
	return filepath.Join(job.PassDir, fmt.Sprintf("pr-%d.review.md", job.PR))
}

// loaded is the text for the review at path, drawn for a pane of the given
// width. It reads the file only when the path is new or the file was
// missing, and renders only when the text, the width or the theme changed.
func (t reviewText) loaded(path string, width int, dark bool) reviewText {
	if path != t.path || !t.found {
		raw, err := os.ReadFile(path)
		t = reviewText{path: path, found: err == nil, raw: sanitize.Block(string(raw))}
	}
	if !t.found || (t.rendered != "" && t.width == width && t.dark == dark) {
		return t
	}
	t.width, t.dark = width, dark
	t.rendered = markdown(t.raw, width, dark)
	return t
}

// markdown draws a review. A text Glamour cannot draw is shown as written:
// a review a person cannot read at all is worse than one without styling.
func markdown(text string, width int, dark bool) string {
	renderer, err := glamour.NewTermRenderer(glamour.WithStyles(reviewStyle(dark)), glamour.WithWordWrap(max(width, 20)))
	if err != nil {
		return text
	}
	out, err := renderer.Render(text)
	if err != nil {
		return text
	}
	// Filtered again after the render: Glamour decodes an entity such as
	// `&#x1b;` into the character itself, so clean text can come out with
	// an escape or a bidi override in it.
	return strings.Trim(sanitize.Styled(out), "\n")
}

// reviewStyle is Glamour's own style without its page margin. The pane
// already has its edge, and two columns matter in a narrow one.
func reviewStyle(dark bool) ansi.StyleConfig {
	style := glamourstyles.LightStyleConfig
	if dark {
		style = glamourstyles.DarkStyleConfig
	}
	none := uint(0)
	style.Document.Margin = &none
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	return style
}

// logTail is the end of the run's own log, re-read only when the file has
// grown.
type logTail struct {
	path string
	size int64
	text string
}

// logKeep is how much of the log's end the pane holds.
const logKeep = 64 << 10

// loaded is the tail of the log at path as it is now.
func (l logTail) loaded(path string) logTail {
	info, err := os.Stat(path)
	if err != nil {
		return logTail{path: path, text: "the run log is not there yet"}
	}
	if path == l.path && info.Size() == l.size {
		return l
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return logTail{path: path, text: "the run log could not be read"}
	}
	if len(raw) > logKeep {
		raw = raw[len(raw)-logKeep:]
		// The cut can fall inside a line. A line with no start is noise.
		if _, rest, ok := strings.Cut(string(raw), "\n"); ok {
			raw = []byte(rest)
		}
	}
	text := strings.ReplaceAll(sanitize.Block(string(raw)), "\t", "    ")
	return logTail{path: path, size: info.Size(), text: text}
}
