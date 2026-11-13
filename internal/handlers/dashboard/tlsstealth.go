// Alyysha Router — built by YannClipper — https://t.me/YannClippers
package dashboard

import (
	"net/http"

	"alyysha/proxy/internal/handlerutil"
	"alyysha/proxy/internal/tlsstealth"
)

// HandleGetTLSStealthStatus serves GET /api/tls-stealth/status: the state the
// permanent dashboard banner renders from — whether TLS stealth is on, which
// browser profile the web-executor lane presents, whether the daily compliance
// alert loop is running, and the legal notice the banner must display while
// the flag is on.
//
// The banner is mandatory, not advisory: an operator who turned stealth on
// sees the notice on every dashboard load until they turn it off. The payload
// therefore carries the notice text itself so the SPA cannot drop it.
func (h *DashboardHandler) HandleGetTLSStealthStatus(w http.ResponseWriter, r *http.Request) {
	st := tlsstealth.Default.Status()
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":          st.Enabled,
		"activeProfile":    st.ActiveProfile,
		"alertLoopRunning": st.AlertLoopRunning,
		"legalNotice":      st.LegalNotice,
	})
}
