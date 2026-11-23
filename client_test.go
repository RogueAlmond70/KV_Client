package kvclient_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RogueAlmond70/KV_Client"
)

func TestStoreGetUpdateDelete(t *testing.T) {
	key := bytes.Repeat([]byte{0xfb}, kvclient.KeySize)
	plaintext := []byte("hello")
	updated := []byte("world")

	var gotMethod, gotPath, gotKey, gotType string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotKey = r.Header.Get("X-Encryption-Key")
		gotType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)

		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(struct {
				Key []byte `json:"key"`
			}{Key: key})
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				Data []byte `json:"data"`
			}{Data: plaintext})
		case http.MethodPut, http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	c := kvclient.New(srv.URL + "/")
	ctx := context.Background()

	got, err := c.Store(ctx, "demo", plaintext)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Fatalf("Store key = %x, want %x", got, key)
	}
	if gotMethod != http.MethodPost || gotPath != "/yoti/demo" {
		t.Fatalf("Store request = %s %s", gotMethod, gotPath)
	}
	if gotKey != "" {
		t.Fatalf("Store sent a key header %q", gotKey)
	}
	if gotType != "application/json" {
		t.Fatalf("Store content type = %q", gotType)
	}
	var stored struct {
		Data []byte `json:"data"`
	}
	if err := json.Unmarshal(gotBody, &stored); err != nil {
		t.Fatalf("Store body: %v", err)
	}
	if !bytes.Equal(stored.Data, plaintext) {
		t.Fatalf("Store data = %q, want %q", stored.Data, plaintext)
	}

	data, err := c.Get(ctx, "a/b", key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(data, plaintext) {
		t.Fatalf("Get = %q, want %q", data, plaintext)
	}
	if gotMethod != http.MethodGet || gotPath != "/yoti/a%2Fb" {
		t.Fatalf("Get request = %s %s", gotMethod, gotPath)
	}
	if gotKey != base64.StdEncoding.EncodeToString(key) {
		t.Fatalf("Get key header = %q", gotKey)
	}
	if strings.ContainsAny(gotKey, "-_") {
		t.Fatalf("key header %q is not standard base64", gotKey)
	}

	if err := c.Update(ctx, "demo", key, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if gotMethod != http.MethodPut || gotType != "application/json" {
		t.Fatalf("Update request = %s content-type %q", gotMethod, gotType)
	}
	if gotKey != base64.StdEncoding.EncodeToString(key) {
		t.Fatalf("Update key header = %q", gotKey)
	}
	var replaced struct {
		Data []byte `json:"data"`
	}
	if err := json.Unmarshal(gotBody, &replaced); err != nil {
		t.Fatalf("Update body: %v", err)
	}
	if !bytes.Equal(replaced.Data, updated) {
		t.Fatalf("Update data = %q, want %q", replaced.Data, updated)
	}

	if err := c.Delete(ctx, "demo", key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/yoti/demo" {
		t.Fatalf("Delete request = %s %s", gotMethod, gotPath)
	}
	if len(gotBody) != 0 {
		t.Fatalf("Delete body = %q", gotBody)
	}
}

func TestEmptyPlaintext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"data":""}` {
			t.Errorf("body = %s", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	err := kvclient.New(srv.URL).Update(context.Background(), "demo", bytes.Repeat([]byte{1}, kvclient.KeySize), []byte{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetEmptyPlaintext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":""}`))
	}))
	t.Cleanup(srv.Close)

	data, err := kvclient.New(srv.URL).Get(context.Background(), "demo", bytes.Repeat([]byte{1}, kvclient.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	if data == nil || len(data) != 0 {
		t.Fatalf("data = %#v, want empty non-nil slice", data)
	}
}

func TestClientValidation(t *testing.T) {
	c := kvclient.New("")
	ctx := context.Background()
	key := bytes.Repeat([]byte{1}, kvclient.KeySize)

	_, err := c.Store(ctx, "", []byte("x"))
	if !errors.Is(err, kvclient.ErrEmptyID) {
		t.Fatalf("Store empty id: %v", err)
	}
	_, err = c.Store(ctx, "demo", nil)
	if !errors.Is(err, kvclient.ErrNilData) {
		t.Fatalf("Store nil data: %v", err)
	}
	_, err = c.Get(ctx, "demo", []byte("short"))
	if !errors.Is(err, kvclient.ErrInvalidKey) {
		t.Fatalf("Get short key: %v", err)
	}
	err = c.Update(ctx, "demo", key, nil)
	if !errors.Is(err, kvclient.ErrNilData) {
		t.Fatalf("Update nil data: %v", err)
	}
	err = c.Delete(ctx, "", key)
	if !errors.Is(err, kvclient.ErrEmptyID) {
		t.Fatalf("Delete empty id: %v", err)
	}
}

func TestStatusErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		check  func(error) bool
		call   func(*kvclient.Client) error
	}{
		{
			name:   "conflict",
			status: http.StatusConflict,
			check:  kvclient.IsConflict,
			call: func(c *kvclient.Client) error {
				_, err := c.Store(context.Background(), "demo", []byte("x"))
				return err
			},
		},
		{
			name:   "unauthorized",
			status: http.StatusUnauthorized,
			check:  kvclient.IsUnauthorized,
			call: func(c *kvclient.Client) error {
				_, err := c.Get(context.Background(), "demo", bytes.Repeat([]byte{2}, kvclient.KeySize))
				return err
			},
		},
		{
			name:   "not found",
			status: http.StatusNotFound,
			check:  kvclient.IsNotFound,
			call: func(c *kvclient.Client) error {
				return c.Delete(context.Background(), "demo", bytes.Repeat([]byte{2}, kvclient.KeySize))
			},
		},
		{
			name:   "bad request",
			status: http.StatusBadRequest,
			body:   "missing data",
			check:  kvclient.IsBadRequest,
			call: func(c *kvclient.Client) error {
				return c.Update(context.Background(), "demo", bytes.Repeat([]byte{2}, kvclient.KeySize), []byte("x"))
			},
		},
		{
			name:   "server",
			status: http.StatusInternalServerError,
			body:   "failed to handle request",
			call: func(c *kvclient.Client) error {
				_, err := c.Get(context.Background(), "demo", bytes.Repeat([]byte{2}, kvclient.KeySize))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, tt.body, tt.status)
			}))
			t.Cleanup(srv.Close)

			err := tt.call(kvclient.New(srv.URL))
			var se *kvclient.StatusError
			if !errors.As(err, &se) {
				t.Fatalf("error = %v, want StatusError", err)
			}
			if se.Status != tt.status {
				t.Fatalf("status = %d, want %d", se.Status, tt.status)
			}
			if tt.body != "" && !strings.Contains(se.Body, tt.body) {
				t.Fatalf("body = %q, want it to contain %q", se.Body, tt.body)
			}
			if tt.check != nil && !tt.check(err) {
				t.Fatalf("predicate rejected %v", err)
			}
		})
	}
}

func TestStoreRejectsShortKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"YQ=="}`))
	}))
	t.Cleanup(srv.Close)

	_, err := kvclient.New(srv.URL).Store(context.Background(), "demo", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "response key length 1, want 32") {
		t.Fatalf("Store err = %v", err)
	}
}

func TestGetRejectsNullData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	t.Cleanup(srv.Close)

	_, err := kvclient.New(srv.URL).Get(context.Background(), "demo", bytes.Repeat([]byte{3}, kvclient.KeySize))
	if err == nil || !strings.Contains(err.Error(), "response data is null") {
		t.Fatalf("Get err = %v", err)
	}
}

func TestBaseURL(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	c := kvclient.New(srv.URL + "/prefix")
	key := bytes.Repeat([]byte{4}, kvclient.KeySize)
	if err := c.Delete(context.Background(), "demo", key); err != nil {
		t.Fatal(err)
	}
	if got != "/prefix/yoti/demo" {
		t.Fatalf("path = %q", got)
	}

	_, err := kvclient.New("localhost:8080").Get(context.Background(), "demo", key)
	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative base url err = %v", err)
	}
}

func TestWithHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	key := bytes.Repeat([]byte{5}, kvclient.KeySize)
	boom := errors.New("boom")
	c := kvclient.New(srv.URL, kvclient.WithHTTPClient(&http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, boom
		}),
	}))
	err := c.Delete(context.Background(), "demo", key)
	if !errors.Is(err, boom) {
		t.Fatalf("Delete err = %v, want boom", err)
	}

	c = kvclient.New(srv.URL, kvclient.WithHTTPClient(nil))
	if err := c.Delete(context.Background(), "demo", key); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := kvclient.New(srv.URL).Delete(ctx, "demo", bytes.Repeat([]byte{6}, kvclient.KeySize))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete err = %v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
