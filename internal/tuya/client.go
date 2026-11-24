// Package tuya is a minimal client for the Tuya Cloud Open API
// (platform.tuya.com, "Smart Home" cloud project with a linked app account).
//
// Every request is signed with HMAC-SHA256:
//
//	sign = HMAC(secret, clientID [+ accessToken] + t + nonce + stringToSign)
//	stringToSign = METHOD \n sha256(body) \n \n /path?sorted=query
//
// The access token (grant_type=1, "simple mode") lives ~2 hours; the client
// fetches a new one when it expires or when the API reports it invalid.
package tuya

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Token error codes after which a fresh token is requested and the call retried.
var tokenErrorCodes = map[int]bool{1010: true, 1011: true, 1012: true}

// Options configure the client.
type Options struct {
	BaseURL  string // regional endpoint, e.g. https://openapi.tuyaeu.com
	AccessID string
	Secret   string
}

// Client talks to the Tuya Cloud API. It is safe for concurrent use.
type Client struct {
	opts Options
	http *http.Client
	now  func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

func NewClient(opts Options) *Client {
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	return &Client{opts: opts, http: &http.Client{Timeout: 15 * time.Second}, now: time.Now}
}

// APIError is a non-success Tuya response.
type APIError struct {
	Path string
	Code int
	Msg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tuya %s: code %d: %s", e.Path, e.Code, e.Msg)
}

type envelope struct {
	Success bool            `json:"success"`
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	Result  json.RawMessage `json:"result"`
}

// get calls an authorized GET endpoint and decodes "result" into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.call(ctx, http.MethodGet, path, query, nil, out)
}

func (c *Client) call(ctx context.Context, method, path string, query url.Values, body, out any) error {
	for attempt := 0; ; attempt++ {
		tok, err := c.accessToken(ctx)
		if err != nil {
			return err
		}
		err = c.do(ctx, method, path, query, body, tok, out)
		var apiErr *APIError
		if attempt == 0 && errors.As(err, &apiErr) && tokenErrorCodes[apiErr.Code] {
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		return err
	}
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Before(c.expires) {
		return c.token, nil
	}
	var res struct {
		AccessToken string `json:"access_token"`
		ExpireTime  int    `json:"expire_time"` // seconds
	}
	q := url.Values{"grant_type": {"1"}}
	if err := c.do(ctx, http.MethodGet, "/v1.0/token", q, nil, "", &res); err != nil {
		return "", err
	}
	c.token = res.AccessToken
	// Renew a minute early to avoid using a token that expires mid-request.
	c.expires = c.now().Add(time.Duration(res.ExpireTime)*time.Second - time.Minute)
	return c.token, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, token string, out any) error {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return err
		}
	}
	target := path
	if len(query) > 0 {
		target += "?" + sortedQuery(query)
	}

	t := fmt.Sprint(c.now().UnixMilli())
	nonce := newNonce()
	req, err := http.NewRequestWithContext(ctx, method, c.opts.BaseURL+target, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("client_id", c.opts.AccessID)
	req.Header.Set("t", t)
	req.Header.Set("nonce", nonce)
	req.Header.Set("sign_method", "HMAC-SHA256")
	req.Header.Set("sign", c.sign(token, t, nonce, method, raw, target))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("access_token", token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tuya %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("tuya %s: %w", path, err)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("tuya %s: HTTP %d: bad response: %w", path, resp.StatusCode, err)
	}
	if !env.Success {
		return &APIError{Path: path, Code: env.Code, Msg: env.Msg}
	}
	if out == nil || len(env.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("tuya %s: decode result: %w", path, err)
	}
	return nil
}

func (c *Client) sign(token, t, nonce, method string, body []byte, target string) string {
	sum := sha256.Sum256(body)
	stringToSign := strings.Join([]string{method, hex.EncodeToString(sum[:]), "", target}, "\n")
	mac := hmac.New(sha256.New, []byte(c.opts.Secret))
	mac.Write([]byte(c.opts.AccessID + token + t + nonce + stringToSign))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

// sortedQuery encodes query parameters sorted by key, as the signature requires.
func sortedQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, "&")
}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
