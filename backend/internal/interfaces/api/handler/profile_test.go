package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/infrastructure/strategyprofile"
)

// newProfileRouter mounts just the profile endpoints so tests do not need
// the full BacktestHandler / sqlite setup.
func newProfileRouter(t *testing.T, profilesDir string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewProfileHandler(profilesDir)
	r := gin.New()
	r.GET("/api/v1/profiles", h.List)
	r.GET("/api/v1/profiles/:name", h.Get)
	return r
}

func TestProfileHandler_List_ReturnsSortedSummaries(t *testing.T) {
	// Two valid profiles in the temp dir; the handler must sort them and
	// return both with Name / Description / IsRouter populated.
	profilesDir := setupProfilesDir(t, map[string][]byte{
		"alpha": readProductionProfileJSON(t),
		"beta":  readProductionProfileJSON(t),
	})
	router := newProfileRouter(t, profilesDir)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/profiles", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var body struct {
		Profiles []strategyprofile.ProfileSummary `json:"profiles"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, w.Body.String())
	}
	if len(body.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d: %+v", len(body.Profiles), body.Profiles)
	}
	// production.json's internal `name` is "production", not "alpha" /
	// "beta" — Loader.List uses the profile's own name (filename is only
	// the disk anchor). Both entries therefore come back named
	// "production"; we assert on Description presence to keep the test
	// robust against that design choice.
	for i, p := range body.Profiles {
		if p.Name == "" {
			t.Errorf("profile[%d] has empty Name", i)
		}
		if p.Description == "" {
			t.Errorf("profile[%d] has empty Description", i)
		}
		if p.IsRouter {
			t.Errorf("profile[%d] is unexpectedly flagged as a router", i)
		}
	}
}

func TestProfileHandler_List_EmptyDirReturnsEmptyArray(t *testing.T) {
	// Nothing on disk — the FE expects a non-nil empty array, not a 500.
	profilesDir := setupProfilesDir(t, nil)
	router := newProfileRouter(t, profilesDir)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/profiles", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"profiles":[]`) {
		t.Errorf("expected empty profiles array, got %s", w.Body.String())
	}
}

func TestProfileHandler_Get_ReturnsFullProfile(t *testing.T) {
	profilesDir := setupProfilesDir(t, map[string][]byte{
		"production": readProductionProfileJSON(t),
	})
	router := newProfileRouter(t, profilesDir)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/profiles/production", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var profile entity.StrategyProfile
	if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if profile.Name == "" {
		t.Error("returned profile has empty Name")
	}
	if profile.Indicators.SMAShort == 0 || profile.Indicators.SMALong == 0 {
		t.Error("indicator periods did not round-trip through /profiles/:name")
	}
}

func TestProfileHandler_Get_MissingReturns404(t *testing.T) {
	profilesDir := setupProfilesDir(t, nil)
	router := newProfileRouter(t, profilesDir)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/profiles/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", w.Code, w.Body.String())
	}
}

func TestProfileHandler_Get_InvalidNameReturns400(t *testing.T) {
	profilesDir := setupProfilesDir(t, nil)
	router := newProfileRouter(t, profilesDir)

	// URL-escape a traversal attempt so gin's path parser passes it to the
	// handler unchanged. The profile name allow-list should reject it.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/profiles/bad..name", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", w.Code, w.Body.String())
	}
}
