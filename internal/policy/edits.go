package policy

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"hody/internal/model"
)

// MaxEdits bounds the number of edits in one fix.
const MaxEdits = 10

// Patch applies the model's edits to a copy of the manifest, builds the
// unified diff the user sees, and checks that the diff reproduces exactly
// the text the policy rules will judge.
func Patch(original, name string, edits []model.Edit) (patched, diff string, err error) {
	patched, err = ApplyEdits(original, edits)
	if err != nil {
		return "", "", err
	}
	diff = Diff(name, original, patched)
	if back, err := Apply(original, diff); err != nil || back != patched {
		return "", "", errors.New("internal error: the diff Hody built does not reproduce the edits")
	}
	return patched, diff, nil
}

// ApplyEdits applies edits in order to text and returns the result. Each
// edit's find text must be complete, consecutive lines of the text and
// occur exactly once. Two slips that models often make are tolerated: an
// indentation that is off by the same number of spaces on every line (the
// replacement is shifted by the same amount, keeping the text's structure),
// and the evidence block's line-number column copied into the lines.
// Anything else is refused with a message saying what to correct.
func ApplyEdits(text string, edits []model.Edit) (string, error) {
	if len(edits) > MaxEdits {
		return "", fmt.Errorf("a fix may have at most %d edits", MaxEdits)
	}
	lines := strings.Split(text, "\n")
	for i, ed := range edits {
		find, replace := editLines(ed)
		if len(find) == 0 {
			return "", fmt.Errorf("edit %d: \"find\" is empty", i+1)
		}
		at, shift, err := locate(lines, find)
		if err != nil {
			return "", fmt.Errorf("edit %d: %w", i+1, err)
		}
		if shift != 0 {
			var ok bool
			if replace, ok = reindent(replace, shift); !ok {
				return "", fmt.Errorf("edit %d: its \"replace\" lines are indented less than its \"find\" lines", i+1)
			}
		}
		lines = slices.Concat(lines[:at], replace, lines[at+len(find):])
	}
	out := strings.Join(lines, "\n")
	if out == text {
		return "", errors.New("the edits change nothing")
	}
	return out, nil
}

// lineNumberRE matches the line-number column of an evidence block
// ("   12 | " or, for a flagged line, "   12!| ").
var lineNumberRE = regexp.MustCompile(`^ *\d+!? ?\| ?`)

// editLines splits an edit into lines. If every line of find starts with
// the evidence block's line-number column, the column is removed from
// find and from every replace line that has it.
func editLines(ed model.Edit) (find, replace []string) {
	find, replace = block(ed.Find), block(ed.Replace)
	numbered := len(find) > 0
	for _, l := range find {
		numbered = numbered && (strings.TrimSpace(l) == "" || lineNumberRE.MatchString(l))
	}
	if numbered {
		for _, b := range []*[]string{&find, &replace} {
			for i, l := range *b {
				(*b)[i] = lineNumberRE.ReplaceAllString(l, "")
			}
		}
	}
	return find, replace
}

// block splits s into lines without leading or trailing empty lines; a
// blank s is no lines at all.
func block(s string) []string {
	s = strings.Trim(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// locate finds the one place where find occurs in lines, exactly or with a
// constant indentation shift, and returns it with the shift.
func locate(lines, find []string) (at, shift int, err error) {
	var exact, shifted, shifts []int
	for i := 0; i+len(find) <= len(lines); i++ {
		if equal(lines[i:i+len(find)], find) {
			exact = append(exact, i)
		} else if d, ok := shiftOf(lines[i:i+len(find)], find); ok && d != 0 {
			shifted, shifts = append(shifted, i), append(shifts, d)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], 0, nil
	case len(exact) > 1:
		return -1, 0, ambiguous(exact)
	case len(shifted) == 1:
		return shifted[0], shifts[0], nil
	case len(shifted) > 1:
		return -1, 0, ambiguous(shifted)
	}
	return -1, 0, errors.New(`its "find" text is not in the manifest (copy complete, consecutive lines exactly as they appear after the line numbers)`)
}

func ambiguous(at []int) error {
	nums := make([]string, len(at))
	for i, a := range at {
		nums[i] = fmt.Sprint(a + 1)
	}
	return fmt.Errorf(`its "find" text occurs %d times in the manifest (at lines %s); include a neighbouring line so it occurs only once`,
		len(at), strings.Join(nums, ", "))
}

// Diff returns a unified diff from a to b with three lines of context,
// for the file name, or "" if the texts are equal.
func Diff(name, a, b string) string {
	ops := lineOps(strings.Split(a, "\n"), strings.Split(b, "\n"))
	const context = 3
	var changes []int
	for i, o := range ops {
		if o.kind != ' ' {
			changes = append(changes, i)
		}
	}
	if len(changes) == 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", name, name)
	for k := 0; k < len(changes); {
		first, last := changes[k], changes[k]
		for k++; k < len(changes) && changes[k]-last <= 2*context; k++ {
			last = changes[k]
		}
		start, end := max(0, first-context), min(len(ops), last+context+1)
		oldStart, newStart := 1, 1
		for _, o := range ops[:start] {
			if o.kind != '+' {
				oldStart++
			}
			if o.kind != '-' {
				newStart++
			}
		}
		var oldN, newN int
		var body strings.Builder
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				oldN++
			}
			if o.kind != '-' {
				newN++
			}
			body.WriteString(string(o.kind) + o.text + "\n")
		}
		if oldN == 0 {
			oldStart--
		}
		if newN == 0 {
			newStart--
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n%s", oldStart, oldN, newStart, newN, body.String())
	}
	return out.String()
}

type lineOp struct {
	kind byte // ' ', '-' or '+'
	text string
}

// lineOps is a line diff: the longest common subsequence of the lines
// that differ, after removing the common prefix and suffix.
func lineOps(a, b []string) []lineOp {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	q := 0
	for q < len(a)-p && q < len(b)-p && a[len(a)-1-q] == b[len(b)-1-q] {
		q++
	}
	x, y := a[p:len(a)-q], b[p:len(b)-q]
	// lcs[i][j] is the length of the LCS of x[i:] and y[j:].
	w := len(y) + 1
	lcs := make([]int, (len(x)+1)*w)
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}
	var ops []lineOp
	for _, l := range a[:p] {
		ops = append(ops, lineOp{' ', l})
	}
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			ops = append(ops, lineOp{' ', x[i]})
			i, j = i+1, j+1
		case j == len(y) || i < len(x) && lcs[(i+1)*w+j] >= lcs[i*w+j+1]:
			ops = append(ops, lineOp{'-', x[i]})
			i++
		default:
			ops = append(ops, lineOp{'+', y[j]})
			j++
		}
	}
	for _, l := range a[len(a)-q:] {
		ops = append(ops, lineOp{' ', l})
	}
	return ops
}
