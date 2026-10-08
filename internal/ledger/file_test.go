package ledger

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func record(id string) Record {
	return Record{V: Version, ID: id, At: 1, Source: "test", Reviewed: true}
}

func ids(records []Record) string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.ID
	}
	return strings.Join(out, " ")
}

func writeLedger(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAFileThatIsNotThereIsNoHistoryAndNoError(t *testing.T) {
	records, err := Read(filepath.Join(t.TempDir(), "nested", "ledger.jsonl"))
	if err != nil || len(records) != 0 {
		t.Errorf("Read() = %v, %v, want no records and no error", records, err)
	}
}

func TestAFileThatCannotBeReadIsAnError(t *testing.T) {
	if _, err := Read(t.TempDir()); err == nil {
		t.Error("Read() of a directory gave no error")
	}
}

func TestAppendedRecordsReadBackOnceEach(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "ledger.jsonl")
	for _, id := range []string{"a", "b", "a"} {
		if err := Append(path, record(id)); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(raw, []byte("\n")); got != 3 || !bytes.HasSuffix(raw, []byte("\n")) {
		t.Errorf("the file has %d line ends, want 3 with one at the end:\n%s", got, raw)
	}
	records, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(records); got != "a b" {
		t.Errorf("Read() ids = %q, want %q", got, "a b")
	}
}

func TestReadKeepsWhatV1Keeps(t *testing.T) {
	const tail = `,"at":1,"source":"test","panel":[],"findings":[],"reviewed":true}`
	line := func(v int, id string) string {
		return fmt.Sprintf(`{"v":%d,"id":%q`, v, id) + tail
	}
	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{"the order of the file", []string{line(1, "b"), line(1, "a"), line(1, "c")}, "b a c"},
		{"the first record for an id", []string{line(1, "a"), line(1, "b"), line(1, "a")}, "a b"},
		{"a torn line is skipped", []string{line(1, "a"), `{"v":1,"id":"c"`, line(1, "b")}, "a b"},
		{"a blank line is skipped", []string{line(1, "a"), "", line(1, "b")}, "a b"},
		{"a record from a newer format is skipped", []string{line(1, "a"), line(2, "n"), line(1, "b")}, "a b"},
		{"an older format is read", []string{line(0, "old"), line(1, "a")}, "old a"},
		{
			"a newer record does not hide an older one with its id",
			[]string{line(2, "a"), line(1, "a")},
			"a",
		},
		{"a record with a null list is skipped", []string{`{"v":1,"id":"n","at":1,"source":"test","panel":null}`, line(1, "a")}, "a"},
		{"a line that is not UTF-8 is skipped", []string{"{\"v\":1,\"id\":\"\xff\"" + tail, line(1, "a")}, "a"},
		{"a line that ends in a carriage return", []string{line(1, "a") + "\r", line(1, "b") + "\r"}, "a b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			records, err := Read(writeLedger(t, c.lines...))
			if err != nil {
				t.Fatal(err)
			}
			if got := ids(records); got != c.want {
				t.Errorf("Read() ids = %q, want %q", got, c.want)
			}
		})
	}
}

func TestALastLineWithNoNewlineIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	raw := `{"v":1,"id":"a","at":1,"source":"test"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := Read(path)
	if err != nil || ids(records) != "a" {
		t.Errorf("Read() = %q, %v, want a", ids(records), err)
	}
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestTheFileIsPrivateAndIsMadePrivateAgainOnEveryAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := Append(path, record("a")); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, path); got != 0o600 {
		t.Errorf("a new ledger has mode %o, want 600", got)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, record("b")); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, path); got != 0o600 {
		t.Errorf("a ledger that became readable has mode %o after an append, want 600", got)
	}
}

func TestAppendAddsToWhatV1Wrote(t *testing.T) {
	lines := fixtureLines(t)
	before := append(bytes.Join(lines, []byte("\n")), '\n')
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, record("new")); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"id":"new","at":1,"source":"test","panel":[],"findings":[],"reviewed":true}` + "\n"
	if !bytes.HasPrefix(after, before) || string(after[len(before):]) != want {
		t.Errorf("the append changed what was there, or added the wrong line:\n%s", after[len(before):])
	}
}

func TestWritersAtTheSameTimeDoNotInterleaveTheirLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	// A line much longer than one atomic append, so a missing lock shows.
	long := strings.Repeat("x", 64*1024)
	const writers, each = 8, 5
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := range writers {
		wg.Go(func() {
			for i := range each {
				r := record(fmt.Sprintf("w%d-%d", w, i))
				r.Repo = &long
				errs <- Append(path, r)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	records, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != writers*each {
		t.Errorf("Read() gave %d records, want %d: a line was torn", len(records), writers*each)
	}
}

// TestV1ReadsWhatAppendWrites is skipped unless $AUTOREVIEW_V1 names the v1
// binary. It appends records in every shape and asks v1 how many it reads.
func TestV1ReadsWhatAppendWrites(t *testing.T) {
	v1 := os.Getenv("AUTOREVIEW_V1")
	if v1 == "" {
		t.Skip("set AUTOREVIEW_V1 to the v1 binary to run this")
	}
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	lines := fixtureLines(t)
	for _, line := range lines {
		var r Record
		if err := r.UnmarshalJSON(line); err != nil {
			t.Fatal(err)
		}
		if err := Append(path, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := Append(path, Record{V: Version, ID: "bare", Source: "panel"}); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(v1, "stats", "--ledger", path, "--json").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`"runs": %d,`, len(lines)+1)
	if !bytes.Contains(out, []byte(want)) {
		t.Errorf("v1 did not read every record, want %s in:\n%s", want, out)
	}
}
