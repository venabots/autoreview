package doctor

import (
	"regexp"
	"strconv"
	"strings"
)

var semver = regexp.MustCompile(`\b(\d+)\.(\d+)\.(\d+)\b`)

// versionIn is the first x.y.z in a program's --version line, or "".
func versionIn(said string) string {
	return semver.FindString(said)
}

// older says version is below min. Either one empty, or not x.y.z, is not
// older: a version the doctor cannot read is not a problem it can name.
func older(version, min string) bool {
	v, okV := parts(version)
	m, okM := parts(min)
	if !okV || !okM {
		return false
	}
	for i := range v {
		if v[i] != m[i] {
			return v[i] < m[i]
		}
	}
	return false
}

func parts(version string) ([3]int, bool) {
	fields := strings.Split(version, ".")
	if len(fields) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}
