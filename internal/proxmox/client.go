// Package proxmox is a typed client for the ProxMox VE API.
//
// All methods accept a context and return typed responses. Read-only
// surface for v0.1; mutating endpoints land later.
package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// formBody encodes url.Values for an x-www-form-urlencoded request body.
func formBody(v url.Values) io.Reader {
	if v == nil {
		return nil
	}
	return strings.NewReader(v.Encode())
}

// StrictPercentEncode percent-encodes every byte that is not in PVE's
// unreserved set per /usr/share/perl5/PVE/JSONSchema.pm pve_verify_urlencoded:
//
//	/^[-%a-zA-Z0-9_.!~*'()]*$/
//
// PVE applies this regex to fields like qemu sshkeys / lxc ssh-public-keys
// AFTER form-decoding once, so it sees the value exactly as we put it in
// url.Values. Go's stdlib url.QueryEscape uses RFC 1738 form encoding
// (space → "+"), which fails this regex. Use this helper for any field PVE
// validates as `format urlencoded`.
func StrictPercentEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z',
			'A' <= c && c <= 'Z',
			'0' <= c && c <= '9',
			c == '-', c == '_', c == '.',
			c == '!', c == '~', c == '*',
			c == '\'', c == '(', c == ')':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

// Config configures a Client.
type Config struct {
	// Endpoint is the ProxMox server URL, e.g. "https://10.0.0.12:8006".
	// /api2/json is appended automatically.
	Endpoint string

	// TokenID is the ProxMox API token identifier ("user@realm!tokenname").
	TokenID string

	// TokenSecret is the secret value paired with TokenID.
	TokenSecret string

	// TLSSkipVerify disables certificate verification (typical for self-signed
	// ProxMox installations). Default false.
	TLSSkipVerify bool

	// Timeout is the per-request timeout. Default 30s.
	Timeout time.Duration
}

// Client speaks the ProxMox VE API.
type Client struct {
	base       string
	tokenID    string
	secret     string
	httpClient *http.Client
}

// New returns a Client. It does not perform I/O.
func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("proxmox: endpoint is required")
	}
	if cfg.TokenID == "" || cfg.TokenSecret == "" {
		return nil, fmt.Errorf("proxmox: token_id and token_secret are required")
	}
	if _, err := url.Parse(cfg.Endpoint); err != nil {
		return nil, fmt.Errorf("proxmox: invalid endpoint: %w", err)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.TLSSkipVerify},
	}
	return &Client{
		base:    strings.TrimRight(cfg.Endpoint, "/") + "/api2/json",
		tokenID: cfg.TokenID,
		secret:  cfg.TokenSecret,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
	}, nil
}

// APIError is returned by client methods when the server responds with
// a non-2xx status. Callers can type-assert to inspect Status.
type APIError struct {
	Status int
	Path   string
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("proxmox: %s: HTTP %d: %s", e.Path, e.Status, e.Body)
}

type apiEnvelope struct {
	Data json.RawMessage `json:"data"`
}

// do executes an authenticated request and returns the unwrapped `data` field.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (json.RawMessage, error) {
	full := c.base + path
	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return nil, fmt.Errorf("proxmox: build request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", c.tokenID, c.secret))
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxmox: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("proxmox: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Path: path, Body: strings.TrimSpace(string(raw))}
	}

	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("proxmox: parse %s: %w", path, err)
	}
	return env.Data, nil
}

// GetJSON issues GET path and decodes the response data field into out.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("proxmox: decode %s: %w", path, err)
	}
	return nil
}

// PostForm issues POST path with url.Values as a form-encoded body and
// returns the raw `data` field. Most mutating ProxMox endpoints return a
// UPID string here; decode with decodeUPID.
func (c *Client) PostForm(ctx context.Context, path string, form url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPost, path, formBody(form))
}

// PutForm issues PUT path with url.Values as a form-encoded body.
func (c *Client) PutForm(ctx context.Context, path string, form url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPut, path, formBody(form))
}

// Delete issues DELETE path with optional params as a query string. PVE
// rejects DELETE requests that carry a body ("HTTP 501: Unexpected content
// for method 'DELETE'"), so we serialise the form into the URL instead.
func (c *Client) Delete(ctx context.Context, path string, form url.Values) (json.RawMessage, error) {
	if len(form) > 0 {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + form.Encode()
	}
	return c.do(ctx, http.MethodDelete, path, nil)
}

// decodeUPID decodes a `data` field that is a JSON-encoded string (the typical
// shape for mutating endpoints, e.g. "UPID:pve1:...").
func decodeUPID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("proxmox: decode UPID: %w (got %s)", err, string(raw))
	}
	return s, nil
}
