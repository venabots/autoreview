package tui

import "testing"

func TestAWideScreenPutsTheListBesideTheDetail(t *testing.T) {
	l := layoutFor(120, 40)
	if !l.wide {
		t.Fatal("a 120-column screen is not wide")
	}
	if l.list != (rect{0, 2, 36, 37}) {
		t.Errorf("list = %+v, want 36 columns by 37 rows under the tabs", l.list)
	}
	if l.detail != (rect{38, 2, 82, 37}) {
		t.Errorf("detail = %+v, want the rest after the rule and one column of padding", l.detail)
	}
	if l.header.y != 0 || l.tabs.y != 1 || l.footer.y != 39 {
		t.Errorf("header, tabs, footer are on rows %d, %d, %d; want 0, 1, 39", l.header.y, l.tabs.y, l.footer.y)
	}
}

func TestTheListWidthStaysWithinItsBounds(t *testing.T) {
	cases := []struct{ width, want int }{{90, 30}, {100, 30}, {120, 36}, {140, 42}, {200, 44}}
	for _, c := range cases {
		if got := layoutFor(c.width, 40).list.w; got != c.want {
			t.Errorf("at %d columns the list is %d wide, want %d", c.width, got, c.want)
		}
	}
}

func TestANarrowScreenStacksTheListOverTheDetail(t *testing.T) {
	l := layoutFor(80, 24)
	if l.wide {
		t.Fatal("an 80-column screen is wide")
	}
	if l.list != (rect{0, 2, 80, 8}) {
		t.Errorf("list = %+v, want two fifths of the 21 body rows", l.list)
	}
	if l.detail != (rect{0, 11, 80, 12}) {
		t.Errorf("detail = %+v, want the rows under the rule", l.detail)
	}
}

func TestEveryRegionFitsOnTheScreen(t *testing.T) {
	for w := 0; w <= 130; w += 13 {
		for h := 0; h <= 12; h++ {
			l := layoutFor(w, h)
			for name, r := range map[string]rect{"header": l.header, "tabs": l.tabs, "list": l.list, "detail": l.detail, "footer": l.footer} {
				if r.w < 0 || r.h < 0 || r.x < 0 || r.y < 0 {
					t.Errorf("%dx%d: %s = %+v has a negative size or place", w, h, name, r)
				}
				if r.h > 0 && (r.y+r.h > h || r.x+r.w > w) {
					t.Errorf("%dx%d: %s = %+v runs off the screen", w, h, name, r)
				}
			}
			used := l.header.h + l.tabs.h + l.body.h + l.footer.h
			if used != h {
				t.Errorf("%dx%d: the regions use %d rows", w, h, used)
			}
		}
	}
}

func TestRectContains(t *testing.T) {
	r := rect{2, 3, 4, 2}
	inside := [][2]int{{2, 3}, {5, 4}}
	outside := [][2]int{{1, 3}, {6, 3}, {2, 2}, {2, 5}}
	for _, p := range inside {
		if !r.contains(p[0], p[1]) {
			t.Errorf("%+v does not contain %v", r, p)
		}
	}
	for _, p := range outside {
		if r.contains(p[0], p[1]) {
			t.Errorf("%+v contains %v", r, p)
		}
	}
}
