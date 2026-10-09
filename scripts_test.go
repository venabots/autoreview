package autoreview

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A review is one POST, and GitHub rejects all of it when one comment sits
// on a line outside the diff. pr-diff-lines.sh tells the reviewer which
// lines are inside, so a wrong answer costs a whole review. These are v1's
// cases (tests/skills.test.sh), run against a fake gh.

const fakeDiffGh = `#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_GH_LOG"
case "$*" in
  "pr view 9 --json number,url,headRefOid,baseRefOid")
    printf '%s\n' '{"number":9,"url":"https://github.com/acme/widgets/pull/9","headRefOid":"aaaa1111aaaa1111","baseRefOid":"bbbb2222bbbb2222"}'
    ;;
  "api repos/acme/widgets/pulls/9/files --paginate")
    cat <<'JSON'
[{"filename":"src/pay.rs","patch":"@@ -10,3 +10,5 @@ fn charge() {\n context\n+added\n+added\n context\n context\n@@ -40 +42,0 @@ fn refund() {\n-removed\n@@ -60,2 +61 @@\n-old\n-old\n+new"},
 {"filename":"assets/logo.png"}]
[{"filename":"docs/a b.md","patch":"@@ -0,0 +1,2 @@\n+one\n+@@ not a hunk header"}]
JSON
    ;;
  "pr view 12 --json number,url,headRefOid,baseRefOid")
    printf '%s\n' '{"number":12,"url":"https://ghe.example.com/acme/widgets/pull/12","headRefOid":"dddd4444","baseRefOid":"eeee5555"}'
    ;;
  "api repos/acme/widgets/pulls/12/files --paginate")
    printf '%s\n' '[{"filename":"src/ghe.rs","patch":"@@ -1 +1,2 @@\n a\n+b"}]'
    ;;
  "pr view 13 --json number,url,headRefOid,baseRefOid")
    printf '%s\n' '{"number":13,"url":"not-a-pr-url","headRefOid":"ffff6666","baseRefOid":"aaaa7777"}'
    ;;
  "api repos/acme/widgets/compare/bbbb2222bbbb2222...cccc3333")
    printf '%s\n' '{"files":[{"filename":"src/old.rs","patch":"@@ -1,2 +1,3 @@\n a\n+b\n c"}]}'
    ;;
  *)
    echo "fake gh: unexpected call: $*" >&2
    exit 1
    ;;
esac
`

// diffLines runs the script and returns what it printed, its exit code and
// the gh calls it made.
func diffLines(t *testing.T, args ...string) (out string, code int, calls string) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("pr-diff-lines.sh needs jq")
	}
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "gh"), []byte(fakeDiffGh), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(work, "gh.log")
	script := filepath.Join("skills", "auto-post-panel-review-comments", "scripts", "pr-diff-lines.sh")
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(), "FAKE_GH_LOG="+log, "PATH="+work+string(os.PathListSeparator)+os.Getenv("PATH"))
	raw, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	logged, _ := os.ReadFile(log)
	return string(raw), code, string(logged)
}

func TestDiffLinesNamesTheNewSideOfEachHunk(t *testing.T) {
	out, code, calls := diffLines(t, "9")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	for _, want := range []string{"src/pay.rs\t10\t14", "src/pay.rs\t61\t61", "docs/a b.md\t1\t2"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"src/pay.rs\t42", "logo.png"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a hunk that only removes, or a file with no patch, has a line: %q in\n%s", unwanted, out)
		}
	}
	if n := len(strings.Split(strings.TrimSpace(out), "\n")); n != 3 {
		t.Errorf("%d lines, want 3: an added line that looks like a hunk header is not one:\n%s", n, out)
	}
	if !strings.Contains(calls, "pulls/9/files") {
		t.Errorf("the head diff did not come from the PR's files: %s", calls)
	}
}

func TestDiffLinesReadsTheHeadByItsShortSha(t *testing.T) {
	out, _, calls := diffLines(t, "9", "aaaa1111")
	if !strings.Contains(out, "src/pay.rs\t10\t14") || strings.Contains(calls, "compare") {
		t.Errorf("a short head sha: %q, calls %q", out, calls)
	}
}

func TestDiffLinesDiffsAnOlderCommitAgainstTheBase(t *testing.T) {
	out, code, calls := diffLines(t, "9", "cccc3333")
	if code != 0 || out != "src/old.rs\t1\t3\n" || strings.Contains(calls, "pulls/9/files") {
		t.Errorf("an older commit: exit %d, %q, calls %q", code, out, calls)
	}
}

func TestDiffLinesReadsAnEnterpriseHostAndRefusesAnotherShape(t *testing.T) {
	if out, _, _ := diffLines(t, "12"); out != "src/ghe.rs\t1\t2\n" {
		t.Errorf("a GitHub Enterprise PR: %q", out)
	}
	out, code, calls := diffLines(t, "13")
	if code != 1 || !strings.Contains(out, "could not read owner/repo from PR url 'not-a-pr-url'") || strings.Contains(calls, "api ") {
		t.Errorf("a url of another shape: exit %d, %q, calls %q", code, out, calls)
	}
}

func TestDiffLinesRefusesBadInput(t *testing.T) {
	if out, code, _ := diffLines(t); code != 1 || !strings.Contains(out, "usage:") {
		t.Errorf("no PR: exit %d, %q", code, out)
	}
	if out, code, _ := diffLines(t, "404"); code != 1 || !strings.Contains(out, "could not resolve PR '404'") {
		t.Errorf("a PR gh cannot find: exit %d, %q", code, out)
	}
}
