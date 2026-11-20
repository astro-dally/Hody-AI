// Package web serves Hody's local web page with the standard library. The
// templates are embedded in the binary and use html/template, so all model
// output and log text is escaped plain text; nothing from a diagnosis is
// ever rendered as a link or image. Requests whose Host or Origin is not
// this loopback address are rejected, and the one state-changing request
// (starting a diagnosis) needs a per-server CSRF token.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"html/template"
	"net"
	"net/http"
	"regexp"
	"sort"
	"sync"

	"hody/internal/model"
	"hody/internal/pipeline"
	"hody/internal/store"
)

//go:embed templates/*.html static/style.css
var files embed.FS

// Runner runs one diagnosis, calling notify with snapshots as it goes.
type Runner func(ctx context.Context, s *model.Session, notify func(model.Session)) error

// Server is the web UI for one local process.
type Server struct {
	addr     string // host:port this server listens on
	csrf     string
	run      Runner
	store    *store.Store
	tmpl     *template.Template
	ctx      context.Context
	mu       sync.Mutex
	sessions map[string]model.Session
}

var (
	containerRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	k8sNameRE   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
)

// New prepares a server for addr (which must be the loopback address it
// will listen on). ctx bounds the diagnoses it starts.
func New(ctx context.Context, addr string, st *store.Store, run Runner) (*Server, error) {
	t, err := template.ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Server{addr: addr, csrf: hex.EncodeToString(b), run: run, store: st, tmpl: t, ctx: ctx, sessions: map[string]model.Session{}}, nil
}

// Handler returns the HTTP handler with host checks and security headers.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("POST /diagnose", s.diagnose)
	mux.HandleFunc("GET /s/{id}", s.session)
	mux.Handle("GET /static/", http.FileServerFS(files))
	return s.guard(mux)
}

// guard rejects non-local Host and Origin values and sets headers.
func (s *Server) guard(next http.Handler) http.Handler {
	_, port, _ := net.SplitHostPort(s.addr)
	hosts := map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true, "[::1]:" + port: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			http.Error(w, "Hody only answers requests addressed to "+s.addr, http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !hosts[trimScheme(o)] {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; img-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func trimScheme(origin string) string {
	for _, p := range []string{"http://", "https://"} {
		if len(origin) > len(p) && origin[:len(p)] == p {
			return origin[len(p):]
		}
	}
	return "\x00" // anything else never matches
}

type page struct {
	Title, CSRF, Error string
	Refresh            bool
	Recent             []model.Session
	S                  *model.Session
	Steps              []stepView
	Cited              []model.Evidence
	Flags              int
}

type stepView struct{ Name, Class string }

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "index.html", page{Title: "Start", CSRF: s.csrf, Recent: s.recent()})
}

func (s *Server) recent() []model.Session {
	s.mu.Lock()
	out := make([]model.Session, 0, len(s.sessions))
	seen := map[string]bool{}
	for id, sess := range s.sessions {
		out, seen[id] = append(out, sess), true
	}
	s.mu.Unlock()
	if ids, err := s.store.List(); err == nil {
		for _, id := range ids {
			if len(out) >= 20 {
				break
			}
			if sess, err := s.store.Load(id); err == nil && !seen[id] {
				out = append(out, *sess)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

func (s *Server) diagnose(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(s.csrf)) != 1 {
		http.Error(w, "missing or wrong form token; reload the page and try again", http.StatusForbidden)
		return
	}
	t, msg := targetFromForm(r)
	if msg != "" {
		s.render(w, http.StatusBadRequest, "index.html", page{Title: "Start", CSRF: s.csrf, Error: msg, Recent: s.recent()})
		return
	}
	sess := pipeline.NewSession(t)
	s.update(sess.Clone())
	go func() {
		_ = s.run(s.ctx, sess, s.update)
		s.update(sess.Clone())
	}()
	http.Redirect(w, r, "/s/"+sess.ID, http.StatusSeeOther)
}

func targetFromForm(r *http.Request) (model.Target, string) {
	t := model.Target{Platform: r.PostFormValue("platform"), File: r.PostFormValue("file")}
	name := r.PostFormValue("name")
	switch t.Platform {
	case model.PlatformDocker:
		if !containerRE.MatchString(name) {
			return t, "That is not a valid container name."
		}
		t.Container = name
	case model.PlatformK8s:
		t.Pod, t.Namespace = name, r.PostFormValue("namespace")
		if !k8sNameRE.MatchString(t.Pod) || !k8sNameRE.MatchString(t.Namespace) {
			return t, "Pod and namespace must be lower-case Kubernetes names."
		}
	default:
		return t, "Choose Docker or Kubernetes."
	}
	return t, ""
}

func (s *Server) update(snap model.Session) {
	s.mu.Lock()
	s.sessions[snap.ID] = snap
	s.mu.Unlock()
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		loaded, err := s.store.Load(id)
		if err != nil {
			http.Error(w, "no diagnosis with that id", http.StatusNotFound)
			return
		}
		sess = *loaded
	}
	p := page{Title: sess.Target.String(), S: &sess, Refresh: sess.Status == model.StatusRunning}
	p.Steps = steps(sess)
	if rc := sess.RootCause; rc != nil {
		for _, id := range rc.EvidenceIDs {
			if ev := sess.FindEvidence(id); ev != nil {
				p.Cited = append(p.Cited, *ev)
			}
		}
	}
	for _, ev := range sess.Evidence {
		p.Flags += len(ev.Flags)
	}
	s.render(w, http.StatusOK, "session.html", p)
}

// steps marks each pipeline step as done, current or pending.
func steps(sess model.Session) []stepView {
	names := (&pipeline.Engine{}).Steps()
	out := make([]stepView, len(names))
	state := "done"
	for i, n := range names {
		if n == sess.Step && sess.Status != model.StatusComplete {
			state = "current"
		}
		out[i] = stepView{n, state}
		if state == "current" {
			state = "pending"
		}
	}
	return out
}

func (s *Server) render(w http.ResponseWriter, code int, name string, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := s.tmpl.ExecuteTemplate(w, name, p); err != nil {
		_, _ = w.Write([]byte("template error"))
	}
}
