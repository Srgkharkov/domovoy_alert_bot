// Package aqara is a minimal client for the Aqara Open API v3
// (https://developer.aqara.com).
//
// Every call is a POST to /v3.0/open/api with a body {"intent": "...", "data": {...}}
// and signed headers (see sign()).
package aqara

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const apiPath = "/v3.0/open/api"

// refreshBefore: the access token is refreshed when it expires sooner than this.
const refreshBefore = 24 * time.Hour

// Options configures the client.
type Options struct {
	APIURL string // regional endpoint, e.g. https://open-ger.aqara.com
	AppID  string
	AppKey string
	KeyID  string
}

// Client talks to the Aqara Open API.
type Client struct {
	opts  Options
	http  *http.Client
	store TokenStore

	mu    sync.Mutex
	token *Token
	now   func() time.Time
}

// NewClient creates a client. store keeps the OAuth tokens between restarts.
func NewClient(opts Options, store TokenStore) *Client {
	return &Client{
		opts:  opts,
		http:  &http.Client{Timeout: 15 * time.Second},
		store: store,
		now:   time.Now,
	}
}

// APIError is a non-zero "code" in an Aqara response.
type APIError struct {
	Intent  string
	Code    int
	Message string
	Details string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aqara %s: code %d: %s %s", e.Intent, e.Code, e.Message, e.Details)
}

type request struct {
	Intent string `json:"intent"`
	Data   any    `json:"data"`
}

type response struct {
	Code       int             `json:"code"`
	RequestID  string          `json:"requestId"`
	Message    string          `json:"message"`
	MsgDetails any             `json:"msgDetails"`
	Result     json.RawMessage `json:"result"`
}

// call performs an authorized API call (with access token).
func (c *Client) call(ctx context.Context, intent string, data, out any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, tok, intent, data, out)
}

// do performs a raw API call. accessToken may be empty for auth intents.
func (c *Client) do(ctx context.Context, accessToken, intent string, data, out any) error {
	body, err := json.Marshal(request{Intent: intent, Data: data})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.opts.APIURL, "/")+apiPath, bytes.NewReader(body))
	if err != nil {
		return err
	}

	nonce := randomNonce()
	ts := strconv.FormatInt(c.now().UnixMilli(), 10)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Appid", c.opts.AppID)
	req.Header.Set("Keyid", c.opts.KeyID)
	req.Header.Set("Nonce", nonce)
	req.Header.Set("Time", ts)
	req.Header.Set("Lang", "en")
	if accessToken != "" {
		req.Header.Set("Accesstoken", accessToken)
	}
	req.Header.Set("Sign", sign(accessToken, c.opts.AppID, c.opts.KeyID, nonce, ts, c.opts.AppKey))

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("aqara %s: %w", intent, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("aqara %s: read body: %w", intent, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("aqara %s: http %d: %s", intent, resp.StatusCode, raw)
	}

	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("aqara %s: decode: %w", intent, err)
	}
	if r.Code != 0 {
		details := ""
		if r.MsgDetails != nil {
			details = fmt.Sprint(r.MsgDetails)
		}
		return &APIError{Intent: intent, Code: r.Code, Message: r.Message, Details: details}
	}
	if out != nil && len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("aqara %s: decode result: %w", intent, err)
		}
	}
	return nil
}

// sign builds the Aqara request signature:
//
//	MD5( lowercase( "Accesstoken=..&Appid=..&Keyid=..&Nonce=..&Time=.." + AppKey ) )
//
// The Accesstoken part is omitted when there is no token (auth intents).
func sign(accessToken, appID, keyID, nonce, ts, appKey string) string {
	var b strings.Builder
	if accessToken != "" {
		b.WriteString("Accesstoken=" + accessToken + "&")
	}
	b.WriteString("Appid=" + appID + "&Keyid=" + keyID + "&Nonce=" + nonce + "&Time=" + ts)
	b.WriteString(appKey)
	sum := md5.Sum([]byte(strings.ToLower(b.String())))
	return hex.EncodeToString(sum[:])
}

func randomNonce() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
