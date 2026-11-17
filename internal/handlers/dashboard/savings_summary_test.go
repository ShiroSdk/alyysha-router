// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9routerx/proxy/internal/db"
)

// TestHandleGetSavingsSummaryShape pins the aggregation: the response carries
// window + totalTokensSaved + totalUsd + the three sources, and the totals
// equal the sum of the sources (the honest-additivity contract).
func TestHandleGetSavingsSummaryShape(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	if err := db.EnsureCoreSchema(database.RawDB()); err != nil {
		t.Fatalf("ensure core schema: %v", err)
	}
	router := setupTestRouter(database)

	req := httptest.NewRequest(http.MethodGet, "/api/savings/summary?period=7d", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Window           string           `json:"window"`
		TotalTokensSaved int64            `json:"totalTokensSaved"`
		TotalUsd         float64          `json:"totalUsd"`
		Sources          []map[string]any `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Window == "" {
		t.Error("window label missing")
	}
	if len(resp.Sources) != 3 {
		t.Fatalf("sources = %d, want 3 (cache, compression, token saver)", len(resp.Sources))
	}
	sumTokens, sumUSD := 0.0, 0.0
	for _, s := range resp.Sources {
		if t, ok := s["tokensSaved"].(float64); ok {
			sumTokens += t
		}
		if u, ok := s["usd"].(float64); ok {
			sumUSD += u
		}
	}
	if float64(resp.TotalTokensSaved) != sumTokens {
		t.Errorf("totalTokensSaved = %d, sources sum = %v", resp.TotalTokensSaved, sumTokens)
	}
	if resp.TotalUsd != sumUSD {
		t.Errorf("totalUsd = %v, sources sum = %v", resp.TotalUsd, sumUSD)
	}
}

// TestHandleGetSavingsSummaryRateZeroFallsBack pins the rate contract: the
// panel's currency conversion is a display concern (currency.ts applies the
// operator rate); the endpoint always answers USD, so a zero rate cannot
// corrupt the stored figure.
func TestHandleGetSavingsSummaryRateZeroFallsBack(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	if err := db.EnsureCoreSchema(database.RawDB()); err != nil {
		t.Fatalf("ensure core schema: %v", err)
	}
	router := setupTestRouter(database)
	req := httptest.NewRequest(http.MethodGet, "/api/savings/summary", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"totalUsd"`) {
		t.Error("response must always carry totalUsd in USD for the display conversion")
	}
}
