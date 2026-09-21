package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	setcatalog "go-price-api/data"
	"go-price-api/internal/models"
)

type integrationQueryTracer struct{ t *testing.T }

func (tr integrationQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (tr integrationQueryTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil {
		tr.t.Logf("database query error: %v", data.Err)
	}
}

// Opt-in, read-only integration test against representative existing snapshots.
func TestMoversLiveSetFilter(t *testing.T) {
	uri := os.Getenv("MOVERS_TEST_DATABASE_URL")
	if uri == "" {
		t.Skip("set MOVERS_TEST_DATABASE_URL for read-only database integration checks")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(uri)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "app,public"
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	// Explicit from/to dates resolve to a non-adjacent pair, which the loader
	// never stores as a single row. Such a window is now answered by composing
	// the adjacent-pair chain in sku_price_changes, so these cases exercise the
	// chained path rather than the live full-snapshot comparison.
	//
	// The live path remains the fallback whenever that chain has a gap, and it
	// was measured at 75-100s against production. The timeout stays generous
	// enough to survive a fallback rather than masking one as a failure.
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "180000"
	cfg.ConnConfig.Tracer = integrationQueryTracer{t: t}
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	catalog, err := setcatalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	h := &PriceHandler{DB: db, SetCatalog: catalog}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/prices/movers", h.Movers)
	var from, to time.Time
	// Explicit from/to parameters select whole UTC dates, so use the last
	// completed snapshot from each of two distinct days.
	if err := db.QueryRow(ctx, `SELECT min(snapshot_at),max(snapshot_at) FROM (SELECT max(snapshot_at) AS snapshot_at FROM ingestion_runs WHERE status='complete' GROUP BY (snapshot_at AT TIME ZONE 'UTC')::date ORDER BY snapshot_at DESC LIMIT 2) s`).Scan(&from, &to); err != nil {
		t.Fatal(err)
	}
	if !from.Before(to) {
		t.Fatal("need completed snapshots on two distinct UTC dates")
	}
	base := "/prices/movers?from=" + from.Format(time.DateOnly) + "&to=" + to.Format(time.DateOnly) + "&limit=20"
	request := func(t *testing.T, query string) []models.PriceMover {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", base+query, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
		}
		var results []models.PriceMover
		if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
			t.Fatal(err)
		}
		for _, m := range results {
			if !m.PreviousSnapshotAt.Equal(from) || !m.CurrentSnapshotAt.Equal(to) {
				t.Fatalf("SKU %d snapshot dates = %s -> %s, want %s -> %s", m.SKUID, m.PreviousSnapshotAt, m.CurrentSnapshotAt, from, to)
			}
		}
		return results
	}
	t.Run("snapshot timestamps", func(t *testing.T) {
		results := request(t, "&group_id=24219&language_id=1&printing_id=2&condition_id=1")
		if len(results) == 0 {
			t.Fatal("need a nonempty page to verify timestamps")
		}
		t.Logf("verified %d results with snapshots %s -> %s", len(results), results[0].PreviousSnapshotAt, results[0].CurrentSnapshotAt)
	})
	t.Run("set filters", func(t *testing.T) {
		cutoff, _ := time.Parse(time.DateOnly, "2020-01-01")
		eligible := map[int64]bool{}
		for _, id := range catalog.GroupsReleasedSince(cutoff) {
			eligible[id] = true
		}
		for _, query := range []string{"", "&set_released_since=2020", "&set_released_since=2020-01-01", "&set_released_since=2020&direction=down&is_sealed=false&language_id=1&printing_id=1&condition_id=1&min_price=500", "&set_released_since=2020&offset=20"} {
			results := request(t, query)
			if len(results) != 20 {
				t.Fatalf("%s returned %d results, expected full page with representative data", query, len(results))
			}
			// These queries omit price_type, so they compare low prices.
			const priceColumn = "low_price_cents"
			ids := make([]int64, 0, len(results))
			skus := make([]int64, 0, len(results))
			for _, m := range results {
				ids = append(ids, m.ProductID)
				skus = append(skus, m.SKUID)
			}
			rows, err := db.Query(ctx, `SELECT product_id,group_id FROM products WHERE product_id=ANY($1)`, ids)
			if err != nil {
				t.Fatal(err)
			}
			groups := map[int64]int64{}
			for rows.Next() {
				var pid, gid int64
				if err := rows.Scan(&pid, &gid); err != nil {
					t.Fatal(err)
				}
				groups[pid] = gid
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows, err = db.Query(ctx, `SELECT f.sku_id,f.`+priceColumn+`,t.`+priceColumn+` FROM sku_price_snapshots f JOIN sku_price_snapshots t USING(sku_id) WHERE f.sku_id=ANY($1) AND f.snapshot_at=$2 AND t.snapshot_at=$3`, skus, from, to)
			if err != nil {
				t.Fatal(err)
			}
			prices := map[int64][2]int32{}
			for rows.Next() {
				var sku int64
				var previous, current int32
				if err := rows.Scan(&sku, &previous, &current); err != nil {
					t.Fatal(err)
				}
				prices[sku] = [2]int32{previous, current}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			for _, m := range results {
				gid, ok := groups[m.ProductID]
				if !ok || query != "" && !eligible[gid] {
					t.Fatalf("%s returned ineligible product %d group %d", query, m.ProductID, gid)
				}
				price, ok := prices[m.SKUID]
				if !ok {
					t.Fatalf("missing database prices for SKU %d", m.SKUID)
				}
				previous, current := price[0], price[1]
				// Prices and deltas are nullable by contract; this fixture only
				// selects SKUs priced in both snapshots, so all three must be set.
				if m.PreviousPriceCents == nil || m.CurrentPriceCents == nil || m.PriceChangeCents == nil {
					t.Fatalf("unexpected null price or delta for SKU %d: %+v", m.SKUID, m)
				}
				if *m.PreviousPriceCents != previous || *m.CurrentPriceCents != current || *m.PriceChangeCents != current-previous {
					t.Fatalf("incorrect prices for SKU %d: %+v; database %d -> %d", m.SKUID, m, previous, current)
				}
				if m.ChangeType != "changed" {
					t.Fatalf("expected change_type 'changed' for SKU %d, got %q", m.SKUID, m.ChangeType)
				}
			}
			t.Logf("%s: %d results; first SKU %d, %d -> %d cents", query, len(results), results[0].SKUID, *results[0].PreviousPriceCents, *results[0].CurrentPriceCents)
		}
	})

	t.Run("price type", func(t *testing.T) {
		// Scoped to one group on purpose. These explicit from/to dates resolve to
		// a non-adjacent pair, which the loader never stores as a single row, so
		// this exercises the chained path -- or the live fallback if the chain
		// has a gap. The group filter matters for the fallback case: unfiltered,
		// that scan compares two whole snapshots and outruns the timeout.
		const group = "&group_id=24219&condition_id=1"
		for _, tc := range []struct{ priceType, column string }{
			{"low", "low_price_cents"},
			{"market", "market_price_cents"},
		} {
			results := request(t, group+"&price_type="+tc.priceType)
			if len(results) == 0 {
				t.Fatalf("price_type=%s returned no rows; need representative data", tc.priceType)
			}
			skus := make([]int64, 0, len(results))
			for _, m := range results {
				skus = append(skus, m.SKUID)
			}
			rows, err := db.Query(ctx, `SELECT f.sku_id,f.`+tc.column+`,t.`+tc.column+
				` FROM sku_price_snapshots f JOIN sku_price_snapshots t USING(sku_id)`+
				` WHERE f.sku_id=ANY($1) AND f.snapshot_at=$2 AND t.snapshot_at=$3`, skus, from, to)
			if err != nil {
				t.Fatal(err)
			}
			prices := map[int64][2]*int32{}
			for rows.Next() {
				var sku int64
				var previous, current *int32
				if err := rows.Scan(&sku, &previous, &current); err != nil {
					t.Fatal(err)
				}
				prices[sku] = [2]*int32{previous, current}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			for _, m := range results {
				price, ok := prices[m.SKUID]
				if !ok {
					t.Fatalf("price_type=%s: missing database row for SKU %d", tc.priceType, m.SKUID)
				}
				// The response must report the price field that was requested,
				// not whichever one the delta table happened to store.
				if !equalPtr(m.PreviousPriceCents, price[0]) || !equalPtr(m.CurrentPriceCents, price[1]) {
					t.Fatalf("price_type=%s SKU %d: %+v does not match database %v -> %v",
						tc.priceType, m.SKUID, m, price[0], price[1])
				}
			}
			t.Logf("price_type=%s: verified %d results against %s", tc.priceType, len(results), tc.column)
		}
		for _, query := range []string{
			"&set_released_since=2020&group_id=7",    // old
			"&set_released_since=2020&group=LEA",     // existing code resolver
			"&set_released_since=2020&group_id=9",    // unmapped
			"&set_released_since=2020&group_id=2422", // shared: 2019 and 2021
			"&set_released_since=9999",
		} {
			if got := request(t, query); len(got) != 0 {
				t.Fatalf("%s returned %d results", query, len(got))
			}
		}
	})
}

// equalPtr compares two nullable prices, treating null as a distinct value
// rather than coercing it to zero.
func equalPtr(a, b *int32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
