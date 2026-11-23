// Package kvclient is an HTTP client for the encrypted key-value store.
//
// The server encrypts each value under a per-entry key. This client sends
// plaintext bytes and presents the raw 32-byte key returned by [Client.Store].
// The key is sent as standard base64 in the X-Encryption-Key header.
package kvclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// KeySize is the raw encryption key length, in bytes.
	KeySize = 32

	encryptionKeyHeader = "X-Encryption-Key"
	defaultBaseURL      = "http://localhost:8080"
	defaultTimeout      = 30 * time.Second
	errorBodyLimit      = 8 << 10
)

// Client calls the store over HTTP. A Client is safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
}

// Option configures a [Client].
type Option func(*Client)

// WithHTTPClient uses the given client for requests.
// A nil client leaves the default in place.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// New returns a client for baseURL. An empty baseURL uses http://localhost:8080.
// baseURL must be absolute and may include a path prefix. The default HTTP
// client times out after 30 seconds.
func New(baseURL string, opts ...Option) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	c := &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: defaultTimeout}
	}
	return c
}

type bytePayload struct {
	Data []byte `json:"data"`
}

type keyPayload struct {
	Key []byte `json:"key"`
}

// Store creates id with data and returns the raw 32-byte key.
// The server returns that key once. data must be non-nil; an empty slice
// stores empty plaintext. A nil slice is rejected because the server rejects JSON null.
func (c *Client) Store(ctx context.Context, id string, data []byte) ([]byte, error) {
	const op = "store"
	if err := validateID(id); err != nil {
		return nil, err
	}
	if err := validateData(data); err != nil {
		return nil, err
	}
	body, err := json.Marshal(bytePayload{Data: data})
	if err != nil {
		return nil, fmt.Errorf("kvclient: %s: %w", op, err)
	}
	resp, err := c.exchange(ctx, op, http.MethodPost, id, nil, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, statusError(op, resp)
	}
	var out keyPayload
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("kvclient: %s: decode response: %w", op, err)
	}
	if len(out.Key) != KeySize {
		return nil, fmt.Errorf("kvclient: %s: response key length %d, want %d", op, len(out.Key), KeySize)
	}
	return out.Key, nil
}

// Get returns the plaintext stored at id.
// key is the raw 32-byte value from [Client.Store].
func (c *Client) Get(ctx context.Context, id string, key []byte) ([]byte, error) {
	const op = "get"
	if err := validateID(id); err != nil {
		return nil, err
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	resp, err := c.exchange(ctx, op, http.MethodGet, id, key, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(op, resp)
	}
	var out bytePayload
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("kvclient: %s: decode response: %w", op, err)
	}
	if out.Data == nil {
		return nil, fmt.Errorf("kvclient: %s: response data is null", op)
	}
	return out.Data, nil
}

// Update replaces the plaintext stored at id.
// key is the raw 32-byte value from [Client.Store]. data follows the same
// rules as [Client.Store].
func (c *Client) Update(ctx context.Context, id string, key, data []byte) error {
	const op = "update"
	if err := validateID(id); err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return err
	}
	if err := validateData(data); err != nil {
		return err
	}
	body, err := json.Marshal(bytePayload{Data: data})
	if err != nil {
		return fmt.Errorf("kvclient: %s: %w", op, err)
	}
	resp, err := c.exchange(ctx, op, http.MethodPut, id, key, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return statusError(op, resp)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, errorBodyLimit))
	return nil
}

// Delete removes id.
// key is the raw 32-byte value from [Client.Store].
func (c *Client) Delete(ctx context.Context, id string, key []byte) error {
	const op = "delete"
	if err := validateID(id); err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return err
	}
	resp, err := c.exchange(ctx, op, http.MethodDelete, id, key, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return statusError(op, resp)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, errorBodyLimit))
	return nil
}

func (c *Client) exchange(ctx context.Context, op, method, id string, key, body []byte) (*http.Response, error) {
	endpoint, err := c.endpoint(id)
	if err != nil {
		return nil, fmt.Errorf("kvclient: %s: %w", op, err)
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rdr)
	if err != nil {
		return nil, fmt.Errorf("kvclient: %s: %w", op, err)
	}
	if key != nil {
		req.Header.Set(encryptionKeyHeader, base64.StdEncoding.EncodeToString(key))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kvclient: %s: %w", op, err)
	}
	return resp, nil
}

func (c *Client) endpoint(id string) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("base url %q must be absolute", c.baseURL)
	}
	u.Fragment = ""
	// PathEscape keeps the id in one segment. URL.String re-encodes Path and
	// would turn %2F into %252F, so the escaped form is stored in RawPath.
	decoded := strings.TrimRight(u.Path, "/")
	escaped := strings.TrimRight(u.EscapedPath(), "/")
	u.Path = decoded + "/yoti/" + id
	u.RawPath = escaped + "/yoti/" + url.PathEscape(id)
	return u.String(), nil
}

func validateID(id string) error {
	if id == "" {
		return ErrEmptyID
	}
	return nil
}

func validateData(data []byte) error {
	if data == nil {
		return ErrNilData
	}
	return nil
}

func validateKey(key []byte) error {
	if len(key) != KeySize {
		return ErrInvalidKey
	}
	return nil
}
