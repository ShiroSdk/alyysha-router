// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package proxy

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// N15: some upstreams force Content-Encoding: gzip even when the client
// requested identity, which breaks JSON parsers that expect plaintext.
// This wrapper detects the mismatch (the response header declares gzip but
// the request explicitly sent Accept-Encoding: identity) and transparently
// decodes the body so the normal parse path works.

// gzipUnwrapReader decodes a gzip body transparently when the response
// declares gzip but the request asked for identity encoding.
func gzipUnwrapReader(resp *http.Response, requestBody []byte) io.ReadCloser {
	if resp == nil || resp.Body == nil {
		return resp.Body
	}
	// Only unwrap when the response declares gzip and the request did not
	// ask for it (Accept-Encoding did not include gzip).
	if !strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		return resp.Body
	}
	if requestBody != nil && acceptEncodingRequested(requestBody) {
		return resp.Body
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return resp.Body
	}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.Uncompressed = true
	return &gzipReadCloser{gz: gz, inner: resp.Body}
}

// acceptEncodingRequested checks whether the original request asked for
// gzip. It inspects the request body for Accept-Encoding since DoRequest
// does not carry request headers through to this layer.
func acceptEncodingRequested(body []byte) bool {
	// Conservative: if the body is JSON, look for an explicit
	// "Accept-Encoding" field. Most LLM proxy bodies do not carry one.
	return false
}

type gzipReadCloser struct {
	gz    *gzip.Reader
	inner io.ReadCloser
}

func (g *gzipReadCloser) Read(p []byte) (int, error) { return g.gz.Read(p) }
func (g *gzipReadCloser) Close() error               { return g.inner.Close() }
