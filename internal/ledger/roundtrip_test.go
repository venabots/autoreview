package ledger

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// TestEveryLineOfARealLedgerRoundTrips is skipped unless
// $AUTOREVIEW_LEDGER_ROUNDTRIP names a ledger file. It reads every line and
// writes it back, and fails on the first line whose bytes change.
//
// Point it at a copy. It only reads, but the real ledger is still being
// appended to. A failure names the line and the byte, and prints neither:
// the file holds repo names, PR numbers and session ids.
func TestEveryLineOfARealLedgerRoundTrips(t *testing.T) {
	path := os.Getenv("AUTOREVIEW_LEDGER_ROUNDTRIP")
	if path == "" {
		t.Skip("set AUTOREVIEW_LEDGER_ROUNDTRIP to a copy of a ledger to run this")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
	for i, line := range lines {
		var record Record
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("line %d of %d did not parse", i+1, len(lines))
		}
		got, err := record.MarshalJSON()
		if err != nil {
			t.Fatalf("line %d of %d did not marshal", i+1, len(lines))
		}
		if !bytes.Equal(got, line) {
			t.Fatalf("line %d of %d changed at byte %d (%d bytes in, %d out)",
				i+1, len(lines), firstDifference(got, line), len(line), len(got))
		}
	}
	records, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d lines, %d matched byte for byte, Read gave %d records", len(lines), len(lines), len(records))
}

func firstDifference(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
