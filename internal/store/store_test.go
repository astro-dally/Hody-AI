package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hody/internal/model"
)

func TestSaveLoadListAudit(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &model.Session{ID: "0123456789abcdef", Status: model.StatusComplete}
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load(s.ID)
	if err != nil || got.Status != model.StatusComplete {
		t.Fatalf("load %+v %v", got, err)
	}
	ids, _ := st.List()
	if len(ids) != 1 || ids[0] != s.ID {
		t.Fatalf("list %v", ids)
	}
	st.Audit(s.ID, "step", map[string]string{"step": "collect"})
	st.Audit(s.ID, "step", map[string]string{"step": "triage"})
	data, _ := os.ReadFile(filepath.Join(st.Dir(), "audit.jsonl"))
	if strings.Count(string(data), "\n") != 2 {
		t.Fatalf("audit log: %s", data)
	}
	info, _ := os.Stat(filepath.Join(st.Dir(), "sessions", s.ID+".json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session file mode %v", info.Mode().Perm())
	}
}

func TestRejectsBadIDs(t *testing.T) {
	st, _ := Open(t.TempDir())
	for _, id := range []string{"", "../../etc/passwd", "ABCDEF0123456789", "short"} {
		if err := st.Save(&model.Session{ID: id}); err == nil {
			t.Errorf("saved id %q", id)
		}
		if _, err := st.Load(id); err == nil {
			t.Errorf("loaded id %q", id)
		}
	}
}
