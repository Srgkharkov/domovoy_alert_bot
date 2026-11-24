package aqara

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestSignIsLowercasedMD5(t *testing.T) {
	// Same input with different case must produce the same signature.
	a := sign("TOKEN", "APP", "KEY", "N", "1", "SECRET")
	b := sign("token", "app", "key", "n", "1", "secret")
	if a != b || len(a) != 32 {
		t.Fatalf("unexpected signatures %q %q", a, b)
	}
	if sign("", "app", "key", "n", "1", "secret") == b {
		t.Fatal("signature without token must differ")
	}
}

func TestCallRefreshesExpiredToken(t *testing.T) {
	var intents []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req request
		_ = json.Unmarshal(body, &req)
		intents = append(intents, req.Intent)

		if r.Header.Get("Sign") == "" || r.Header.Get("Appid") != "app" {
			t.Errorf("missing auth headers: %v", r.Header)
		}

		switch req.Intent {
		case "config.auth.refreshToken":
			_, _ = w.Write([]byte(`{"code":0,"result":{"expiresIn":"604800","accessToken":"new","refreshToken":"r2","openId":"o"}}`))
		case "query.device.info":
			if got := r.Header.Get("Accesstoken"); got != "new" {
				t.Errorf("Accesstoken = %q, want new", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"result":{"data":[{"did":"lumi.1","deviceName":"Lamp","state":1}],"totalCount":1}}`))
		default:
			_, _ = w.Write([]byte(`{"code":100,"message":"unknown intent"}`))
		}
	}))
	defer srv.Close()

	store := NewFileTokenStore(filepath.Join(t.TempDir(), "token.json"))
	if err := store.Save(&Token{AccessToken: "old", RefreshToken: "r1", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	c := NewClient(Options{APIURL: srv.URL, AppID: "app", AppKey: "secret", KeyID: "key"}, store)
	devices, err := c.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DeviceName != "Lamp" || !devices[0].Online() {
		t.Fatalf("unexpected devices: %+v", devices)
	}
	if len(intents) != 2 || intents[0] != "config.auth.refreshToken" {
		t.Fatalf("intents = %v", intents)
	}

	saved, err := store.Load()
	if err != nil || saved.AccessToken != "new" {
		t.Fatalf("token not persisted: %+v, %v", saved, err)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":302,"message":"Missing parameter"}`))
	}))
	defer srv.Close()

	c := NewClient(Options{APIURL: srv.URL}, NewFileTokenStore(filepath.Join(t.TempDir(), "t.json")))
	err := c.RequestAuthCode(context.Background(), "me@example.com", "1y")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != 302 {
		t.Fatalf("expected APIError 302, got %v", err)
	}
}
