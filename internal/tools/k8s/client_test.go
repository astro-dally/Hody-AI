package k8s

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestServerVersionReadsOnly(t *testing.T) {
	cs := fake.NewSimpleClientset()
	v, err := NewForClientset(cs).ServerVersion(context.Background())
	if err != nil || v == "" {
		t.Fatalf("version %q err %v", v, err)
	}
	for _, a := range cs.Actions() {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			t.Errorf("ServerVersion made a %s call", a.GetVerb())
		}
	}
}
