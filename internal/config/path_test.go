package config

import "testing"

func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestThePathIsTheOverrideThenXDGThenHome(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		want   string
		wantOK bool
	}{
		{"the override wins", map[string]string{"AUTOREVIEW_CONFIG": "/x/c.toml", "XDG_CONFIG_HOME": "/c", "HOME": "/h"}, "/x/c.toml", true},
		{"off reads no file", map[string]string{"AUTOREVIEW_CONFIG": "OFF", "XDG_CONFIG_HOME": "/c", "HOME": "/h"}, "", false},
		{"then the XDG config directory", map[string]string{"XDG_CONFIG_HOME": "/c", "HOME": "/h"}, "/c/autoreview/config.toml", true},
		{"then the home directory", map[string]string{"HOME": "/h"}, "/h/.config/autoreview/config.toml", true},
		{"no home has no file", map[string]string{}, "", false},
	}
	for _, c := range cases {
		got, ok := Path(envOf(c.env))
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: Path = %q, %v; want %q, %v", c.name, got, ok, c.want, c.wantOK)
		}
	}
}
