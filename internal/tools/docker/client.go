// Package docker is a read-only Docker Engine API client that speaks plain
// HTTP over the Unix socket. Its transport refuses every method except GET
// and every path that is not on a fixed allowlist, so no code path through
// this package can change a container or image.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// allowedPaths are the only Docker API paths the client will request.
var allowedPaths = []*regexp.Regexp{
	regexp.MustCompile(`^/containers/json$`),
	regexp.MustCompile(`^/containers/[A-Za-z0-9][A-Za-z0-9_.-]*/json$`),
	regexp.MustCompile(`^/containers/[A-Za-z0-9][A-Za-z0-9_.-]*/logs$`),
	regexp.MustCompile(`^/events$`),
	regexp.MustCompile(`^/images/[A-Za-z0-9][A-Za-z0-9_.:/@-]*/json$`),
}

var versionPrefixRE = regexp.MustCompile(`^/v1\.\d+`)

// readOnly is the http.RoundTripper that enforces GET and the allowlist.
type readOnly struct{ next http.RoundTripper }

// RoundTrip refuses anything that is not a GET to an allowlisted path.
func (r readOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, fmt.Errorf("docker client is read-only: method %s refused", req.Method)
	}
	p := versionPrefixRE.ReplaceAllString(req.URL.Path, "")
	if strings.Contains(p, "..") || !pathAllowed(p) {
		return nil, fmt.Errorf("docker client is read-only: path %q is not on the allowlist", req.URL.Path)
	}
	return r.next.RoundTrip(req)
}

func pathAllowed(p string) bool {
	for _, re := range allowedPaths {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// Client talks to one Docker daemon.
type Client struct {
	http *http.Client
	base string
}

// New returns a client for the daemon listening on the Unix socket path.
func New(socket string) *Client {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socket)
	}
	t := &http.Transport{DialContext: dial, DisableCompression: true}
	return &Client{http: &http.Client{Transport: readOnly{t}, Timeout: 30 * time.Second}, base: "http://docker"}
}

// NewForTest returns a client for an HTTP test server, with the same
// read-only transport as production.
func NewForTest(baseURL string) *Client {
	return &Client{http: &http.Client{Transport: readOnly{http.DefaultTransport}, Timeout: 5 * time.Second}, base: baseURL}
}

// HTTPClient exposes the read-only http.Client so tests can prove that
// non-GET requests are refused by the client itself.
func (c *Client) HTTPClient() *http.Client { return c.http }

// get performs one GET and returns the body, limited to 8 MiB.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct{ Message string }
		_ = json.Unmarshal(body, &e)
		return nil, fmt.Errorf("docker API %s: HTTP %d: %s", path, resp.StatusCode, e.Message)
	}
	return body, nil
}

// ListContainers lists containers (running or not) whose name matches.
func (c *Client) ListContainers(ctx context.Context, name string) ([]Summary, error) {
	f, _ := json.Marshal(map[string][]string{"name": {name}})
	body, err := c.get(ctx, "/containers/json", url.Values{"all": {"1"}, "filters": {string(f)}})
	if err != nil {
		return nil, err
	}
	var out []Summary
	return out, json.Unmarshal(body, &out)
}

// Inspect returns the container's configuration and state.
func (c *Client) Inspect(ctx context.Context, name string) (Container, error) {
	var out Container
	body, err := c.get(ctx, "/containers/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(body, &out)
}

// InspectImage returns an image's metadata.
func (c *Client) InspectImage(ctx context.Context, ref string) (Image, error) {
	var out Image
	body, err := c.get(ctx, "/images/"+ref+"/json", nil)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(body, &out)
}

// Logs returns the last tail lines of stdout and stderr with timestamps.
func (c *Client) Logs(ctx context.Context, name string, tail int) (string, error) {
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {strconv.Itoa(tail)}}
	body, err := c.get(ctx, "/containers/"+url.PathEscape(name)+"/logs", q)
	if err != nil {
		return "", err
	}
	return Demux(body), nil
}

// Events returns engine events for one container over the last window.
func (c *Client) Events(ctx context.Context, name string, window time.Duration) ([]Event, error) {
	now := time.Now()
	f, _ := json.Marshal(map[string][]string{"container": {name}})
	q := url.Values{"since": {strconv.FormatInt(now.Add(-window).Unix(), 10)}, "until": {strconv.FormatInt(now.Unix(), 10)}, "filters": {string(f)}}
	body, err := c.get(ctx, "/events", q)
	if err != nil {
		return nil, err
	}
	var out []Event
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Demux decodes Docker's multiplexed log stream (8-byte frame headers:
// stream type, three zero bytes, big-endian length). A container with a
// TTY sends a raw stream instead, which is returned unchanged.
func Demux(b []byte) string {
	var out bytes.Buffer
	rest := b
	for len(rest) >= 8 {
		if rest[0] > 2 || rest[1] != 0 || rest[2] != 0 || rest[3] != 0 {
			return string(b) // not multiplexed
		}
		n := int(binary.BigEndian.Uint32(rest[4:8]))
		if 8+n > len(rest) {
			return string(b)
		}
		frame := rest[8 : 8+n]
		if rest[0] == 2 && len(frame) > 0 {
			frame = prefixLines(frame, "stderr: ")
		}
		out.Write(frame)
		rest = rest[8+n:]
	}
	if len(rest) != 0 {
		return string(b)
	}
	return out.String()
}

func prefixLines(b []byte, prefix string) []byte {
	lines := strings.SplitAfter(string(b), "\n")
	var out strings.Builder
	for _, l := range lines {
		if l != "" {
			out.WriteString(prefix + l)
		}
	}
	return []byte(out.String())
}
