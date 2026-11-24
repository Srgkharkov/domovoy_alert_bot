package tuya

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testID, testSecret = "cid", "secret"

// fakeTuya verifies request signatures and serves a token and one home.
type fakeTuya struct {
	t          *testing.T
	tokens     int  // tokens issued
	failNext   bool // answer 1010 (token invalid) once
	lastTarget string
}

func (f *fakeTuya) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	f.lastTarget = target
	tok := r.Header.Get("access_token")
	sum := sha256.Sum256(nil)
	sts := strings.Join([]string{r.Method, hex.EncodeToString(sum[:]), "", target}, "\n")
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(testID + tok + r.Header.Get("t") + r.Header.Get("nonce") + sts))
	if want := strings.ToUpper(hex.EncodeToString(mac.Sum(nil))); r.Header.Get("sign") != want {
		f.t.Errorf("bad sign for %s", target)
	}

	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.URL.Path == "/v1.0/token":
		f.tokens++
		reply(map[string]any{"success": true, "result": map[string]any{"access_token": "tok", "expire_time": 7200}})
	case f.failNext:
		f.failNext = false
		reply(map[string]any{"success": false, "code": 1010, "msg": "token invalid"})
	case r.URL.Path == "/v1.0/homes/42" && tok == "tok":
		reply(map[string]any{"success": true, "result": map[string]any{"home_id": 42, "name": "Викендица"}})
	default:
		reply(map[string]any{"success": false, "code": 1106, "msg": "permission deny"})
	}
}

func newTestClient(t *testing.T) (*Client, *fakeTuya) {
	f := &fakeTuya{t: t}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return NewClient(Options{BaseURL: srv.URL, AccessID: testID, Secret: testSecret}), f
}

func TestHomeSignedAndTokenCached(t *testing.T) {
	c, f := newTestClient(t)
	for range 2 {
		h, err := c.Home(context.Background(), 42)
		if err != nil {
			t.Fatal(err)
		}
		if h.Name != "Викендица" {
			t.Fatalf("name = %q", h.Name)
		}
	}
	if f.tokens != 1 {
		t.Errorf("tokens issued = %d, want 1 (cached)", f.tokens)
	}
}

func TestTokenInvalidRetried(t *testing.T) {
	c, f := newTestClient(t)
	if _, err := c.Home(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	f.failNext = true
	if _, err := c.Home(context.Background(), 42); err != nil {
		t.Fatalf("expected retry with a new token, got %v", err)
	}
	if f.tokens != 2 {
		t.Errorf("tokens issued = %d, want 2", f.tokens)
	}
}

func TestAPIError(t *testing.T) {
	c, _ := newTestClient(t)
	_, err := c.Home(context.Background(), 7)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != 1106 {
		t.Fatalf("err = %v", err)
	}
}

func TestSortedQuery(t *testing.T) {
	c, f := newTestClient(t)
	_ = c.get(context.Background(), "/v1.0/x", map[string][]string{"b": {"2"}, "a": {"1"}}, nil)
	if f.lastTarget != "/v1.0/x?a=1&b=2" {
		t.Errorf("target = %q", f.lastTarget)
	}
}

func TestScaled(t *testing.T) {
	for _, tt := range []struct {
		raw   any
		scale int
		want  float64
		ok    bool
	}{
		{float64(317428), 2, 3174.28, true},
		{float64(-15), 1, -1.5, true},
		{"121", 1, 12.1, true},
		{true, 0, 0, false},
	} {
		got, ok := Scaled(tt.raw, tt.scale)
		if ok != tt.ok || (ok && (got-tt.want > 1e-9 || tt.want-got > 1e-9)) {
			t.Errorf("Scaled(%v, %d) = %v, %v", tt.raw, tt.scale, got, ok)
		}
	}
}
