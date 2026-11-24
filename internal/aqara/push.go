package aqara

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

// ResourceReport is one attribute change from an HTTP push message
// (msgType "resource_report"), e.g. 3.1.85 = "1" for a water leak.
type ResourceReport struct {
	SubjectID  string `json:"subjectId"`
	ResourceID string `json:"resourceId"`
	Value      string `json:"value"`
	Time       string `json:"time"` // ms
	StatusCode int    `json:"statusCode"`
}

// pushEnvelope covers both push kinds: attribute messages carry msgType,
// event messages (gateway_online, subdevice_offline, ...) carry eventType.
type pushEnvelope struct {
	MsgID     string          `json:"msgId"`
	OpenID    string          `json:"openId"`
	MsgType   string          `json:"msgType"`
	EventType string          `json:"eventType"`
	Data      json.RawMessage `json:"data"`
}

// PushHandler receives Aqara "Message push" HTTP callbacks.
//
// Aqara expects {"code":0} for every request, including the console's
// "Verify" probe. Any other answer counts as a failure, and the push service
// is suspended when failures exceed 5% in 5 minutes. So the handler always
// acknowledges, and parsing problems are only logged.
//
// Authentication: the handler must be mounted on a secret URL path. The
// `token` header (the account's access token) is logged on mismatch but not
// enforced, because a token refresh race must never drop a leak alert.
func PushHandler(c *Client, onReports func([]ResourceReport), log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0}`))
		}()

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil || len(body) == 0 {
			log.Info("aqara push: empty request (verify probe?)", "method", r.Method)
			return
		}

		if tok := r.Header.Get("token"); tok != "" && !c.IsCurrentToken(tok) {
			log.Warn("aqara push: token header does not match stored access token")
		}

		var env pushEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			log.Warn("aqara push: bad json", "err", err)
			return
		}

		switch {
		case env.MsgType == "resource_report":
			var reports []ResourceReport
			if err := json.Unmarshal(env.Data, &reports); err != nil {
				log.Warn("aqara push: bad resource_report data", "msg_id", env.MsgID, "err", err)
				return
			}
			log.Debug("aqara push", "msg_id", env.MsgID, "reports", len(reports))
			onReports(reports)
		case env.EventType != "":
			log.Info("aqara push event", "type", env.EventType, "msg_id", env.MsgID)
		default:
			log.Debug("aqara push: unhandled message", "msgType", env.MsgType)
		}
	})
}

// IsCurrentToken reports whether tok is the access token we currently hold.
func (c *Client) IsCurrentToken(tok string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token != nil && c.token.AccessToken == tok
}

// Subscribe enables push messages for the given device attributes
// (config.resource.subscribe). Required in the console's "User-defined
// subscription mode"; in "All receiving mode" Aqara rejects this call,
// which is harmless because everything is pushed anyway.
func (c *Client) Subscribe(ctx context.Context, queries []ResourceQuery) error {
	return c.call(ctx, "config.resource.subscribe", map[string]any{"resources": queries}, nil)
}
