package worktree

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestFinishRunsV1sGitStepsAndKeepsWhatHoldsWork(t *testing.T) {
	status := call{wt, "status --porcelain --untracked-files=no"}
	ahead := call{wt, "rev-list --count @{upstream}..HEAD"}
	remove := call{root, "worktree remove --force " + wt}
	deleteBranch := call{root, "branch --quiet -d me/work"}
	clean := map[string]answer{"rev-list --count @{upstream}..HEAD": {out: "0\n"}}
	cases := []struct {
		name    string
		answers map[string]answer
		created bool
		kept    Kept
		isKept  bool
		err     string
		want    []call
	}{
		{"a clean worktree goes with the branch it made", clean, true, Kept{}, false, "", []call{status, ahead, remove, deleteBranch}},
		{"a clean worktree on your branch leaves the branch", clean, false, Kept{}, false, "", []call{status, ahead, remove}},
		{"uncommitted changes keep it", map[string]answer{"status --porcelain --untracked-files=no": {out: "A  half.txt\n"}}, true,
			Kept{wt, "uncommitted changes"}, true, "", []call{status}},
		{"unpushed commits keep it", map[string]answer{"rev-list --count @{upstream}..HEAD": {out: "2\n"}}, true,
			Kept{wt, "commits that are not pushed"}, true, "", []call{status, ahead}},
		{"a status git cannot read keeps it", map[string]answer{"status --porcelain --untracked-files=no": {err: errNo}}, true,
			Kept{wt, "git could not read it: no"}, true, "git could not read it: no", []call{status}},
		{"a comparison git cannot make keeps it", map[string]answer{"rev-list --count @{upstream}..HEAD": {err: errNo}}, true,
			Kept{wt, "git could not compare it with origin: no"}, true, "git could not compare it with origin: no", []call{status, ahead}},
		{"a removal git refuses keeps it and the branch", map[string]answer{
			"rev-list --count @{upstream}..HEAD": {out: "0"},
			"worktree remove --force " + wt:      {err: errNo},
		}, true, Kept{wt, "git could not remove it: no"}, true, "git could not remove it: no", []call{status, ahead, remove}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{answers: c.answers}
			p := Prepared{Path: wt, Branch: "me/work", Head: "abc", CreatedBranch: c.created}
			kept, isKept, err := Finish(context.Background(), r.git, root, p)
			if kept != c.kept || isKept != c.isKept {
				t.Errorf("Finish = %+v, %v; want %+v, %v", kept, isKept, c.kept, c.isKept)
			}
			if got := errText(err); got != c.err {
				t.Errorf("error = %q, want %q", got, c.err)
			}
			if c.err != "" && !errors.Is(err, errNo) {
				t.Errorf("error %v does not wrap git's", err)
			}
			if got := r.seen(); !slices.Equal(got, c.want) {
				t.Errorf("git calls:\n got %v\nwant %v", got, c.want)
			}
		})
	}
}

func TestFinishDoesNotChangeThePreparedItIsHanded(t *testing.T) {
	r := &recorder{answers: map[string]answer{"rev-list --count @{upstream}..HEAD": {out: "0"}}}
	p := Prepared{Path: wt, Branch: "me/work", Head: "abc", CreatedBranch: true}
	before := p
	if _, _, err := Finish(context.Background(), r.git, root, p); err != nil {
		t.Fatal(err)
	}
	if p != before {
		t.Errorf("Prepared changed: %+v, was %+v", p, before)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
