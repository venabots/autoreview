package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/cli"
)

// The formula is Ruby that only Homebrew runs, after a tag. These tests
// read it here, so a rename that would break the build in the tap fails a
// pull request instead.

func formula(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(packageDir, "..", "..", "packaging", "autoreview.rb"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestTheFormulaBuildsThisPackageAndStampsItsVersion(t *testing.T) {
	f := formula(t)
	build := regexp.MustCompile(`system "go", "build", \*std_go_args\(ldflags: "([^"]*)"\), "(\./[^"]+)"`).FindStringSubmatch(f)
	if build == nil {
		t.Fatalf("the formula has no go build line:\n%s", f)
	}
	if build[2] != "./cmd/autoreview" {
		t.Errorf("the formula builds %s", build[2])
	}
	ldflags := strings.ReplaceAll(build[1], "#{version}", "1.2.3")
	bin := filepath.Join(t.TempDir(), "autoreview")
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, ".")
	cmd.Dir, cmd.Env = packageDir, startEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build with the formula's flags: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil || string(out) != "autoreview 1.2.3\n" {
		t.Errorf("--version said %q (%v): the ldflag no longer reaches main.version", out, err)
	}
}

func TestTheFormulaTestMatchesTheHelp(t *testing.T) {
	m := regexp.MustCompile(`assert_match "([^"#]+)", shell_output\("#\{bin\}/autoreview --help"\)`).FindStringSubmatch(formula(t))
	if m == nil {
		t.Fatal("the formula does not test --help")
	}
	if !strings.Contains(cli.Help, m[1]) {
		t.Errorf("the formula looks for %q, which the help does not say", m[1])
	}
}
