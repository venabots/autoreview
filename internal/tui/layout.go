package tui

// rect is a region of the screen, in cells.
type rect struct {
	x, y, w, h int
}

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// layout is where each part of the screen goes. The list and the detail
// are side by side on a wide terminal and stacked on a narrow one.
type layout struct {
	width  int
	height int
	header rect
	tabs   rect
	// body is the list, the detail and the divider between them.
	body   rect
	list   rect
	detail rect
	footer rect
	// wide is true when the list is beside the detail.
	wide bool
}

const (
	// wideFrom is the width at which the two panes fit side by side.
	wideFrom = 90
	// The list takes three tenths of a wide screen, within these bounds: a
	// branch name needs about thirty columns, and past forty-four the
	// detail pane is the better use of the space.
	listMin = 30
	listMax = 44
	// divider is the column, or the row, between the two panes. On a wide
	// screen one more column of padding follows it.
	divider = 1
	padding = 1
)

// layoutFor divides a screen of w by h cells. Every region has a size that
// is zero or more, so a tiny terminal draws less but never panics.
func layoutFor(w, h int) layout {
	l := layout{width: w, height: h, wide: w >= wideFrom}
	l.header = rect{0, 0, w, min(h, 1)}
	l.footer = rect{0, max(h-1, 0), w, btoi(h >= 2)}
	l.tabs = rect{0, 1, w, btoi(h >= 3)}
	top := l.tabs.y + l.tabs.h
	body := rect{0, top, w, max(l.footer.y-top, 0)}
	l.body = body
	if l.wide {
		listW := min(max(w*3/10, listMin), listMax)
		l.list = rect{0, body.y, listW, body.h}
		left := listW + divider + padding
		l.detail = rect{left, body.y, max(w-left, 0), body.h}
		return l
	}
	listH := min(max(body.h*2/5, 3), body.h)
	l.list = rect{0, body.y, w, listH}
	below := body.y + listH + divider
	l.detail = rect{0, below, w, max(body.y+body.h-below, 0)}
	return l
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
