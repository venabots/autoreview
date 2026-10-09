package serde

import (
	"math"
	"testing"
)

// The wanted text in these tables came from serde_json 1.0.151, the version
// v1 0.23.0 locks, run on the same values.

func TestAFloatIsSpelledAsV1SpellsIt(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want string
	}{
		{"a cost with six places", 2.699934, "2.699934"},
		{"a whole number keeps its fraction", 3.0, "3.0"},
		{"one", 1.0, "1.0"},
		{"a hundred", 100.0, "100.0"},
		{"sixteen", 16.0, "16.0"},
		{"zero", 0.0, "0.0"},
		{"negative zero", math.Copysign(0, -1), "-0.0"},
		{"a tenth", 0.1, "0.1"},
		{"a half", 0.5, "0.5"},
		{"a negative", -2.5, "-2.5"},
		{"a fraction on a whole part", 12.5, "12.5"},
		{"two places", 4.35, "4.35"},
		{"a large whole number", 1234567.0, "1234567.0"},
		{"the sum that is not 0.3", 0.30000000000000004, "0.30000000000000004"},
		{"the float under 0.8", 0.7999999999999999, "0.7999999999999999"},
		{"a third", 1.0 / 3.0, "0.3333333333333333"},
		{"two thirds", 2.0 / 3.0, "0.6666666666666666"},
		{"a wilson bound", 0.20654329147389294, "0.20654329147389294"},
		{"the smallest value written plainly", 1e-5, "0.00001"},
		{"digits after leading zeros", 1.234e-5, "0.00001234"},
		{"three leading zeros", 0.000123, "0.000123"},
		{"one step smaller takes an exponent", 1e-6, "1e-6"},
		{"an exponent with a fraction", 1.5e-6, "1.5e-6"},
		{"an exponent with more digits", 0.000001234, "1.234e-6"},
		{"a small value", 1e-7, "1e-7"},
		{"a small negative value", -1e-7, "-1e-7"},
		{"the largest power of ten written plainly", 1e15, "1000000000000000.0"},
		{"sixteen digits written plainly", 9999999999999998.0, "9999999999999998.0"},
		{"two to the fifty-third", 9007199254740992.0, "9007199254740992.0"},
		{"one step larger takes a signed exponent", 1e16, "1e+16"},
		{"a larger power of ten", 1e17, "1e+17"},
		{"seventeen digits", 123456789012345680.0, "1.2345678901234568e+17"},
		{"seventeen digits, one lower", 1.2345678901234566e17, "1.2345678901234566e+17"},
		{"a long value past the plain range", 99999999999999990.0, "9.999999999999998e+16"},
		{"ten to the twenty-first", 1e21, "1e+21"},
		{"ten to the twenty-second", 1e22, "1e+22"},
		{"the smallest float", 5e-324, "5e-324"},
		{"the largest float", math.MaxFloat64, "1.7976931348623157e+308"},
		{"not a number", math.NaN(), "null"},
		{"infinity", math.Inf(1), "null"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(AppendFloat(nil, c.in)); got != c.want {
				t.Errorf("AppendFloat(%g) = %s, want %s", c.in, got, c.want)
			}
		})
	}
}

func TestAStringEscapesOnlyWhatV1Escapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text", "plain", `"plain"`},
		{"a model name with brackets", "claude-opus-5[1m]", `"claude-opus-5[1m]"`},
		{"angle brackets and an ampersand stay", "a<b>&c", `"a<b>&c"`},
		{"a quote and a backslash, and a slash that stays", `q"b\s/`, `"q\"b\\s/"`},
		{
			"the controls",
			"\x00\x01\b\t\n\x0b\f\r\x1b\x1f",
			`"\u0000\u0001\b\t\n\u000b\f\r\u001b\u001f"`,
		},
		{
			"DEL, a C1 control and the line separators stay",
			"\x7f\u0080\u2028\u2029",
			"\"\x7f\u0080\u2028\u2029\"",
		},
		{"text outside ASCII stays", "\u00e9\u65e5\u672c\U0001F642", "\"\u00e9\u65e5\u672c\U0001F642\""},
		{"nothing", "", `""`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(AppendString(nil, c.in)); got != c.want {
				t.Errorf("AppendString(%q) = %s, want %s", c.in, got, c.want)
			}
		})
	}
}

func TestAnObjectKeepsTheOrderItWasGiven(t *testing.T) {
	var o Object
	o.Uint("v", 1)
	o.String("id", "a")
	o.Int("at", -5)
	o.Float("cost", 3)
	o.Bool("ok", true)
	o.Null("since")
	o.Raw("list", Strings([]string{"x", "y"}))
	want := `{"v":1,"id":"a","at":-5,"cost":3.0,"ok":true,"since":null,"list":["x","y"]}`
	if got := string(o.Bytes()); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := string(o.Bytes()); got != want {
		t.Errorf("a second read changed the object: %s", got)
	}
}

func TestNothingIsWrittenAsEmptyNeverNull(t *testing.T) {
	var o Object
	if got := string(o.Bytes()); got != "{}" {
		t.Errorf("an object with no fields = %s, want {}", got)
	}
	if got := string(Array(nil)); got != "[]" {
		t.Errorf("an array with no items = %s, want []", got)
	}
	if got := string(Strings(nil)); got != "[]" {
		t.Errorf("no strings = %s, want []", got)
	}
}
