// Package serde writes JSON with the bytes v1's serde_json writes.
//
// v1 and v2 append to one ledger, and a script may read `stats --json` from
// either. encoding/json cannot give the same bytes: it writes a whole float
// as `3` where serde_json writes `3.0`, and it always escapes U+2028 and
// U+2029, which serde_json leaves alone. So the strings and the floats are
// written here, by v1's rules.
package serde

import (
	"math"
	"slices"
	"strconv"
	"strings"
)

const hexDigits = "0123456789abcdef"

// AppendString adds s as a JSON string. Only the quote, the backslash and
// the C0 controls are escaped. `<`, `>`, `&`, DEL and U+2028 pass through,
// as they do in v1, so a line read and written back keeps its bytes.
func AppendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); i++ {
		esc := escapeOf(s[i])
		if esc == "" {
			continue
		}
		dst = append(dst, s[start:i]...)
		dst = append(dst, esc...)
		start = i + 1
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

// escapeOf is what replaces one byte inside a string, or "" for a byte that
// is written as it is. A byte of a multibyte character is never below 0x20,
// so the test is safe on bytes.
func escapeOf(b byte) string {
	switch b {
	case '"':
		return `\"`
	case '\\':
		return `\\`
	case '\b':
		return `\b`
	case '\t':
		return `\t`
	case '\n':
		return `\n`
	case '\f':
		return `\f`
	case '\r':
		return `\r`
	}
	if b < 0x20 {
		return `\u00` + string([]byte{hexDigits[b>>4], hexDigits[b&0xF]})
	}
	return ""
}

// The decimal exponents between which a float is written without an
// exponent. Outside them v1 writes `1e-6` and `1e+16`.
const (
	minPlainExponent = -5
	maxPlainExponent = 15
)

// AppendFloat adds f as v1 writes an f64: the shortest digits that read back
// to the same value, always with a fraction (`3.0`, never `3`), and with a
// signed exponent that has no padding (`1e-7`, `1e+16`). A value that is not
// a number is `null`, because JSON has no other word for it.
func AppendFloat(dst []byte, f float64) []byte {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return append(dst, "null"...)
	}
	if math.Signbit(f) {
		dst = append(dst, '-')
		f = -f
	}
	if f == 0 {
		return append(dst, "0.0"...)
	}
	digits, exponent := shortest(f)
	switch {
	case exponent < minPlainExponent || exponent > maxPlainExponent:
		return appendScientific(dst, digits, exponent)
	case exponent < 0:
		dst = append(dst, "0."...)
		dst = append(dst, strings.Repeat("0", -exponent-1)...)
		return append(dst, digits...)
	case len(digits) <= exponent+1:
		dst = append(dst, digits...)
		dst = append(dst, strings.Repeat("0", exponent+1-len(digits))...)
		return append(dst, ".0"...)
	}
	dst = append(dst, digits[:exponent+1]...)
	dst = append(dst, '.')
	return append(dst, digits[exponent+1:]...)
}

// shortest is the shortest run of digits that reads back as f, and the
// decimal exponent of the first of them: 2.5 is "25" and 0, 0.025 is "25"
// and -2.
func shortest(f float64) (digits string, exponent int) {
	text := strconv.FormatFloat(f, 'e', -1, 64)
	mantissa, exp, _ := strings.Cut(text, "e")
	exponent, _ = strconv.Atoi(exp)
	return strings.Replace(mantissa, ".", "", 1), exponent
}

func appendScientific(dst []byte, digits string, exponent int) []byte {
	dst = append(dst, digits[0])
	if len(digits) > 1 {
		dst = append(dst, '.')
		dst = append(dst, digits[1:]...)
	}
	dst = append(dst, 'e')
	if exponent >= 0 {
		dst = append(dst, '+')
	}
	return strconv.AppendInt(dst, int64(exponent), 10)
}

// Object writes one JSON object, a field at a time, in the order the fields
// are given. v1's field order is part of the ledger's bytes, and a Go map or
// a reordered struct would lose it.
type Object struct {
	buf []byte
}

func (o *Object) name(name string) {
	if len(o.buf) == 0 {
		o.buf = append(o.buf, '{')
	} else {
		o.buf = append(o.buf, ',')
	}
	o.buf = AppendString(o.buf, name)
	o.buf = append(o.buf, ':')
}

// String adds a string field.
func (o *Object) String(name, value string) {
	o.name(name)
	o.buf = AppendString(o.buf, value)
}

// Int adds a signed whole number.
func (o *Object) Int(name string, value int64) {
	o.name(name)
	o.buf = strconv.AppendInt(o.buf, value, 10)
}

// Uint adds an unsigned whole number.
func (o *Object) Uint(name string, value uint64) {
	o.name(name)
	o.buf = strconv.AppendUint(o.buf, value, 10)
}

// Float adds a float in v1's spelling.
func (o *Object) Float(name string, value float64) {
	o.name(name)
	o.buf = AppendFloat(o.buf, value)
}

// Bool adds true or false.
func (o *Object) Bool(name string, value bool) {
	o.name(name)
	o.buf = strconv.AppendBool(o.buf, value)
}

// Null adds a field with no value.
func (o *Object) Null(name string) {
	o.name(name)
	o.buf = append(o.buf, "null"...)
}

// Raw adds a field whose value is JSON already: an object or an array.
func (o *Object) Raw(name string, value []byte) {
	o.name(name)
	o.buf = append(o.buf, value...)
}

// Bytes is the finished object. An object with no fields is `{}`.
func (o *Object) Bytes() []byte {
	if len(o.buf) == 0 {
		return []byte("{}")
	}
	return append(slices.Clip(o.buf), '}')
}

// Array joins values that are JSON already. No values is `[]`, never
// `null`: v1 refuses a null list and drops the whole record with it.
func Array(items [][]byte) []byte {
	out := []byte{'['}
	for i, item := range items {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, item...)
	}
	return append(out, ']')
}

// Strings is an array of strings.
func Strings(values []string) []byte {
	items := make([][]byte, len(values))
	for i, v := range values {
		items[i] = AppendString(nil, v)
	}
	return Array(items)
}
