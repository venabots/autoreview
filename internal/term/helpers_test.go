package term

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
)

// call is one program a test saw Open run.
type call struct {
	name string
	args []string
}

// reply is what a fake program prints, or the error it ends with.
type reply struct {
	out string
	err error
}

// recorder is a Runner that keeps every call. It answers by the program
// and its first argument, such as "cmux send", and a call with no answer
// succeeds and prints nothing.
type recorder struct {
	calls   []call
	replies map[string]reply
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, call{name, slices.Clone(args)})
	key := name
	if len(args) > 0 {
		key += " " + args[0]
	}
	answer := r.replies[key]
	return []byte(answer.out), answer.err
}

// steps is the program and first argument of each call, in order.
func (r *recorder) steps() []string {
	steps := make([]string, len(r.calls))
	for i, c := range r.calls {
		steps[i] = c.name + " " + c.args[0]
	}
	return steps
}

// answering is a recorder in which every step of every terminal works.
func answering() *recorder {
	return &recorder{replies: map[string]reply{
		"herdr tab":        {out: `{"result":{"root_pane":{"pane_id":"pane-7"}}}` + "\n"},
		"cmux new-surface": {out: "surface:1\n"},
	}}
}

// failing is answering with one step that ends with errBoom.
func failing(step string) *recorder {
	r := answering()
	r.replies[step] = reply{err: errBoom}
	return r
}

var errBoom = errors.New("boom")

// envOf is an Env with these variables set and these programs on PATH.
func envOf(goos string, programs []string, vars map[string]string) Env {
	return Env{
		Get: func(key string) string { return vars[key] },
		LookPath: func(program string) (string, error) {
			if slices.Contains(programs, program) {
				return "/bin/" + program, nil
			}
			return "", errors.New("not found")
		},
		GOOS: goos,
	}
}

func inHerdr() Env {
	return envOf("darwin", []string{"herdr"}, map[string]string{"HERDR_ENV": "1"})
}

func inCmux() Env {
	return envOf("darwin", []string{"cmux"}, map[string]string{"CMUX_SURFACE_ID": "surface:9"})
}

func inGhostty() Env {
	return envOf("darwin", nil, map[string]string{"TERM_PROGRAM": "ghostty"})
}

// open calls Open in virtual time, so the wait after a Ghostty tab costs a
// test nothing.
func open(t *testing.T, run Runner, env Env, label, command string) (Kind, error) {
	t.Helper()
	var kind Kind
	var err error
	synctest.Test(t, func(*testing.T) {
		kind, err = Open(context.Background(), run, env, label, command)
	})
	return kind, err
}
