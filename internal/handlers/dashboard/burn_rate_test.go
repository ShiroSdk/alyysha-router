// 9RouterX — this file is part of a port maintained by https://github.com/yanstd888/ — https://t.me/aiclub_id
package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"9routerx/proxy/internal/db"
)

func TestHandleGetBurnRate(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	if err := db.EnsureCoreSchema(database.RawDB()); err != nil {
		t.Fatal(err)
	}
	repo := database
	router := setupTestRouter(repo)

	// Seed usage
	for i := 0; i < 24; i++ {
		ts := time.Now().UTC().Add(-time.Duration(i) * time.Hour).Format("2006-01-02T15:04:05Z")
		if _, err := repo.RawDB().Exec(
			`INSERT INTO usageHistory (timestamp, provider, model, promptTokens, completionTokens, cost, status)
			 VALUES (?, 'deepseek', 'deepseek-chat', 1000, 100, 0.01, 'success')`, ts); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/burn-rate", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		WindowHours   int64   `json:"windowHours"`
		Tokens        int64   `json:"tokens"`
		CostUsd       float64 `json:"costUsd"`
		TokensPerHour float64 `json:"tokensPerHour"`
		CostPerHour   float64 `json:"costPerHourUsd"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.WindowHours != 24 {
		t.Errorf("window = %d, want 24", resp.WindowHours)
	}
	if resp.Tokens <= 0 || resp.CostUsd <= 0 {
		t.Errorf("tokens = %d, cost = %v; want positive from seeded data", resp.Tokens, resp.CostUsd)
	}
	if resp.TokensPerHour <= 0 {
		t.Errorf("tokensPerHour = %v, want positive", resp.TokensPerHour)
	}
}
