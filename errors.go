package kvclient

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	// ErrEmptyID is returned when an id is the empty string.
	ErrEmptyID = errors.New("kvclient: id is empty")

	// ErrNilData is returned when data is nil.
	// An empty non-nil slice is valid and stores empty plaintext.
	ErrNilData = errors.New("kvclient: data is nil")

	// ErrInvalidKey is returned when a key is not 32 bytes.
	ErrInvalidKey = errors.New("kvclient: encryption key must be 32 bytes")
)

// StatusError is an HTTP response other than the success status for the call.
type StatusError struct {
	Op     string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("kvclient: %s: %d", e.Op, e.Status)
	}
	return fmt.Sprintf("kvclient: %s: %d: %s", e.Op, e.Status, e.Body)
}

// IsNotFound reports whether err is a 404 from the store.
func IsNotFound(err error) bool {
	return statusIs(err, http.StatusNotFound)
}

// IsUnauthorized reports whether err is a 401 from the store.
// The id exists and the key does not match.
func IsUnauthorized(err error) bool {
	return statusIs(err, http.StatusUnauthorized)
}

// IsConflict reports whether err is a 409 from the store.
// Store was called for an id that already exists.
func IsConflict(err error) bool {
	return statusIs(err, http.StatusConflict)
}

// IsBadRequest reports whether err is a 400 from the store.
func IsBadRequest(err error) bool {
	return statusIs(err, http.StatusBadRequest)
}

func statusIs(err error, status int) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == status
}

func statusError(op string, resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	return &StatusError{
		Op:     op,
		Status: resp.StatusCode,
		Body:   strings.TrimSpace(string(b)),
	}
}
