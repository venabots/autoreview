package clipboard

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

type call struct {
	text, name string
	args       []string
}

func found(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if slices.Contains(names, name) {
			return "/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestCopyUsesTheFirstProgramThatCanReachADisplay(t *testing.T) {
	cases := []struct {
		name  string
		there []string
		vars  map[string]string
		want  []call
	}{
		{"pbcopy on a Mac", []string{"pbcopy", "xclip"}, map[string]string{"DISPLAY": ":0"},
			[]call{{"copied text", "/bin/pbcopy", nil}}},
		{"wl-copy under Wayland", []string{"wl-copy", "xclip"}, map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"},
			[]call{{"copied text", "/bin/wl-copy", nil}}},
		{"xclip under X", []string{"wl-copy", "xclip"}, map[string]string{"DISPLAY": ":0"},
			[]call{{"copied text", "/bin/xclip", []string{"-selection", "clipboard"}}}},
		{"nothing over SSH with no display", []string{"wl-copy", "xclip", "xsel"}, nil, nil},
		{"nothing where no program is there", nil, map[string]string{"DISPLAY": ":0"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls []call
			run := func(_ context.Context, text, name string, args ...string) error {
				calls = append(calls, call{text, name, args})
				return nil
			}
			if err := Copy(context.Background(), found(c.there...), env(c.vars), run, "copied text"); err != nil {
				t.Fatalf("Copy = %v", err)
			}
			if len(calls) != len(c.want) {
				t.Fatalf("calls = %+v, want %+v", calls, c.want)
			}
			for i := range calls {
				if calls[i].text != c.want[i].text || calls[i].name != c.want[i].name || !slices.Equal(calls[i].args, c.want[i].args) {
					t.Errorf("call %d = %+v, want %+v", i, calls[i], c.want[i])
				}
			}
		})
	}
}

func TestCopyReturnsTheProgramsFailure(t *testing.T) {
	refused := errors.New("no access")
	run := func(context.Context, string, string, ...string) error { return refused }
	err := Copy(context.Background(), found("pbcopy"), env(nil), run, "x")
	if !errors.Is(err, refused) {
		t.Errorf("Copy = %v, want %v", err, refused)
	}
}

func TestExecHandsTheTextToTheProgramAndSaysWhyItFailed(t *testing.T) {
	if err := Exec(context.Background(), "hello", "sh", "-c", `test "$(cat)" = hello`); err != nil {
		t.Errorf("the program did not get the text on stdin: %v", err)
	}
	err := Exec(context.Background(), "", "sh", "-c", "echo 'no display' >&2; exit 1")
	if err == nil || !strings.Contains(err.Error(), "no display") {
		t.Errorf("Exec = %v, want the program's own words", err)
	}
}
