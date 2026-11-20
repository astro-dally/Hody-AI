package pipeline

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestLayering walks the import graph from this package (non-test files,
// transitively through every hody/ package) and fails if it reaches a
// platform client. The pipeline must reach tools only through
// internal/tools, which returns gateway evidence.
func TestLayering(t *testing.T) {
	forbidden := []string{"hody/internal/tools/docker", "hody/internal/tools/k8s", "k8s.io/"}
	root := "../.."
	seen := map[string]bool{}
	var visit func(pkg string, path []string)
	visit = func(pkg string, path []string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		dir := filepath.Join(root, strings.TrimPrefix(pkg, "hody"))
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no Go files for %s in %s", pkg, dir)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range src.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				for _, bad := range forbidden {
					if p == bad || strings.HasPrefix(p, bad+"/") || (strings.HasSuffix(bad, "/") && strings.HasPrefix(p, bad)) {
						t.Errorf("pipeline reaches %s via %s -> %s", p, strings.Join(path, " -> "), filepath.Base(f))
					}
				}
				if strings.HasPrefix(p, "hody/") {
					visit(p, append(path, p))
				}
			}
		}
	}
	visit("hody/internal/pipeline", []string{"hody/internal/pipeline"})
	if !seen["hody/internal/tools"] || !seen["hody/internal/gateway"] {
		t.Fatal("walk did not reach internal/tools and internal/gateway; the test is broken")
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal("module root not found")
	}
}
