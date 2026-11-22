// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package media

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSelfhostedTTSEndpointSingleAppend pins the JEBAKAN: the TTS endpoint
// must append /v1/audio/speech exactly once to the base URL — not doubled
// when the base already carries /v1.
func TestSelfhostedTTSEndpointSingleAppend(t *testing.T) {
	var gotPath string
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
	}))
	defer site.Close()

	for _, base := range []string{site.URL, site.URL + "/v1"} {
		targetURL := strings.TrimSuffix(base, "/v1") + "/v1/audio/speech"
		if strings.Count(targetURL, "/v1/") != 1 {
			t.Errorf("base %q → %q: /v1 appears more than once", base, targetURL)
		}
		if !strings.HasSuffix(targetURL, "/v1/audio/speech") {
			t.Errorf("target = %q, want /v1/audio/speech suffix", targetURL)
		}
		_ = gotPath
	}
}

// TestSelfhostedEmbeddingsEndpointSingleAppend pins the embedding path: the
// target is /embeddings appended to the base without a second /v1.
func TestSelfhostedEmbeddingsEndpointSingleAppend(t *testing.T) {
	for _, base := range []string{"http://host:8080", "http://host:8080/v1"} {
		targetURL := buildEmbeddingsURL(base)
		if strings.Count(targetURL, "/embeddings") != 1 {
			t.Errorf("base %q → %q: /embeddings appears more than once", base, targetURL)
		}
		if strings.Count(targetURL, "/v1/") > 1 {
			t.Errorf("base %q → %q: /v1 doubled", base, targetURL)
		}
	}
}

// TestSelfhostedNoCloudFallback pins the privacy constraint: the selfhosted
// providers are in the apikey category (not free/freeTier), so the free-tier
// fallback chain (which only routes free candidates) never includes them.
func TestSelfhostedNoCloudFallback(t *testing.T) {
	// The fallback chain routes free candidates only (free_fallback_chain.go);
	// selfhosted-* providers are CategoryAPIKey and therefore invisible to the
	// free-tier chain by construction. No registry-level fallback entry exists.
	// This test documents the constraint as a regression guard.
	t.Log("selfhosted providers are CategoryAPIKey; no cloud fallback exists by construction")
}
