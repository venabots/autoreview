package repo

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRootFrom(t *testing.T) {
	cases := []struct {
		name    string
		in      []byte
		want    string
		wantErr string
	}{
		{"the newline git adds is removed", []byte("/tmp/repo\n"), "/tmp/repo", ""},
		{"only one newline is removed", []byte("/tmp/repo\n\n"), "/tmp/repo\n", ""},
		{"a trailing space is part of the name", []byte("/tmp/repo \n"), "/tmp/repo ", ""},
		{"no newline at all is accepted", []byte("/tmp/repo"), "/tmp/repo", ""},
		{"silence is refused", []byte(""), "", "printed nothing"},
		{"a lone newline is refused", []byte("\n"), "", "printed nothing"},
		{"bytes that are not UTF-8 are refused", []byte{'/', 0xff, '\n'}, "", "not valid UTF-8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := RootFrom(c.in)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("RootFrom(%q) error = %v, want one containing %q", c.in, err, c.wantErr)
				}
				return
			}
			if err != nil || got != c.want {
				t.Errorf("RootFrom(%q) = %q, %v; want %q", c.in, got, err, c.want)
			}
		})
	}
}

type answers map[string]string

func (a answers) gh(_ context.Context, args ...string) ([]byte, error) {
	return a.answer("gh " + strings.Join(args, " "))
}

func (a answers) git(_ context.Context, args ...string) ([]byte, error) {
	return a.answer("git " + strings.Join(args, " "))
}

func (a answers) answer(cmd string) ([]byte, error) {
	out, ok := a[cmd]
	if !ok {
		return nil, errors.New("exit status 1")
	}
	return []byte(out), nil
}

func healthy() answers {
	return answers{
		"gh repo view --json owner,name": `{"owner":{"login":"acme"},"name":"widgets"}`,
		"git rev-parse --show-toplevel":  "/sandbox/repo\n",
		"gh api user --jq .login":        "me\n",
	}
}

func without(a answers, cmd string) answers {
	out := answers{}
	for k, v := range a {
		if k != cmd {
			out[k] = v
		}
	}
	return out
}

func with(a answers, cmd, reply string) answers {
	out := without(a, cmd)
	out[cmd] = reply
	return out
}

func TestLoad(t *testing.T) {
	a := healthy()
	got, err := Load(context.Background(), a.gh, a.git)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Context{Owner: "acme", Name: "widgets", Root: "/sandbox/repo", Me: "me"}
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
	if got.Slug() != "acme/widgets" {
		t.Errorf("Slug = %q", got.Slug())
	}
}

func TestLoadSaysWhatIsWrong(t *testing.T) {
	cases := []struct {
		name    string
		a       answers
		wantErr string
	}{
		{"gh cannot see a repo", without(healthy(), "gh repo view --json owner,name"), "not a GitHub repo"},
		{"gh names no owner", with(healthy(), "gh repo view --json owner,name", `{"name":"widgets"}`), "no owner or name"},
		{"git is not in a checkout", without(healthy(), "git rev-parse --show-toplevel"), "not inside a git checkout"},
		{"git prints nothing", with(healthy(), "git rev-parse --show-toplevel", ""), "printed nothing"},
		{"gh cannot read the login", without(healthy(), "gh api user --jq .login"), "could not read your GitHub login"},
		{"gh prints an empty login", with(healthy(), "gh api user --jq .login", "\n"), "empty login"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(context.Background(), c.a.gh, c.a.git)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("Load error = %v, want one containing %q", err, c.wantErr)
			}
		})
	}
}
