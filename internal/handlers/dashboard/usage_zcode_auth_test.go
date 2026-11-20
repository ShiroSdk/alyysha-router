// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestZcodeBillingTokenPrecedence(t *testing.T) {
	cases := []struct {
		name, jwt, business, access, key, want string
	}{
		{"zcodeJwtToken from psd wins over apiKey", "legacy-plan", "business", "access", "api-key", "legacy-plan"},
		{"business token before access", "", "business", "access", "api-key", "business"},
		{"OAuth access JWT unchanged", "", "", "plan.jwt", "api-key", "plan.jwt"},
		{"manual plan JWT fallback", "", "", "", "manual.jwt", "manual.jwt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer "+tc.want {
					t.Errorf("Authorization=%q, want selected billing JWT", got)
				}
				if r.URL.Path == zcodeBalancePath && r.Header.Get("X-Device-Mid") != "12345678-1234-1234-1234-123456789abc" {
					t.Error("stored device identity changed")
				}
				_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
			}))
			defer srv.Close()
			old := zcodeBillingOrigin
			zcodeBillingOrigin = srv.URL
			defer func() { zcodeBillingOrigin = old }()
			_, supported := fetchProviderUsage(t.Context(), "zcode", map[string]any{
				"accessToken": tc.access, "apiKey": tc.key,
				"providerSpecificData": map[string]any{
					"zcodeJwtToken": tc.jwt, "zaiBusinessToken": tc.business,
					"deviceId": "12345678-1234-1234-1234-123456789abc",
				},
			})
			if !supported { t.Fatal("ZCode tracker disappeared") }
		})
	}
}

func TestZcodeAuthRejectionGuidance(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		for _, code := range []int{401, 3001, 3012, 9999} {
			t.Run(fmt.Sprintf("fallback=%v/code=%d", fallback, code), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if fallback && r.URL.Path == zcodeBalancePath {
						_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
						return
					}
					_, _ = fmt.Fprintf(w, `{"code":%d,"msg":"token_version rejected"}`, code)
				}))
				defer srv.Close()
				old := zcodeBillingOrigin
				zcodeBillingOrigin = srv.URL
				defer func() { zcodeBillingOrigin = old }()
				res := fetchZcodeUsage(t.Context(), "fixture-token", "fixture-device")
				if res.message != zcodeQuotaAuthMessage || !res.bare {
					t.Errorf("rejection guidance=%q, bare=%v", res.message, res.bare)
				}
			})
		}
	}
}
