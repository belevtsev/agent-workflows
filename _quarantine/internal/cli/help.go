package cli

import (
	"embed"
	"strings"
)

// helpFiles holds the long help of the commands whose explanation runs past a
// few sentences, one sentence per line, so the code keeps only the wiring.
//
//go:embed help/*.txt
var helpFiles embed.FS

// helpText returns the long help of command from help/<command>.txt. A
// missing file is a build mistake that every help test catches, so it panics.
func helpText(command string) string {
	data, err := helpFiles.ReadFile("help/" + command + ".txt")
	if err != nil {
		panic("cli: no help file for " + command + ": " + err.Error())
	}
	return strings.TrimRight(string(data), "\n")
}
