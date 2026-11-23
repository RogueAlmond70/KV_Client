// Command kv stores and retrieves JSON documents on the encrypted key-value server.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/RogueAlmond70/KV_Client"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kv", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "http://localhost:8080", "store base URL")
	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage:
  kv [-addr URL] store <json-file>
  kv [-addr URL] get <json-file>
  kv [-addr URL] put <json-file>
  kv [-addr URL] delete <json-file>

-addr is the store base URL (default http://localhost:8080).
The file is a JSON object with "id". store and put also require "data".
get, put, and delete require "key", the standard base64 key printed by store.
A string "data" is stored as those characters. Any other JSON value is stored as its JSON text.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fs.Usage()
		return 2
	}

	c := kvclient.New(*addr)
	ctx := context.Background()
	switch rest[0] {
	case "store":
		if len(rest) != 2 {
			fs.Usage()
			return 2
		}
		return store(ctx, c, rest[1], stdout, stderr)
	case "get":
		if len(rest) != 2 {
			fs.Usage()
			return 2
		}
		return get(ctx, c, rest[1], stdout, stderr)
	case "put":
		if len(rest) != 2 {
			fs.Usage()
			return 2
		}
		return put(ctx, c, rest[1], stderr)
	case "delete":
		if len(rest) != 2 {
			fs.Usage()
			return 2
		}
		return del(ctx, c, rest[1], stderr)
	default:
		fmt.Fprintf(stderr, "kv: unknown command %q\n", rest[0])
		fs.Usage()
		return 2
	}
}

type document struct {
	id       string
	data     []byte
	key      string
	keyBytes []byte
	hasData  bool
	hasKey   bool
}

func store(ctx context.Context, c *kvclient.Client, path string, stdout, stderr io.Writer) int {
	doc, err := readDocument(path)
	if err != nil {
		return usage(stderr, err)
	}
	if doc.id == "" {
		return usage(stderr, fmt.Errorf("%s: id is required", path))
	}
	if !doc.hasData {
		return usage(stderr, fmt.Errorf("%s: data is required", path))
	}
	key, err := c.Store(ctx, doc.id, doc.data)
	if err != nil {
		return fail(stderr, err)
	}
	succeeded(stderr, "store", doc.id, http.StatusCreated)
	fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(key))
	return 0
}

func get(ctx context.Context, c *kvclient.Client, path string, stdout, stderr io.Writer) int {
	doc, n := open(path, true, false, stderr)
	if n != 0 {
		return n
	}
	data, err := c.Get(ctx, doc.id, doc.keyBytes)
	if err != nil {
		return fail(stderr, err)
	}
	succeeded(stderr, "get", doc.id, http.StatusOK)
	if _, err := stdout.Write(data); err != nil {
		fmt.Fprintf(stderr, "kv: %v\n", err)
		return 1
	}
	return 0
}

func put(ctx context.Context, c *kvclient.Client, path string, stderr io.Writer) int {
	doc, n := open(path, true, true, stderr)
	if n != 0 {
		return n
	}
	if err := c.Update(ctx, doc.id, doc.keyBytes, doc.data); err != nil {
		return fail(stderr, err)
	}
	succeeded(stderr, "put", doc.id, http.StatusNoContent)
	return 0
}

func del(ctx context.Context, c *kvclient.Client, path string, stderr io.Writer) int {
	doc, n := open(path, true, false, stderr)
	if n != 0 {
		return n
	}
	if err := c.Delete(ctx, doc.id, doc.keyBytes); err != nil {
		return fail(stderr, err)
	}
	succeeded(stderr, "delete", doc.id, http.StatusNoContent)
	return 0
}

func succeeded(stderr io.Writer, op, id string, status int) {
	fmt.Fprintf(stderr, "kv: %s %s: %d %s\n", op, id, status, http.StatusText(status))
}

// open reads the file and checks the fields this command sends.
func open(path string, needKey, needData bool, stderr io.Writer) (document, int) {
	doc, err := readDocument(path)
	if err != nil {
		return document{}, usage(stderr, err)
	}
	if doc.id == "" {
		return document{}, usage(stderr, fmt.Errorf("%s: id is required", path))
	}
	if needData && !doc.hasData {
		return document{}, usage(stderr, fmt.Errorf("%s: data is required", path))
	}
	if !needKey {
		return doc, 0
	}
	if !doc.hasKey {
		return document{}, usage(stderr, fmt.Errorf("%s: key is required", path))
	}
	key, err := decodeKey(doc.key)
	if err != nil {
		return document{}, usage(stderr, err)
	}
	doc.keyBytes = key
	return doc, 0
}

func readDocument(path string) (document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return document{}, err
	}
	if !json.Valid(b) {
		return document{}, fmt.Errorf("%s: invalid JSON", path)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return document{}, fmt.Errorf("%s: JSON must be an object", path)
	}
	var doc document
	if raw, ok := fields["id"]; ok {
		if err := json.Unmarshal(raw, &doc.id); err != nil {
			return document{}, fmt.Errorf("%s: id must be a string", path)
		}
	}
	if raw, ok := fields["data"]; ok {
		data, err := dataBytes(raw)
		if err != nil {
			return document{}, fmt.Errorf("%s: %w", path, err)
		}
		doc.data = data
		doc.hasData = true
	}
	if raw, ok := fields["key"]; ok {
		if err := json.Unmarshal(raw, &doc.key); err != nil {
			return document{}, fmt.Errorf("%s: key must be a string", path)
		}
		doc.hasKey = true
	}
	return doc, nil
}

func dataBytes(raw json.RawMessage) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, errors.New("data is required")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("data: %w", err)
		}
		return []byte(s), nil
	}
	return raw, nil
}

func decodeKey(s string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("key must be standard base64: %w", err)
	}
	if len(key) != kvclient.KeySize {
		return nil, kvclient.ErrInvalidKey
	}
	return key, nil
}

func usage(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "kv: %v\n", err)
	return 2
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "kv: %v\n", err)
	switch {
	case errors.Is(err, kvclient.ErrEmptyID), errors.Is(err, kvclient.ErrNilData), errors.Is(err, kvclient.ErrInvalidKey):
		return 2
	case kvclient.IsUnauthorized(err):
		return 3
	case kvclient.IsNotFound(err):
		return 4
	case kvclient.IsConflict(err):
		return 5
	default:
		return 1
	}
}
