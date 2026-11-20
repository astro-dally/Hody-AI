package gateway

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
)

func TestIsSecretKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"DB_PASSWORD", true}, {"password", true}, {"dbPasswd", true}, {"MYSQL_ROOT_PASS", true},
		{"client_secret", true}, {"GITHUB_TOKEN", true}, {"apiKey", true}, {"api_key", true},
		{"SIGNING_KEY", true}, {"AWS_SECRET_ACCESS_KEY", true}, {"credentials", true},
		{"BASIC_AUTH", true}, {"Authorization", true}, {"session_cookie", true},
		{"key", false}, {"secretName", false}, {"secretKeyRef", false}, {"passwordFile", false},
		{"tokenPath", false}, {"keyId", false}, {"PWD", false}, {"monkey", false},
		{"image", false}, {"KeyError", false}, {"TokenExpiredException", false}, {"LOG_LEVEL", false}, {"AUTH_URL", false}, {"name", false},
	}
	for _, c := range cases {
		if got := isSecretKey(c.key); got != c.want {
			t.Errorf("isSecretKey(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestRedactByKeyName(t *testing.T) {
	cases := []struct {
		name, in, secret string
	}{
		{"env", "DB_PASSWORD=hunter2hunter2\nLOG=info", "hunter2hunter2"},
		{"yaml", "  password: s3cr3t-value\n", "s3cr3t-value"},
		{"json", `{"api_key": "abcd-efgh-1234"}`, "abcd-efgh-1234"},
		{"docker env array", `"Env": ["PATH=/bin", "GITHUB_TOKEN=tok-value-99"]`, "tok-value-99"},
		{"query string", "GET /cb?access_token=qwerty12345&x=1", "qwerty12345"},
		{"cli flag", "args: --db-password=pa55word9", "pa55word9"},
		{"k8s yaml env", "- name: DB_PASSWORD\n  value: \"multi line secret\"\n", "multi line secret"},
		{"k8s yaml env unquoted", "      - name: API_TOKEN\n        value: plain-token-xyz\n", "plain-token-xyz"},
		{"k8s json env", `{"name":"SMTP_PASSWORD","value":"mailpass77"}`, "mailpass77"},
		{"single quotes", "secret='with space inside'", "with space inside"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := New()
			out, n := g.redact(c.in)
			if strings.Contains(out, c.secret) || n == 0 {
				t.Fatalf("secret survived: %q (n=%d)", out, n)
			}
			if !strings.Contains(out, "[SECRET_1]") {
				t.Fatalf("no placeholder in %q", out)
			}
		})
	}
}

func TestRedactKeepsHarmlessValues(t *testing.T) {
	cases := []string{
		"configMapKeyRef:\n  key: database_url\n  name: app-config",
		"secretKeyRef:\n  name: db-creds\n  key: password",
		"PWD=/app\nHOME=/root",
		"Authorization: Bearer [SECRET_4]",
		"password: true",
		"token: ${API_TOKEN}",
		"image: nginx@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"commit 3f2a9c1d8e7b6a5f4e3d2c1b0a9f8e7d6c5b4a39",
		"request id 123e4567-e89b-12d3-a456-426614174000",
		"pod api-7d9f8b6c5d-x2k9p restarted",
		"container 4b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c",
		"ConnectionRefusedError2024 while dialing",
		"/usr/local/lib/python3.12/site-packages/requests",
		"KeyError: 'DB_HOST'",
		"owner: ReplicaSet/orders-5c9d8f7b6",
		"BackOff Pod/api-new-7d9f8b6c5d-x2k9p restarting",
	}
	for _, in := range cases {
		g := New()
		if out, n := g.redact(in); n != 0 || out != in {
			t.Errorf("redacted harmless text %q -> %q", in, out)
		}
	}
}

func TestRedactByPattern(t *testing.T) {
	cases := []struct{ name, in, secret string }{
		{"aws", "using AKIAIOSFODNN7EXAMPLE now", "AKIAIOSFODNN7EXAMPLE"},
		{"github", "clone with ghp_abcdefghijklmnopqrstuvwxyz0123456789", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
		{"github pat", "github_pat_11ABCDEFG0abcdefghijklmn_opqrstu", "github_pat_11ABCDEFG0abcdefghijklmn_opqrstu"},
		{"jwt", "cookie eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"},
		{"bearer", "sent header: bearer abc.def-ghi_jkl", "abc.def-ghi_jkl"},
		{"basic", "Basic dXNlcjpwYXNzd29yZA==", "dXNlcjpwYXNzd29yZA=="},
		{"url creds", "dsn postgres://app:pg-pa55@db:5432/shop", "pg-pa55"},
		{"slack", "xoxb-1234567890-abcdefghij", "xoxb-1234567890-abcdefghij"},
		{"private key", "x\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA1\nabc\n-----END RSA PRIVATE KEY-----\ny", "MIIEpAIBAAKCAQEA1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, _ := New().redact(c.in)
			if strings.Contains(out, c.secret) {
				t.Fatalf("secret survived: %q", out)
			}
		})
	}
}

func TestRedactByEntropy(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	missed := 0
	for i := 0; i < 200; i++ {
		n := 24 + rng.Intn(40)
		b := make([]byte, n)
		for j := range b {
			b[j] = alphabet[rng.Intn(len(alphabet))]
		}
		tok := string(b)
		if !mixedClasses(tok) {
			continue
		}
		out, _ := New().redact("value " + tok + " end")
		if strings.Contains(out, tok) {
			missed++
		}
	}
	// Documented: the threshold is tuned to catch random base64 almost
	// always; a small miss rate on short tokens is accepted.
	if missed > 4 {
		t.Fatalf("entropy layer missed %d of 200 random tokens", missed)
	}
}

func TestPlaceholderStability(t *testing.T) {
	g := New()
	a, _ := g.redact("PASSWORD=alpha-secret-1")
	b, _ := g.redact("TOKEN=beta-secret-2\nlog: connecting with alpha-secret-1")
	c, _ := g.redact("again PASSWORD=alpha-secret-1")
	if a != "PASSWORD=[SECRET_1]" {
		t.Fatalf("first: %q", a)
	}
	if b != "TOKEN=[SECRET_2]\nlog: connecting with [SECRET_1]" {
		t.Fatalf("second: %q", b)
	}
	if c != "again PASSWORD=[SECRET_1]" {
		t.Fatalf("third: %q", c)
	}
}

func TestGatewayNeverSerialisesSecrets(t *testing.T) {
	g := New()
	g.redact("PASSWORD=do-not-serialise-me")
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "do-not-serialise-me") {
		t.Fatalf("gateway JSON contains a secret: %s", data)
	}
}

func TestRedactionBeforeLimit(t *testing.T) {
	// A private key block in the middle of a long log would be cut by the
	// limiter. Redaction runs first, so no fragment of it may survive.
	var lines []string
	for i := 0; i < 400; i++ {
		lines = append(lines, "info line")
	}
	key := []string{"-----BEGIN PRIVATE KEY-----"}
	for i := 0; i < 30; i++ {
		key = append(key, "error KEYMATERIALLINE"+strings.Repeat("Q", i))
	}
	key = append(key, "-----END PRIVATE KEY-----")
	lines = append(lines[:200], append(key, lines[200:]...)...)
	ev := New().Process("t", nil, strings.Join(lines, "\n"))
	if strings.Contains(ev.Text, "KEYMATERIAL") || strings.Contains(ev.Block, "KEYMATERIAL") {
		t.Fatal("private key material survived")
	}
	if ev.Omitted == 0 {
		t.Fatal("expected the limiter to drop lines")
	}
}
