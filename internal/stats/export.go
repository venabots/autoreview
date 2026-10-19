package stats

// ParseSince reads a --since value for another subcommand. A second parser
// would let `autoreview doctor --since 2w` and `autoreview stats --since 2w`
// mean two different days.
func ParseSince(raw string, now int64) (int64, error) {
	return parseSince(raw, now)
}

// Cohort is the backend and the model a panelist is counted under. Another
// report that spelled the model its own way would show `xai/grok-4.7` where
// stats shows `grok-4.7`, and the two could not be compared.
func Cohort(name string, model *string) (backend, spelled string) {
	k := canonical(name, model)
	return k.backend, k.model
}
