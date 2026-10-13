package term

import "testing"

func TestAHerdrPaneIDIsReadOutOfTheJSON(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
		ok   bool
	}{
		{"a string id", `{"result":{"root_pane":{"pane_id":"pane-7"}}}`, "pane-7", true},
		{"a number is still an id", `{"result":{"root_pane":{"pane_id":7}}}`, "7", true},
		{"a newline after the JSON", `{"result":{"root_pane":{"pane_id":"pane-7"}}}` + "\n", "pane-7", true},
		{"other fields are passed over", `{"id":1,"result":{"tab":"t","root_pane":{"pane_id":"p","x":1}}}`, "p", true},
		{"no pane in the result", `{"result":{}}`, "", false},
		{"an empty id", `{"result":{"root_pane":{"pane_id":""}}}`, "", false},
		{"a null id", `{"result":{"root_pane":{"pane_id":null}}}`, "", false},
		{"an id that is neither a string nor a number", `{"result":{"root_pane":{"pane_id":true}}}`, "", false},
		{"a result that is not an object", `{"result":[]}`, "", false},
		{"words that are not JSON", "herdr: something went wrong", "", false},
		{"nothing", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parsePaneID(c.out)
			if got != c.want || ok != c.ok {
				t.Errorf("parsePaneID(%q) = %q, %v; want %q, %v", c.out, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestACmuxSurfaceIsReadOutOfTheLine(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
		ok   bool
	}{
		{"the token alone", "surface:1\n", "surface:1", true},
		{"the token inside a line", "created surface:42 in workspace 3", "surface:42", true},
		{"the token at the end with no newline", "OK surface:7", "surface:7", true},
		{"no token", "no surface here", "", false},
		{"a token with no digits", "surface:", "", false},
		{"only the first token is read", "surface:x surface:2", "", false},
		{"nothing", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseSurface(c.out)
			if got != c.want || ok != c.ok {
				t.Errorf("parseSurface(%q) = %q, %v; want %q, %v", c.out, got, ok, c.want, c.ok)
			}
		})
	}
}
