package main

import (
	"io"
	"os"
	"strings"
)

const logo = ` _               _
| |__   ___   __| |_   _
| '_ \ / _ \ / _` + "`" + ` | | | |
| | | | (_) | (_| | |_| |
|_| |_|\___/ \__,_|\__, |
                   |___/`

// banner is the greeting shown when hody is run with no command at a
// terminal.
func banner(color bool) string {
	cyan, bold, reset := "", "", ""
	if color {
		cyan, bold, reset = "\033[36m", "\033[1m", "\033[0m"
	}
	return cyan + logo + reset + "   " + version + "\n\n" +
		bold + "Hello from Hody 👋" + reset + "  I find out why containers crash. I only look, never touch.\n"
}

// isTerminal reports whether w is an interactive terminal. It is a
// variable so tests can pretend.
var isTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// useColor follows the NO_COLOR convention (https://no-color.org) and
// leaves dumb terminals alone.
func useColor(w io.Writer) bool {
	return isTerminal(w) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" &&
		!strings.EqualFold(os.Getenv("TERM"), "")
}
