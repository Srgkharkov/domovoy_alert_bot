package aqara

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushHandler(t *testing.T) {
	c := NewClient(Options{}, NewFileTokenStore(filepath.Join(t.TempDir(), "t.json")))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	var got []ResourceReport
	h := PushHandler(c, func(r []ResourceReport) { got = append(got, r...) }, log)

	tests := []struct {
		name, body string
		reports    int
	}{
		{"verify probe (empty)", "", 0},
		{"garbage", "not json", 0},
		{"resource report", `{"msgId":"m1","openId":"o","time":"1","msgType":"resource_report",
			"data":[{"subjectId":"lumi.1","resourceId":"3.1.85","value":"1","time":"1","statusCode":0}]}`, 1},
		{"event", `{"msgId":"m2","eventType":"gateway_online","data":{}}`, 0},
	}
	for _, tt := range tests {
		got = nil
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body)))

		// Aqara requires {"code":0} for every request, otherwise push gets suspended.
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"code":0}` {
			t.Errorf("%s: response %d %q", tt.name, rec.Code, rec.Body.String())
		}
		if len(got) != tt.reports {
			t.Errorf("%s: got %d reports, want %d", tt.name, len(got), tt.reports)
		}
	}
}
