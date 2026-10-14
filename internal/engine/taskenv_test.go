package engine

import (
	"slices"
	"testing"
	"testing/synctest"
)

// A task on a branch of the run's own runs a plain `git push`, as every
// skill does. Without the push setting in its environment, git refuses
// that push, and the task ends with nothing on the PR.
func TestATaskOnABranchOfItsOwnGetsThePushSettingInItsEnvironment(t *testing.T) {
	cases := []struct {
		name string
		own  bool
		want []string
	}{
		{"the PR's own branch", false, nil},
		{"a branch of the run's own", true, []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=push.default", "GIT_CONFIG_VALUE_0=upstream"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := &trees{own: c.own}
				b := newBench(t, held(setup{deps: f.deps}))
				b.eng.Do(fixOf(4))
				synctest.Wait()

				if env := b.reviewer.command(t, 4).Env; !slices.Equal(env, c.want) {
					t.Errorf("Env = %v, want %v", env, c.want)
				}
				b.reviewer.finish(t, 4, 0, "done")
				synctest.Wait()
				b.eng.Do(Quit{})
				<-b.results
			})
		})
	}
}
