// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// claude.ai sits behind Cloudflare, so the lane ships the same browser-
// fingerprint TLS stack grok-web uses. A plain-HTTP escape hatch
// (CLAUDE_WEB_PLAIN_HTTP=1) exists for tests and for hosts where the
// fingerprint stack misbehaves; tests aim every URL at an httptest fake
// through it.

const claudeWebUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

var claudeWebTLSProfile = profiles.Chrome_146

// claudeDoer is what claude-web turns requests through: the fingerprint stack
// in production, a plain client under the escape hatch and in tests.
type claudeDoer interface {
	do(ctx context.Context, method, target string, headers [][2]string, body []byte) (*http.Response, error)
}

type claudePlainDoer struct{}

func (d *claudePlainDoer) do(ctx context.Context, method, target string, headers [][2]string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	for _, pair := range headers {
		req.Header.Set(pair[0], pair[1])
	}
	return http.DefaultClient.Do(req)
}

type claudeTLSDoer struct {
	client tls_client.HttpClient
}

func (d *claudeTLSDoer) do(ctx context.Context, method, target string, headers [][2]string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := fhttp.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, fmt.Errorf("claude-web: build request: %w", err)
	}
	for _, pair := range headers {
		request.Header.Set(pair[0], pair[1])
	}
	response, err := d.client.Do(request)
	if err != nil {
		return nil, err
	}
	header := make(http.Header, len(response.Header))
	for name, vals := range response.Header {
		header[name] = vals
	}
	return &http.Response{
		StatusCode:    response.StatusCode,
		Status:        response.Status,
		Header:        header,
		Body:          response.Body,
		ContentLength: response.ContentLength,
	}, nil
}

var claudeTransportFactory = func() (claudeDoer, error) {
	if claudeWebPlainHTTP() {
		return &claudePlainDoer{}, nil
	}
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(0),
		tls_client.WithClientProfile(claudeWebTLSProfile),
		// Chrome shuffles its TLS extension order per connection; a fixed
		// order is itself a fingerprint.
		tls_client.WithRandomTLSExtensionOrder(),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithDialer(net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}),
	}
	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, err
	}
	return &claudeTLSDoer{client: client}, nil
}

func claudeWebPlainHTTP() bool {
	return os.Getenv("CLAUDE_WEB_PLAIN_HTTP") == "1"
}

// claudeWebDo performs one claude.ai request with the browser headers every
// lane request carries. The cookie header is supplied by the caller; it is
// never logged.
func claudeWebDo(ctx context.Context, method, url string, headers map[string]string, body []byte) (*http.Response, error) {
	doer, err := claudeTransportFactory()
	if err != nil {
		return nil, err
	}
	pairs := make([][2]string, 0, len(headers)+3)
	for key, value := range headers {
		if strings.EqualFold(key, "User-Agent") || strings.EqualFold(key, "Accept-Language") {
			continue
		}
		pairs = append(pairs, [2]string{key, value})
	}
	pairs = append(pairs,
		[2]string{"User-Agent", claudeWebUserAgent},
		[2]string{"Accept-Language", "en-US,en;q=0.9"},
	)
	// Deduplicate while keeping order: a caller-supplied header wins.
	seen := make(map[string]bool, len(pairs))
	ordered := make([][2]string, 0, len(pairs))
	for _, pair := range pairs {
		lower := strings.ToLower(pair[0])
		if seen[lower] {
			continue
		}
		seen[lower] = true
		ordered = append(ordered, pair)
	}
	return doer.do(ctx, method, url, ordered, body)
}

// ClaudeIsCloudflareChallenge sniffs the Cloudflare interstitial markers the
// challenge page answers with.
func ClaudeIsCloudflareChallenge(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range []string{"just a moment", "attention required", "cf-mitigated"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
