package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	setcatalog "go-price-api/data"
)

func TestParseSetReleasedSince(t *testing.T) {
	for input, want := range map[string]string{"2020": "2020-01-01", "2020-01-01": "2020-01-01", "2024-02-29": "2024-02-29"} {
		got, err := parseSetReleasedSince(input)
		if err != nil || got.Format(time.DateOnly) != want {
			t.Fatalf("parse %q = %v, %v", input, got, err)
		}
	}
}

func TestMoversInvalidSetCutoffReturnsExplicit400BeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, input := range []string{"", "20", "2020-1-01", "2020-01", "2023-02-29", "0000", " 2020", "tomorrow"} {
		t.Run(input, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/prices/movers?set_released_since="+url.QueryEscape(input), nil)
			(&PriceHandler{}).Movers(c)
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != 400 || body["error"] != "invalid set_released_since: use YYYY or YYYY-MM-DD with a valid date (years 0001-9999); YYYY means January 1" {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMoversCutoffWithNoEligibleGroupsReturnsEmptyArray(t *testing.T) {
	catalog, err := setcatalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/prices/movers?set_released_since=9999", nil)
	(&PriceHandler{SetCatalog: catalog}).Movers(c)
	if w.Code != 200 || w.Body.String() != "[]" {
		t.Fatalf("response: %d %s", w.Code, w.Body.String())
	}
}

func TestMoversRejectsMalformedEncodedAndRepeatedCutoffs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{
		"set_released_since=%ZZ",
		"set_released_since=2020&set_released_since=%ZZ",
		"%73et_released_since=%ZZ",
		"set_released_since=2020&set_released_since=2021",
	} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/prices/movers?"+query, nil)
			(&PriceHandler{}).Movers(c)
			if w.Code != 400 {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
