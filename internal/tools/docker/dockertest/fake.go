// Package dockertest provides a fake Docker Engine API built on
// net/http/httptest for tests. It records every request it receives.
package dockertest

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"sync"
)

// FakeAPI is a fake Docker Engine API that records every request.
type FakeAPI struct {
	*httptest.Server
	mu      sync.Mutex
	Methods []string
	Paths   []string
}

// InspectJSON is the fake inspect body for container "web".
const InspectJSON = `{"Id":"4b1c2d3e4f5a6b7c8d9e0f1a","Name":"/web","RestartCount":3,
"State":{"Status":"exited","OOMKilled":true,"ExitCode":137,"StartedAt":"t1","FinishedAt":"t2"},
"Config":{"Image":"shop/web:1.4","User":"","Env":["PATH=/usr/bin","DB_PASSWORD=fake-db-pass-123"],"Cmd":["node","server.js"],
"ExposedPorts":{"8080/tcp":{}}},
"HostConfig":{"Privileged":false,"NetworkMode":"bridge","Memory":67108864,"Binds":["/srv/data:/data"],
"PortBindings":{"8080/tcp":[{"HostIp":"","HostPort":"8080"}]},"RestartPolicy":{"Name":"always"}},
"Mounts":[{"Type":"bind","Source":"/srv/data","Destination":"/data","RW":true}]}`

// Frame builds one multiplexed log frame.
func Frame(stream byte, s string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(s)))
	return append(h, s...)
}

// NewFakeAPI starts the fake.
func NewFakeAPI() *FakeAPI {
	f := &FakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.Methods = append(f.Methods, r.Method)
		f.Paths = append(f.Paths, r.URL.Path)
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/containers/json":
			w.Write([]byte(`[{"Id":"4b1c2d3e4f5a","Names":["/web"],"Image":"shop/web:1.4","State":"exited","Status":"Exited (137)"}]`))
		case r.URL.Path == "/containers/web/json":
			w.Write([]byte(InspectJSON))
		case r.URL.Path == "/containers/web/logs":
			w.Write(append(Frame(1, "starting\n"), Frame(2, "Killed\n")...))
		case r.URL.Path == "/events":
			w.Write([]byte(`{"Type":"container","Action":"oom","time":1700000000,"Actor":{"Attributes":{"name":"web"}}}` + "\n" +
				`{"Type":"container","Action":"die","time":1700000001,"Actor":{"Attributes":{"exitCode":"137","name":"web"}}}` + "\n"))
		case r.URL.Path == "/images/shop/web:1.4/json":
			w.Write([]byte(`{"Id":"sha256:abc","RepoTags":["shop/web:1.4"],"Config":{"User":"node","Cmd":["node","server.js"]}}`))
		default:
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		}
	}))
	return f
}
