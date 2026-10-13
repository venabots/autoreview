package ledger

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// private is the ledger's mode. The file names repositories, files and line
// numbers, so only its owner reads it.
const private fs.FileMode = 0o600

// Append adds one record as one line. It makes the parent directory on first
// use.
//
// Two autoreview processes can share one ledger under cron, and a line with
// many findings is longer than one atomic append. An exclusive lock makes
// the writers take turns, so no two lines interleave. The lock is released
// when the file closes.
func Append(path string, r Record) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, private)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()

	// The mode above applies only when the file is made. Set it again, so a
	// ledger that became readable to others is made private on the next
	// write. A failure here must not lose the record.
	_ = file.Chmod(private)

	if err := lockExclusive(file); err != nil {
		return err
	}
	line, err := r.MarshalJSON()
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return err
}

// Read is every record in the file, in the order written, one for each id.
// A line that does not parse is skipped: a torn write must not take the rest
// of the history with it. A record from a newer format is skipped too. A
// file that is not there yet is no history, and not an error.
func Read(path string) ([]Record, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []Record
	seen := map[string]bool{}
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		record, ok := parseLine(line)
		// The format is checked before the id, as v1 does: a record this
		// build cannot read must not hide an older one with the same id.
		if !ok || record.V > Version || seen[record.ID] {
			continue
		}
		seen[record.ID] = true
		records = append(records, record)
	}
	return records, nil
}

// parseLine reads one line. Text that is not UTF-8 is refused before
// encoding/json can replace the bad bytes and call the result a record.
func parseLine(line []byte) (Record, bool) {
	if !utf8.Valid(line) {
		return Record{}, false
	}
	var record Record
	if err := json.Unmarshal(line, &record); err != nil {
		return Record{}, false
	}
	return record, true
}
