package cli

import (
	"strings"
	"testing"
	"time"
)

func TestLoopFlagDefaults(t *testing.T) {
	cfg := parse(t)
	if cfg.WatchGiven || cfg.BabysitGiven || cfg.Once {
		t.Errorf("a loop flag is on by default: %+v", cfg)
	}
	if cfg.Babysit.Duration != 30*time.Minute || cfg.CIWait.Duration != 30*time.Minute || cfg.MaxIdle != 3 || cfg.MaxPasses != 3 {
		t.Errorf("defaults: babysit %v, CI wait %v, max idle %d, max passes %d", cfg.Babysit.Duration, cfg.CIWait.Duration, cfg.MaxIdle, cfg.MaxPasses)
	}
}

func TestLoopFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  map[string]string
		want func(Config) bool
	}{
		{"a bare babysit", []string{"--babysit"}, nil, func(c Config) bool { return c.BabysitGiven && c.Babysit.Duration == 30*time.Minute }},
		{"a babysit interval", []string{"-b=1h"}, nil, func(c Config) bool { return c.BabysitGiven && c.Babysit.Duration == time.Hour }},
		{"the babysit variable", []string{"--babysit"}, map[string]string{EnvBabysit: "15"}, func(c Config) bool { return c.Babysit.Duration == 15*time.Minute }},
		{"the babysit variable without the flag is the rest", nil, map[string]string{EnvBabysit: "10"}, func(c Config) bool {
			return !c.BabysitGiven && c.Babysit.Duration == 10*time.Minute
		}},
		{"watch is noted", []string{"--watch=5"}, nil, func(c Config) bool { return c.WatchGiven && !c.BabysitGiven }},
		{"both", []string{"--watch", "--babysit=45"}, nil, func(c Config) bool {
			return c.WatchGiven && c.BabysitGiven && c.Babysit.Duration == 45*time.Minute
		}},
		{"the caps", []string{"--max-idle", "5", "--max-passes=2"}, nil, func(c Config) bool { return c.MaxIdle == 5 && c.MaxPasses == 2 }},
		{"the caps from the environment", nil, map[string]string{EnvMaxIdle: "9", EnvMaxPasses: "7", EnvCIWait: "1h"}, func(c Config) bool {
			return c.MaxIdle == 9 && c.MaxPasses == 7 && c.CIWait.Duration == time.Hour
		}},
		{"once", []string{"--once"}, nil, func(c Config) bool { return c.Once }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := Parse(c.args, envOf(c.env))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !c.want(cfg) {
				t.Errorf("Parse(%q) = %+v", c.args, cfg)
			}
		})
	}
}

func TestLoopFlagsRefuse(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{"once with watch", []string{"--once", "--watch"}, nil, "--once cannot be used with --watch or --babysit"},
		{"once with babysit", []string{"--babysit=10", "--once"}, nil, "--once cannot be used with --watch or --babysit"},
		{"a babysit value after a space", []string{"--babysit", "15"}, nil, `--babysit takes its value with "=": write --babysit=15`},
		{"a short babysit value after a space", []string{"-b", "15"}, nil, `-b takes its value with "="`},
		{"a babysit of zero", []string{"--babysit=0"}, nil, "invalid babysit interval"},
		{"a bad babysit variable", nil, map[string]string{EnvBabysit: "soon"}, `invalid babysit interval: "soon"`},
		{"a bad CI wait variable", nil, map[string]string{EnvCIWait: "30s"}, `invalid CI wait interval: "30s"`},
		{"zero idle checks", []string{"--max-idle=0"}, nil, `--max-idle expects an integer >= 1, got "0"`},
		{"too many passes", []string{"--max-passes", "5000"}, nil, `--max-passes expects an integer <= 1000`},
		{"a flag where the cap goes", []string{"--max-passes", "--all"}, nil, "--max-passes expects a value, but found the flag --all"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.args, envOf(c.env))
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("Parse(%q) error = %v, want one containing %q", c.args, err, c.wantErr)
			}
		})
	}
}
