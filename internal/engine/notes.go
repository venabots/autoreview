package engine

import "maps"

// withNote and withoutNote return the notes with one PR's entry set or
// gone. A published snapshot holds the old map, so it is never written to.
func withNote(notes map[int]string, pr int, text string) map[int]string {
	next := maps.Clone(notes)
	if next == nil {
		next = map[int]string{}
	}
	next[pr] = text
	return next
}

func withoutNote(notes map[int]string, pr int) map[int]string {
	if _, ok := notes[pr]; !ok {
		return notes
	}
	next := maps.Clone(notes)
	delete(next, pr)
	return next
}
