package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreGetPutDelete(t *testing.T) {
	key := bytes.Repeat([]byte{0xfb}, 32)
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.json")
	if err := os.WriteFile(storePath, []byte("{\n  \"id\": \"records\",\n  \"data\": \"hello\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var method, gotKey, gotPath string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		gotKey = r.Header.Get("X-Encryption-Key")
		gotPath = r.URL.EscapedPath()
		body, _ = readAll(r)
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(struct {
				Key []byte `json:"key"`
			}{Key: key})
		case http.MethodGet:
			_, _ = w.Write(mustJSON(t, map[string][]byte{"data": []byte("hello")}))
		case http.MethodPut, http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-addr", srv.URL, "store", storePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("store exit %d: %s", code, stderr.String())
	}
	if stdout.String() != base64.StdEncoding.EncodeToString(key)+"\n" {
		t.Fatalf("store stdout = %q", stdout.String())
	}
	if stderr.String() != "kv: store records: 201 Created\n" {
		t.Fatalf("store stderr = %q", stderr.String())
	}
	if method != http.MethodPost || gotPath != "/yoti/records" {
		t.Fatalf("store request = %s %s", method, gotPath)
	}
	var stored struct {
		Data []byte `json:"data"`
	}
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored.Data, []byte("hello")) {
		t.Fatalf("stored %q, want hello", stored.Data)
	}

	keyText := strings.TrimSpace(stdout.String())
	getPath := filepath.Join(dir, "get.json")
	if err := os.WriteFile(getPath, []byte(`{"id":"records","key":"`+keyText+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-addr", srv.URL, "get", getPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("get exit %d: %s", code, stderr.String())
	}
	if !bytes.Equal(stdout.Bytes(), []byte("hello")) {
		t.Fatalf("get stdout = %q", stdout.Bytes())
	}
	if stderr.String() != "kv: get records: 200 OK\n" {
		t.Fatalf("get stderr = %q", stderr.String())
	}
	if gotKey != keyText || gotPath != "/yoti/records" {
		t.Fatalf("get request key %q path %s", gotKey, gotPath)
	}

	object := []byte(`{"msg":"world"}`)
	putPath := filepath.Join(dir, "put.json")
	putFile := []byte(`{"id":"records","key":"` + keyText + `","data":` + string(object) + `}`)
	if err := os.WriteFile(putPath, putFile, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-addr", srv.URL, "put", putPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("put exit %d: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("put stdout = %q", stdout.String())
	}
	if stderr.String() != "kv: put records: 204 No Content\n" {
		t.Fatalf("put stderr = %q", stderr.String())
	}
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored.Data, object) {
		t.Fatalf("updated %q, want %q", stored.Data, object)
	}
	if gotKey != keyText {
		t.Fatalf("put key header = %q", gotKey)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-addr", srv.URL, "delete", getPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("delete exit %d: %s", code, stderr.String())
	}
	if method != http.MethodDelete || gotPath != "/yoti/records" {
		t.Fatalf("delete request = %s %s", method, gotPath)
	}
	if stderr.String() != "kv: delete records: 204 No Content\n" {
		t.Fatalf("delete stderr = %q", stderr.String())
	}
}

func TestRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	bad := write("bad.json", "{")
	noID := write("noid.json", `{"data":"hello"}`)
	noData := write("nodata.json", `{"id":"records"}`)
	nullData := write("null.json", `{"id":"records","data":null}`)
	array := write("array.json", `[1]`)
	shortKey := write("short.json", `{"id":"records","key":"YQ=="}`)
	badKey := write("badkey.json", `{"id":"records","key":"not base64"}`)
	missing := filepath.Join(dir, "missing.json")

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	cases := [][]string{
		{"-addr", srv.URL, "store", bad},
		{"-addr", srv.URL, "store", missing},
		{"-addr", srv.URL, "store", noID},
		{"-addr", srv.URL, "store", noData},
		{"-addr", srv.URL, "store", nullData},
		{"-addr", srv.URL, "store", array},
		{"-addr", srv.URL, "get", shortKey},
		{"-addr", srv.URL, "get", badKey},
		{"-addr", srv.URL, "store"},
		{"nope"},
	}
	for _, args := range cases {
		called = false
		var stdout, stderr bytes.Buffer
		code := run(args, &stdout, &stderr)
		if code != 2 {
			t.Errorf("%v exit %d, stderr %q", args, code, stderr.String())
		}
		if called {
			t.Errorf("%v sent a request", args)
		}
		if stderr.Len() == 0 {
			t.Errorf("%v wrote no error", args)
		}
	}
}

func TestStatusExitCodes(t *testing.T) {
	keyText := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.json")
	getPath := filepath.Join(dir, "get.json")
	putPath := filepath.Join(dir, "put.json")
	if err := os.WriteFile(storePath, []byte(`{"id":"records","data":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(getPath, []byte(`{"id":"records","key":"`+keyText+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(putPath, []byte(`{"id":"records","key":"`+keyText+`","data":{"ok":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		status int
		want   int
		args   []string
	}{
		{http.StatusConflict, 5, []string{"store", storePath}},
		{http.StatusUnauthorized, 3, []string{"get", getPath}},
		{http.StatusNotFound, 4, []string{"delete", getPath}},
		{http.StatusInternalServerError, 1, []string{"put", putPath}},
	}
	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "failed to handle request", tt.status)
		}))
		args := append([]string{"-addr", srv.URL}, tt.args...)
		var stdout, stderr bytes.Buffer
		code := run(args, &stdout, &stderr)
		srv.Close()
		if code != tt.want {
			t.Errorf("status %d exit %d, want %d (%s)", tt.status, code, tt.want, stderr.String())
		}
	}
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
