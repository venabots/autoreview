package cli

import "errors"

// The variables that set the loop's defaults.
const (
	EnvBabysit   = "AUTOREVIEW_BABYSIT_INTERVAL"
	EnvMaxIdle   = "AUTOREVIEW_MAX_IDLE"
	EnvMaxPasses = "AUTOREVIEW_MAX_PASSES"
	EnvCIWait    = "AUTOREVIEW_CI_WAIT"
)

const (
	defaultBabysit   = "30"
	defaultMaxIdle   = "3"
	defaultMaxPasses = "3"
	defaultCIWait    = "30"
	maxIdle          = 100_000
	maxPasses        = 1000
	// The autofix cap has no flag and no variable, and its default is
	// babysit.MaxStreak.
	defaultAutofixCap = "3"
	maxAutofixCap     = 100
)

// withLoopFlags checks the flags that say what a run does after its first
// pass. babysit is the raw --babysit value, noValue when it was given bare.
func withLoopFlags(cfg Config, raw map[string]*string, babysit string, given func(string) bool, env func(string) string, set settings) (Config, error) {
	cfg.WatchGiven = given("watch")
	cfg.BabysitGiven = given("babysit")
	if cfg.Once && (cfg.WatchGiven || cfg.BabysitGiven) {
		return Config{}, errors.New("--once cannot be used with --watch or --babysit, which keep the run going")
	}
	var err error
	value, told := set.pick("babysit", babysit, babysit != "" && babysit != noValue, env(EnvBabysit), defaultBabysit)
	if cfg.Babysit, err = ParseInterval(value, "babysit"); err != nil {
		return Config{}, told(err)
	}
	// The wait for checks has no flag.
	value, told = set.pick("ci-wait", "", false, env(EnvCIWait), defaultCIWait)
	if cfg.CIWait, err = ParseInterval(value, "CI wait"); err != nil {
		return Config{}, told(err)
	}
	value, told = set.pick("max-idle", *raw["max-idle"], given("max-idle"), env(EnvMaxIdle), defaultMaxIdle)
	if cfg.MaxIdle, err = count("max-idle", value, 1, maxIdle); err != nil {
		return Config{}, told(err)
	}
	value, told = set.pick("max-passes", *raw["max-passes"], given("max-passes"), env(EnvMaxPasses), defaultMaxPasses)
	if cfg.MaxPasses, err = count("max-passes", value, 1, maxPasses); err != nil {
		return Config{}, told(err)
	}
	value, told = set.pick("autofix-cap", "", false, "", defaultAutofixCap)
	if cfg.AutofixCap, err = countOf("the autofix cap", value, 1, maxAutofixCap); err != nil {
		return Config{}, told(err)
	}
	return cfg, nil
}
