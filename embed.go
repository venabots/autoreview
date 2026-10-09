// Package autoreview holds the files the binary carries with it.
//
// The binary invokes review skills by name, and an agent finds a skill on
// disk. So the tree under skills/ is embedded at build time and written out
// again for each run. A Homebrew install then needs nothing else, and the
// skill a review ran is the one this version was built with.
//
// The embed is here, at the module root, because a go:embed pattern cannot
// reach above its own package directory.
package autoreview

import (
	"embed"
	"io/fs"
)

// The all: prefix keeps files whose names start with a dot or an
// underscore, which a plain pattern leaves out without a word.
//
//go:embed all:skills
var bundled embed.FS

// The prompts `autoreview panel` hands its panelists and its synthesizer.
// They are compiled in for the reason the skills are: a panel whose prompt
// changed under it on disk would make two runs incomparable, and nothing
// would say why.
//
//go:embed prompts/*.md
var prompts embed.FS

// Prompt is the text of one bundled prompt, by file name: "review.md".
func Prompt(name string) string {
	text, err := prompts.ReadFile("prompts/" + name)
	if err != nil {
		// Only the names in internal/panel are asked for, and
		// embed_test.go reads each one.
		panic(err)
	}
	return string(text)
}

// Skills is the bundled skills: one directory for each skill, each with a
// SKILL.md.
func Skills() fs.FS {
	skills, err := fs.Sub(bundled, "skills")
	if err != nil {
		// The pattern above embeds the directory, so it is always there.
		panic(err)
	}
	return skills
}
