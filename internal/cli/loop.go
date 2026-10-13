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
)

// withLoopFlags checks the flags that say what a run does after its first
// pass. babysit is the raw --babysit value, noValue when it was given bare.
func withLoopFlags(cfg Config, raw map[string]*string, babysit string, given func(string) bool, env func(string) string) (Config, error) {
	cfg.WatchGiven = given("watch")
	cfg.BabysitGiven = given("babysit")
	if cfg.Once && (cfg.WatchGiven || cfg.BabysitGiven) {
		return Config{}, errors.New("--once cannot be used with --watch or --babysit, which keep the run going")
	}
	var err error
	value := chosen(babysit, babysit != "" && babysit != noValue, env(EnvBabysit), defaultBabysit)
	if cfg.Babysit, err = ParseInterval(value, "babysit"); err != nil {
		return Config{}, err
	}
	if cfg.CIWait, err = ParseInterval(chosen("", false, env(EnvCIWait), defaultCIWait), "CI wait"); err != nil {
		return Config{}, err
	}
	if cfg.MaxIdle, err = count("max-idle", chosen(*raw["max-idle"], given("max-idle"), env(EnvMaxIdle), defaultMaxIdle), 1, maxIdle); err != nil {
		return Config{}, err
	}
	cfg.MaxPasses, err = count("max-passes", chosen(*raw["max-passes"], given("max-passes"), env(EnvMaxPasses), defaultMaxPasses), 1, maxPasses)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}
