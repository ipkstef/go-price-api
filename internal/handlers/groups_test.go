package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go-price-api/internal/models"
)

func TestGroupsRejectRetiredCurrentFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"is_current=true", "is_current=false", "is_current=", "is_current", "%69s_current=true"} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/groups?"+query, nil)
			// No database: rejection must happen before any query.
			(&GroupHandler{}).List(c)
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != 400 || body["error"] != "is_current has been removed; omit this parameter" {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGroupJSONHasOnlySupportedFields(t *testing.T) {
	b, err := json.Marshal(models.Group{GroupID: 2576})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 {
		t.Fatalf("unexpected fields: %s", b)
	}
	for _, key := range []string{"group_id", "name", "abbr"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
}
