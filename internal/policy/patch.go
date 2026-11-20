package policy

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var hunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+\d+(?:,\d+)? @@`)

type hunk struct {
	start    int // 1-based line in the original, from the header
	old, new []string
}

// Apply applies a unified diff for a single file to text, in memory, and
// returns the patched text. Each hunk must match the text exactly; its
// position is searched near the header's line number, after the previous
// hunk, so slightly wrong line numbers are tolerated but wrong content is
// not.
func Apply(text, diff string) (string, error) {
	hunks, err := parseDiff(diff)
	if err != nil {
		return "", err
	}
	lines := strings.Split(text, "\n")
	var out []string
	cursor := 0
	for i, h := range hunks {
		at := find(lines, h.old, cursor, h.start-1)
		if at < 0 {
			return "", fmt.Errorf("hunk %d does not match the manifest", i+1)
		}
		out = append(out, lines[cursor:at]...)
		out = append(out, h.new...)
		cursor = at + len(h.old)
	}
	out = append(out, lines[cursor:]...)
	return strings.Join(out, "\n"), nil
}

func parseDiff(diff string) ([]hunk, error) {
	var hunks []hunk
	files := 0
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ "):
			if files++; files > 1 {
				return nil, errors.New("the diff changes more than one file")
			}
			i++ // skip the +++ line
		case strings.HasPrefix(line, "@@"):
			m := hunkRE.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("bad hunk header %q", line)
			}
			start, _ := strconv.Atoi(m[1])
			hunks = append(hunks, hunk{start: start})
		case len(hunks) == 0 || strings.HasPrefix(line, `\`):
			// text before the first hunk, or "\ No newline at end of file"
		default:
			if err := addLine(&hunks[len(hunks)-1], line); err != nil {
				return nil, err
			}
		}
	}
	if len(hunks) == 0 {
		return nil, errors.New("the diff has no hunks")
	}
	for i, h := range hunks {
		if len(h.old) == 0 && len(h.new) == 0 {
			return nil, fmt.Errorf("hunk %d is empty", i+1)
		}
	}
	return hunks, nil
}

// find returns where block occurs in lines at or after from, preferring the
// position closest to hint.
func find(lines, block []string, from, hint int) int {
	if len(block) == 0 {
		return max(from, min(hint+1, len(lines)))
	}
	best := -1
	for i := from; i+len(block) <= len(lines); i++ {
		if equal(lines[i:i+len(block)], block) && (best < 0 || abs(i-hint) < abs(best-hint)) {
			best = i
		}
	}
	return best
}

func equal(a, b []string) bool {
	for i := range a {
		if strings.TrimRight(a[i], " \t") != strings.TrimRight(b[i], " \t") {
			return false
		}
	}
	return true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// addLine adds one hunk body line. A completely empty line is treated as
// an unchanged blank line, because models often drop its leading space.
func addLine(h *hunk, line string) error {
	mark, body := byte(' '), ""
	if line != "" {
		mark, body = line[0], line[1:]
	}
	switch mark {
	case ' ':
		h.old, h.new = append(h.old, body), append(h.new, body)
	case '-':
		h.old = append(h.old, body)
	case '+':
		h.new = append(h.new, body)
	default:
		return fmt.Errorf("unexpected diff line %q", line)
	}
	return nil
}
