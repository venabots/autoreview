package prs

// GitHub's own stacks. A PR in one says which stack it is in and which
// layer it is, so nothing has to be worked out from branch names or commits:
// what GitHub says is what the list shows. Each layer's diff is its own, so
// no layer waits for the one below it.

// Layer is a PR's place in a GitHub stack. Position 1 is the layer nearest
// the base branch.
type Layer struct {
	Stack    int
	Position int
	Size     int
}

// layerOf is nil for a PR in no GitHub stack. An answer with a stack and no
// position is not a layer anyone can name.
func layerOf(n node) *Layer {
	if n.Stack == nil || n.StackEntry == nil || n.StackEntry.Position < 1 {
		return nil
	}
	return &Layer{Stack: n.Stack.Number, Position: n.StackEntry.Position, Size: n.Stack.Size}
}

// nativeParents is each layer of a GitHub stack that has the layer below it
// in the list, and that layer. A layer whose PR below is merged or past the
// list's end sits on nothing this list can name.
func nativeParents(nodes []node) map[int]Parent {
	type place struct{ stack, position int }
	at := map[place]int{}
	for _, n := range nodes {
		if l := layerOf(n); l != nil {
			at[place{l.Stack, l.Position}] = n.Number
		}
	}
	found := map[int]Parent{}
	for _, n := range nodes {
		l := layerOf(n)
		if l == nil {
			continue
		}
		if below, ok := at[place{l.Stack, l.Position - 1}]; ok {
			found[n.Number] = Parent{Number: below, Evidence: Native}
		}
	}
	return found
}
