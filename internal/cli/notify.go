package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/venabots/autoreview/internal/config"
)

// Channels is every channel an alert can go to. cmd/autoreview holds one
// sender for each, and a test there keeps the two lists the same.
var Channels = []string{"herdr"}

// defaultChannels is where alerts go when the file names none. herdr does
// nothing outside Herdr, so it costs nothing as a default.
var defaultChannels = []string{"herdr"}

// notifyChannels is where alerts go: the file's notify.channels, or the
// default when the file names none. An empty list turns alerts off.
func notifyChannels(file config.File) ([]string, error) {
	if !file.Keys()["notify.channels"] {
		return slices.Clone(defaultChannels), nil
	}
	for _, c := range file.Notify.Channels {
		if !slices.Contains(Channels, c) {
			return nil, fmt.Errorf("notify.channels in %s: %q is not a channel; the channels are %s", file.Path, c, strings.Join(Channels, ", "))
		}
	}
	return slices.Clone(file.Notify.Channels), nil
}
