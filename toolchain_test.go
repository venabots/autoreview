package autoreview

import (
	"os"
	"regexp"
	"testing"
)

// Dependabot updates the workflow and not mise.toml. A linter that is newer
// in one place than in the other passes on this machine and fails in CI, or
// the other way round.
func TestMiseAndCIRunTheSameLinter(t *testing.T) {
	local := versionIn(t, "mise.toml", `(?m)^golangci-lint\s*=\s*"([^"]+)"`)
	ci := versionIn(t, ".github/workflows/test.yml", `(?m)^\s*version:\s*v(\S+)`)
	if local != ci {
		t.Errorf("mise.toml pins golangci-lint %s and the Test workflow runs %s", local, ci)
	}
}

func versionIn(t *testing.T, path, pattern string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(pattern).FindSubmatch(raw)
	if found == nil {
		t.Fatalf("%s names no version that matches %s", path, pattern)
	}
	return string(found[1])
}
