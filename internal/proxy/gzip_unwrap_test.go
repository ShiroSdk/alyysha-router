// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package proxy

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGzipUnwrapForcedGzip pins (N15): an upstream that forces
// Content-Encoding: gzip has its body transparently decoded.
func TestGzipUnwrapForcedGzip(t *testing.T) {
	inner := "hello from the unwrapped body"
	enc := &strings.Builder{}
	gz := gzip.NewWriter(enc)
	gz.Write([]byte(inner))
	gz.Close()

	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Encoding", "gzip")
	rec.Body.WriteString(enc.String())
	resp := rec.Result()

	reader := gzipUnwrapReader(resp, nil)
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != inner {
		t.Errorf("body = %q, want %q", data, inner)
	}
}

// TestGzipUnwrapNoGzipSkipped pins the pass-through: a non-gzip response is
// returned as-is.
func TestGzipUnwrapNoGzipSkipped(t *testing.T) {
	inner := "plain text body"
	rec := httptest.NewRecorder()
	rec.Body.WriteString(inner)
	resp := rec.Result()

	reader := gzipUnwrapReader(resp, nil)
	data, _ := io.ReadAll(reader)
	if string(data) != inner {
		t.Errorf("body = %q, want %q", data, inner)
	}
}
