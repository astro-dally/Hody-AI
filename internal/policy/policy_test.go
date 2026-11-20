package policy

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"hody/internal/model"
)

func TestUnsafeFixes(t *testing.T) {
	data, err := os.ReadFile("../../eval/testdata/unsafe_fixes.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Original, Diff, Rule, Verdict string }
	if err := json.Unmarshal(data, &cases); err != nil || len(cases) < 15 {
		t.Fatalf("test data: %v (%d cases)", err, len(cases))
	}
	covered := map[string]bool{}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			patched, err := Apply(c.Original, c.Diff)
			if err != nil {
				t.Fatalf("diff does not apply: %v", err)
			}
			f := Check(c.Original, patched)
			if got := Verdict(f); got != c.Verdict {
				t.Fatalf("verdict %s want %s (findings %+v)", got, c.Verdict, f)
			}
			if c.Rule != "" && (len(f) == 0 || f[0].Rule != c.Rule) {
				t.Fatalf("findings %+v, want rule %s", f, c.Rule)
			}
			for _, x := range f {
				covered[x.Rule] = true
				if x.Risk == "" || x.Alternative == "" {
					t.Errorf("rule %s lacks risk or alternative", x.Rule)
				}
			}
		})
	}
	for _, r := range Rules {
		if !covered[r.Name] {
			t.Errorf("no unsafe-fix case exercises rule %s", r.Name)
		}
	}
}

func TestExistingViolationIsNotBlamedOnPatch(t *testing.T) {
	orig := "privileged: true\nimage: app:1.0\n"
	patched, err := Apply(orig, "--- a/m\n+++ b/m\n@@ -2,1 +2,1 @@\n-image: app:1.0\n+image: app:1.1\n")
	if err != nil {
		t.Fatal(err)
	}
	if f := Check(orig, patched); len(f) != 0 {
		t.Fatalf("findings %+v", f)
	}
}

func TestApply(t *testing.T) {
	text := "a\nb\nc\nd\ne\n"
	cases := []struct {
		name, diff, want, err string
	}{
		{"replace", "--- a/f\n+++ b/f\n@@ -2,1 +2,1 @@\n-b\n+B\n", "a\nB\nc\nd\ne\n", ""},
		{"wrong line number tolerated", "--- a/f\n+++ b/f\n@@ -9,2 +9,2 @@\n c\n-d\n+D\n", "a\nb\nc\nD\ne\n", ""},
		{"insert only", "--- a/f\n+++ b/f\n@@ -1,0 +2,1 @@\n+x\n", "a\nx\nb\nc\nd\ne\n", ""},
		{"two hunks", "--- a/f\n+++ b/f\n@@ -1,1 +1,1 @@\n-a\n+A\n@@ -5,1 +5,1 @@\n-e\n+E\n", "A\nb\nc\nd\nE\n", ""},
		{"blank context without space", "--- a/f\n+++ b/f\n@@ -5,2 +5,2 @@\n-e\n+E\n\n", "a\nb\nc\nd\nE\n", ""},
		{"removed line starting with dashes", "--- a/f\n+++ b/f\n@@ -1,1 +1,1 @@\n-a\n+-- a\n", "-- a\nb\nc\nd\ne\n", ""},
		{"content mismatch", "--- a/f\n+++ b/f\n@@ -2,1 +2,1 @@\n-z\n+Z\n", "", "does not match"},
		{"no hunks", "just text", "", "no hunks"},
		{"two files", "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+A\n--- a/g\n+++ b/g\n@@ -1 +1 @@\n-x\n+y\n", "", "more than one file"},
		{"bad line", "--- a/f\n+++ b/f\n@@ -1,1 +1,1 @@\n*a\n", "", "unexpected"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Apply(text, c.diff)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err=%v want %q", err, c.err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %q err %v, want %q", got, err, c.want)
			}
		})
	}
}

func TestStricter(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{model.VerdictApprove, model.VerdictWarn, model.VerdictWarn},
		{model.VerdictBlock, model.VerdictApprove, model.VerdictBlock},
		{model.VerdictWarn, model.VerdictWarn, model.VerdictWarn},
		{model.VerdictApprove, "maybe", "maybe"},
	}
	for _, c := range cases {
		if got := Stricter(c.a, c.b); got != c.want {
			t.Errorf("Stricter(%s,%s)=%s", c.a, c.b, got)
		}
	}
}
