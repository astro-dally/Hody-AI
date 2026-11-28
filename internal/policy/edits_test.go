package policy

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"hody/internal/model"
)

const deployment = `spec:
  template:
    spec:
      containers:
      - name: app
        image: shop/app:1.0
        resources:
          limits:
            memory: 64Mi
          requests:
            memory: 64Mi`

func ed(find, replace string) model.Edit { return model.Edit{Find: find, Replace: replace} }

// Run 3's fixes were hand-written diffs, and 7 of 12 did not match the
// manifest. Edits name the lines to change by their content instead; these
// are the shapes models produce, and the mistakes Hody must refuse.
func TestApplyEdits(t *testing.T) {
	cases := []struct {
		name  string
		edits []model.Edit
		want  string // the changed lines, or the error
		err   bool
	}{
		{"one line, exact", []model.Edit{ed("        image: shop/app:1.0", "        image: shop/app:1.1")},
			"        image: shop/app:1.1", false},
		{"several lines make it unique", []model.Edit{ed("          limits:\n            memory: 64Mi", "          limits:\n            memory: 256Mi")},
			"          limits:\n            memory: 256Mi\n          requests:\n            memory: 64Mi", false},
		{"indentation off by a constant is shifted back", []model.Edit{ed("limits:\n  memory: 64Mi", "limits:\n  memory: 256Mi")},
			"          limits:\n            memory: 256Mi\n          requests:", false},
		{"add a line by repeating its neighbour", []model.Edit{ed("        image: shop/app:1.0", "        image: shop/app:1.0\n        imagePullPolicy: IfNotPresent")},
			"        image: shop/app:1.0\n        imagePullPolicy: IfNotPresent\n        resources:", false},
		{"delete lines", []model.Edit{ed("          requests:\n            memory: 64Mi", "")},
			"            memory: 64Mi", false},
		{"line-number column copied from the evidence", []model.Edit{ed("    6 |         image: shop/app:1.0", "    6 |         image: shop/app:1.1")},
			"        image: shop/app:1.1", false},
		{"trailing newlines and CRLF are ignored", []model.Edit{ed("        image: shop/app:1.0\r\n", "        image: shop/app:1.1\n")},
			"        image: shop/app:1.1", false},
		{"two edits in order", []model.Edit{ed("        image: shop/app:1.0", "        image: shop/app:1.1"), ed("      - name: app", "      - name: app2")},
			"      - name: app2\n        image: shop/app:1.1", false},

		{"text that is not in the manifest", []model.Edit{ed("        resources: {}", "        resources: {limits: {memory: 1Gi}}")}, "not in the manifest", true},
		{"a partial line", []model.Edit{ed("shop/app:1.0", "shop/app:1.1")}, "not in the manifest", true},
		{"ambiguous text says where it occurs", []model.Edit{ed("            memory: 64Mi", "            memory: 1Gi")}, "occurs 2 times in the manifest (at lines 9, 11)", true},
		{"ambiguous after an indentation shift", []model.Edit{ed("memory: 64Mi", "memory: 1Gi")}, "occurs 2 times", true},
		{"empty find", []model.Edit{ed(" \n", "x: 1")}, `"find" is empty`, true},
		{"no change", []model.Edit{ed("        image: shop/app:1.0", "        image: shop/app:1.0")}, "change nothing", true},
		{"replacement indented too little for the shift", []model.Edit{ed("          image: shop/app:1.0", "image: x")}, "indented less", true},
		{"too many edits", make([]model.Edit, MaxEdits+1), "at most", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ApplyEdits(deployment, c.edits)
			if c.err {
				if err == nil || !strings.Contains(err.Error(), c.want) {
					t.Fatalf("err=%v, want it to contain %q (got %q)", err, c.want, got)
				}
				return
			}
			if err != nil || !strings.Contains(got, c.want) {
				t.Fatalf("got %q err %v, want it to contain %q", got, err, c.want)
			}
		})
	}
}

func TestDiffFormat(t *testing.T) {
	a := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj"
	b := "a\nb\nc\nD\ne\nf\ng\nh\ni\nj\nk"
	want := "--- a/m.yaml\n+++ b/m.yaml\n@@ -1,7 +1,7 @@\n a\n b\n c\n-d\n+D\n e\n f\n g\n@@ -8,3 +8,4 @@\n h\n i\n j\n+k\n"
	if got := Diff("m.yaml", a, b); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if Diff("m", a, a) != "" {
		t.Fatal("equal texts must give an empty diff")
	}
}

// The diff Hody shows must be exactly the change the rules judged: for
// many random edits, applying the built diff reproduces the edited text.
func TestPatchRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	base := strings.Split(deployment+"\n"+strings.ReplaceAll(deployment, "app", "side"), "\n")
	checked := 0
	for n := 0; n < 300; n++ {
		lines := append([]string(nil), base...)
		var edits []model.Edit
		for k := 0; k < 1+r.IntN(3); k++ {
			at := r.IntN(len(lines) - 2)
			size := 1 + r.IntN(2)
			find := strings.Join(lines[at:at+size], "\n")
			if strings.Count(strings.Join(lines, "\n"), find) != 1 {
				continue
			}
			var repl []string
			for j := 0; j < r.IntN(3); j++ {
				repl = append(repl, fmt.Sprintf("        new%d-%d: x", n, j))
			}
			edits = append(edits, ed(find, strings.Join(repl, "\n")))
			lines = append(lines[:at], append(repl, lines[at+size:]...)...)
		}
		if len(edits) == 0 {
			continue
		}
		original := strings.Join(base, "\n")
		patched, diff, err := Patch(original, "m.yaml", edits)
		if err != nil {
			if strings.Contains(err.Error(), "internal error") {
				t.Fatalf("case %d: %v\nedits %q", n, err, edits)
			}
			continue // a later edit's text was made ambiguous by an earlier one
		}
		checked++
		if back, err := Apply(original, diff); err != nil || back != patched {
			t.Fatalf("case %d: diff does not reproduce the edits: %v\n%s", n, err, diff)
		}
	}
	if checked < 100 {
		t.Fatalf("only %d cases checked", checked)
	}
}

// Edits are judged like diffs were: a fix that runs as root is blocked.
func TestEditsAreCheckedByRules(t *testing.T) {
	orig := "services:\n  web:\n    image: app:1.0\n    user: \"1000\""
	patched, diff, err := Patch(orig, "docker-compose.yml", []model.Edit{ed(`    user: "1000"`, `    user: "root"`)})
	if err != nil || !strings.Contains(diff, `+    user: "root"`) {
		t.Fatalf("patch: %v\n%s", err, diff)
	}
	if v := Verdict(Check(orig, patched)); v != model.VerdictBlock {
		t.Fatalf("verdict %s", v)
	}
}
