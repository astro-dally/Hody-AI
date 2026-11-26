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
// not. One slip models often make is tolerated: indentation that is off by
// the same number of spaces on every line of a hunk. Such a hunk is
// matched with that offset and its new lines are shifted by the same
// amount, so the result keeps the text's real indentation and structure.
func Apply(text, diff string) (string, error) {
	hunks, err := parseDiff(diff)
	if err != nil {
		return "", err
	}
	lines := strings.Split(text, "\n")
	var out []string
	cursor := 0
	for i, h := range hunks {
		at, added := find(lines, h.old, cursor, h.start-1), h.new
		if at < 0 {
			var shift int
			if at, shift = findShifted(lines, h.old, cursor, h.start-1); at >= 0 {
				var ok bool
				if added, ok = reindent(h.new, shift); !ok {
					at = -1
				}
			}
		}
		if at < 0 {
			return "", fmt.Errorf("hunk %d does not match the manifest", i+1)
		}
		out = append(out, lines[cursor:at]...)
		out = append(out, added...)
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

// findShifted is find for a block whose indentation differs from the
// text's by the same non-zero number of spaces on every non-blank line. It
// returns the position and the shift (text indentation minus block
// indentation), or -1.
func findShifted(lines, block []string, from, hint int) (int, int) {
	best, bestShift := -1, 0
	for i := from; i+len(block) <= len(lines); i++ {
		if d, ok := shiftOf(lines[i:i+len(block)], block); ok && d != 0 && (best < 0 || abs(i-hint) < abs(best-hint)) {
			best, bestShift = i, d
		}
	}
	return best, bestShift
}

// shiftOf reports the constant indentation difference between text and
// block, which must otherwise be equal line by line.
func shiftOf(text, block []string) (int, bool) {
	d, seen := 0, false
	for k := range block {
		t, b := strings.TrimRight(text[k], " \t"), strings.TrimRight(block[k], " \t")
		if t == "" || b == "" {
			if t != b {
				return 0, false
			}
			continue
		}
		tt, bt := strings.TrimLeft(t, " "), strings.TrimLeft(b, " ")
		if tt != bt {
			return 0, false
		}
		diff := (len(t) - len(tt)) - (len(b) - len(bt))
		if seen && diff != d {
			return 0, false
		}
		d, seen = diff, true
	}
	return d, seen
}

// reindent shifts every non-blank line by d spaces (d < 0 removes them).
// It fails if a line has fewer leading spaces than it should lose.
func reindent(block []string, d int) ([]string, bool) {
	out := make([]string, len(block))
	for i, l := range block {
		switch {
		case strings.TrimSpace(l) == "":
			out[i] = l
		case d > 0:
			out[i] = strings.Repeat(" ", d) + l
		case !strings.HasPrefix(l, strings.Repeat(" ", -d)):
			return nil, false
		default:
			out[i] = l[-d:]
		}
	}
	return out, true
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
