package config

import (
	"os"
	"strings"
	"testing"
)

// The README's table is where a person finds what the file can set. A key
// added without a row there is a setting nobody learns of: a file written
// before it never gains its line.
func TestTheReadmeListsEverySetting(t *testing.T) {
	raw, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range KeyNames() {
		if !strings.Contains(string(raw), "| `"+key+"` ") {
			t.Errorf("the README's settings table has no row for %s", key)
		}
	}
}
