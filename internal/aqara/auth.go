package aqara

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Account types for config.auth.* intents.
const accountTypeAqara = 0

type tokenResult struct {
	ExpiresIn    json.RawMessage `json:"expiresIn"` // seconds; string or number
	OpenID       string          `json:"openId"`
	AccessToken  string          `json:"accessToken"`
	RefreshToken string          `json:"refreshToken"`
}

func (r tokenResult) toToken(now time.Time) (*Token, error) {
	secs, err := strconv.ParseInt(strings.Trim(string(r.ExpiresIn), `"`), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("aqara: bad expiresIn %q: %w", r.ExpiresIn, err)
	}
	return &Token{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		OpenID:       r.OpenID,
		ExpiresAt:    now.Add(time.Duration(secs) * time.Second),
	}, nil
}

// RequestAuthCode asks Aqara to send a one-time code to the account's
// email / phone. validity is the access token lifetime, e.g. "1y", "30d".
func (c *Client) RequestAuthCode(ctx context.Context, account, validity string) error {
	return c.do(ctx, "", "config.auth.getAuthCode", map[string]any{
		"account":             account,
		"accountType":         accountTypeAqara,
		"accessTokenValidity": validity,
	}, nil)
}

// ExchangeAuthCode trades the one-time code for tokens and stores them.
func (c *Client) ExchangeAuthCode(ctx context.Context, account, code string) (*Token, error) {
	var res tokenResult
	err := c.do(ctx, "", "config.auth.getToken", map[string]any{
		"authCode":    code,
		"account":     account,
		"accountType": accountTypeAqara,
	}, &res)
	if err != nil {
		return nil, err
	}
	tok, err := res.toToken(c.now())
	if err != nil {
		return nil, err
	}
	if err := c.store.Save(tok); err != nil {
		return nil, fmt.Errorf("save token: %w", err)
	}
	c.mu.Lock()
	c.token = tok
	c.mu.Unlock()
	return tok, nil
}

// accessToken returns a valid access token, refreshing it when needed.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token == nil {
		t, err := c.store.Load()
		if err != nil {
			return "", err
		}
		c.token = t
	}

	if c.token.ExpiresAt.Sub(c.now()) < refreshBefore {
		var res tokenResult
		err := c.do(ctx, "", "config.auth.refreshToken", map[string]any{
			"refreshToken": c.token.RefreshToken,
		}, &res)
		if err != nil {
			return "", fmt.Errorf("refresh token: %w", err)
		}
		t, err := res.toToken(c.now())
		if err != nil {
			return "", err
		}
		if err := c.store.Save(t); err != nil {
			return "", fmt.Errorf("save token: %w", err)
		}
		c.token = t
	}
	return c.token.AccessToken, nil
}
