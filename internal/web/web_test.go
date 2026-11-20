package web

import (
	"context"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"hody/internal/model"
	"hody/internal/store"
)

func newServer(t *testing.T, run Runner) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		run = func(_ context.Context, s *model.Session, notify func(model.Session)) error {
			s.Status = model.StatusComplete
			return nil
		}
	}
	s, err := New(context.Background(), "127.0.0.1:8080", st, run)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(s *Server, method, path, host, origin string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestHostAndOriginChecks(t *testing.T) {
	s := newServer(t, nil)
	ok := url.Values{"csrf": {s.csrf}, "platform": {"docker"}, "name": {"web"}}
	cases := []struct {
		name, method, host, origin string
		form                       url.Values
		code                       int
	}{
		{"local host", "GET", "127.0.0.1:8080", "", nil, 200},
		{"localhost name", "GET", "localhost:8080", "", nil, 200},
		{"DNS rebinding host", "GET", "evil.example:8080", "", nil, 403},
		{"other port", "GET", "127.0.0.1:9999", "", nil, 403},
		{"cross-site post", "POST", "127.0.0.1:8080", "http://evil.example", ok, 403},
		{"null origin", "POST", "127.0.0.1:8080", "null", ok, 403},
		{"post without token", "POST", "127.0.0.1:8080", "http://127.0.0.1:8080", url.Values{"platform": {"docker"}, "name": {"web"}}, 403},
		{"post with wrong token", "POST", "127.0.0.1:8080", "http://127.0.0.1:8080", url.Values{"csrf": {"x"}, "platform": {"docker"}, "name": {"web"}}, 403},
		{"bad container name", "POST", "127.0.0.1:8080", "http://127.0.0.1:8080", url.Values{"csrf": {s.csrf}, "platform": {"docker"}, "name": {"a b;c"}}, 400},
		{"bad platform", "POST", "127.0.0.1:8080", "http://127.0.0.1:8080", url.Values{"csrf": {s.csrf}, "platform": {"vm"}, "name": {"x"}}, 400},
		{"valid post", "POST", "127.0.0.1:8080", "http://127.0.0.1:8080", ok, 303},
	}
	for _, c := range cases {
		path := "/"
		if c.method == "POST" {
			path = "/diagnose"
		}
		if w := do(s, c.method, path, c.host, c.origin, c.form); w.Code != c.code {
			t.Errorf("%s: code %d want %d", c.name, w.Code, c.code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := do(newServer(t, nil), "GET", "/", "127.0.0.1:8080", "", nil)
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "img-src 'none'") || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("headers %v", w.Header())
	}
}

func TestDiagnosisStartsAndCompletes(t *testing.T) {
	started := make(chan model.Target, 1)
	s := newServer(t, func(_ context.Context, sess *model.Session, notify func(model.Session)) error {
		started <- sess.Target
		sess.Status = model.StatusComplete
		return nil
	})
	w := do(s, "POST", "/diagnose", "127.0.0.1:8080", "", url.Values{"csrf": {s.csrf}, "platform": {"k8s"}, "name": {"api-7d9f"}, "namespace": {"shop"}})
	if w.Code != 303 {
		t.Fatalf("code %d", w.Code)
	}
	select {
	case tg := <-started:
		if tg.Pod != "api-7d9f" || tg.Namespace != "shop" {
			t.Fatalf("target %+v", tg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner not started")
	}
	loc := w.Header().Get("Location")
	deadline := time.Now().Add(2 * time.Second)
	for {
		page := do(s, "GET", loc, "127.0.0.1:8080", "", nil).Body.String()
		if !strings.Contains(page, `http-equiv="refresh"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session never completed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHostileTextIsEscaped(t *testing.T) {
	s := newServer(t, nil)
	evil := `<script>alert(1)</script> <a href="http://evil.example/x">click</a> <img src="http://evil.example/p.png"> [see](http://evil.example/md)`
	sess := model.Session{ID: "00112233445566aa", Target: model.Target{Platform: "docker", Container: "web"}, Status: model.StatusComplete,
		Evidence:  []model.Evidence{{ID: "E1", Tool: "docker.logs", Text: evil, Flags: []model.Flag{{Line: 1, Rule: "asks-for-link", Text: evil}}}},
		RootCause: &model.RootCause{Outcome: model.OutcomeRootCause, Summary: evil, Object: evil, EvidenceIDs: []string{"E1"}, NextChecks: []string{evil}},
		Fix:       &model.Fix{EvidenceID: "E1", Diff: evil, Explanation: evil},
		Review:    &model.Review{Verdict: "block", Findings: []model.Finding{{Rule: "r", Severity: "block", Risk: evil, Alternative: evil}}, CriticReason: evil},
		Warnings:  []string{evil}}
	s.update(sess)
	body := do(s, "GET", "/s/"+sess.ID, "127.0.0.1:8080", "", nil).Body.String()
	for _, bad := range []string{"<script", "<img", `href="http://evil`, "<a href=\"http"} {
		if strings.Contains(body, bad) {
			t.Errorf("page contains %q", bad)
		}
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("expected escaped text in the page")
	}
}
