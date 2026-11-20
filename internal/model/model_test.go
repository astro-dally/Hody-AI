package model

import "testing"

func TestFindEvidenceAndClone(t *testing.T) {
	s := &Session{Evidence: []Evidence{{ID: "E1"}, {ID: "E2"}}, RootCause: &RootCause{Summary: "a"}}
	if s.FindEvidence("E2") == nil || s.FindEvidence("E9") != nil {
		t.Fatal("FindEvidence wrong")
	}
	c := s.Clone()
	c.Evidence[0].ID = "X"
	c.RootCause.Summary = "b"
	if s.Evidence[0].ID != "E1" || s.RootCause.Summary != "a" {
		t.Fatal("Clone shares memory with the original")
	}
}

func TestModelRequests(t *testing.T) {
	s := &Session{ToolCalls: []ToolCall{{By: "collect"}, {By: "model"}, {By: "model", Accepted: false}}}
	if got := s.ModelRequests(); got != 2 {
		t.Fatalf("got %d", got)
	}
}

func TestTargetString(t *testing.T) {
	cases := []struct {
		t    Target
		want string
	}{
		{Target{Platform: PlatformDocker, Container: "web"}, "docker web"},
		{Target{Platform: PlatformK8s, Pod: "api", Namespace: "shop"}, "k8s pod/api -n shop"},
	}
	for _, c := range cases {
		if got := c.t.String(); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}
