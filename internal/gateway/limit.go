package gateway

import (
	"fmt"
	"regexp"
)

// Limits bounds how much of one tool result the model sees.
type Limits struct {
	Head    int // first lines always kept
	Tail    int // last lines always kept
	Middle  int // max matching lines kept from between head and tail
	LineLen int // longer lines are truncated
}

// DefaultLimits keeps a result to roughly 250 lines.
var DefaultLimits = Limits{Head: 60, Tail: 80, Middle: 110, LineLen: 400}

var errorLineRE = regexp.MustCompile(`(?i)(error|\berr\b|fail|fatal|panic|exception|denied|refused|killed|oom|exit(ed)? ?(code|status)|back-?off|unhealthy|not found|no such|timeout|timed out|cannot|unable|invalid|missing|warn)`)

// keptLine is one line that survived limiting; n is its 1-based number in
// the full text, or 0 for an omission marker.
type keptLine struct {
	n    int
	text string
}

// limit keeps the head, the tail, and error or flagged lines in between.
// It returns the kept lines and how many lines were dropped.
func limit(lines []string, flagged map[int]bool, l Limits) ([]keptLine, int) {
	keep := func(i int) keptLine { return keptLine{n: i + 1, text: clip(lines[i], l.LineLen)} }
	if len(lines) <= l.Head+l.Tail+l.Middle {
		out := make([]keptLine, len(lines))
		for i := range lines {
			out[i] = keep(i)
		}
		return out, 0
	}
	var out []keptLine
	for i := 0; i < l.Head; i++ {
		out = append(out, keep(i))
	}
	omitted, gap, middle := 0, 0, 0
	end := len(lines) - l.Tail
	for i := l.Head; i < end; i++ {
		important := flagged[i+1] || errorLineRE.MatchString(lines[i])
		if important && (middle < l.Middle || flagged[i+1]) {
			if gap > 0 {
				out = append(out, omission(gap))
				gap = 0
			}
			out = append(out, keep(i))
			middle++
			continue
		}
		gap++
		omitted++
	}
	if gap > 0 {
		out = append(out, omission(gap))
	}
	for i := end; i < len(lines); i++ {
		out = append(out, keep(i))
	}
	return out, omitted
}

func omission(n int) keptLine {
	return keptLine{text: fmt.Sprintf("… [%d lines omitted by Hody's size limit] …", n)}
}
