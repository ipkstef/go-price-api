package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type DocsHandler struct{}

func (h *DocsHandler) Index(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name":        "go-price-api",
		"description": "TCGPlayer card price data API backed by TimescaleDB",
		"auth":        "JWT Bearer token. Register via POST /auth/register, then pass Authorization: Bearer <token>",
		"endpoints": []gin.H{
			{
				"method":      "POST",
				"path":        "/auth/register",
				"auth":        false,
				"description": "Create a new user account and receive a JWT",
				"body":        gin.H{"username": "string (required)", "password": "string (required, min 8 chars)"},
			},
			{
				"method":      "POST",
				"path":        "/auth/login",
				"auth":        false,
				"description": "Authenticate and receive a JWT",
				"body":        gin.H{"username": "string", "password": "string"},
			},
			{
				"method":      "POST",
				"path":        "/auth/refresh",
				"auth":        true,
				"description": "Refresh the current JWT",
			},
			{
				"method":      "GET",
				"path":        "/products",
				"auth":        true,
				"description": "List and search products (cards)",
				"params": gin.H{
					"name":      "search by card name (ILIKE)",
					"group_id":  "filter by set",
					"rarity_id": "filter by rarity",
					"subtype":   "filter by subtype (ILIKE)",
					"is_sealed": "true/false",
					"limit":     "1-200 (default 50)",
					"offset":    "pagination offset (default 0)",
				},
			},
			{
				"method":      "GET",
				"path":        "/products/:id",
				"auth":        true,
				"description": "Get full product detail including extended data",
			},
			{
				"method":      "GET",
				"path":        "/products/:id/skus",
				"auth":        true,
				"description": "List all SKU variants (language × printing × condition) for a product",
			},
			{
				"method":      "GET",
				"path":        "/products/:id/prices",
				"auth":        true,
				"description": "Latest prices for all SKUs of a product",
			},
			{
				"method":      "GET",
				"path":        "/products/:id/prices/history",
				"auth":        true,
				"description": "Price history over time using TimescaleDB time_bucket aggregation",
				"params": gin.H{
					"interval": "1 hour, 6 hours, 12 hours, 1 day, 7 days, 30 days (default: 1 day)",
					"sku_id":   "filter to a specific SKU",
					"from":     "start date (YYYY-MM-DD)",
					"to":       "end date (YYYY-MM-DD)",
				},
			},
			{
				"method":      "GET",
				"path":        "/groups",
				"auth":        true,
				"description": "List and search card sets",
				"params": gin.H{
					"name":       "search by set name (ILIKE)",
					"is_current": "true/false",
					"limit":      "1-200 (default 50)",
					"offset":     "pagination offset",
				},
			},
			{
				"method":      "GET",
				"path":        "/groups/:id",
				"auth":        true,
				"description": "Get a single set by ID",
			},
			{
				"method":      "GET",
				"path":        "/groups/:id/products",
				"auth":        true,
				"description": "List all products in a set (paginated)",
			},
			{
				"method":      "POST",
				"path":        "/prices/latest",
				"auth":        true,
				"description": "Bulk lookup of latest prices by product or SKU IDs (max 500)",
				"body": gin.H{
					"product_ids": "[]int64 — get latest prices for all SKUs of these products",
					"sku_ids":     "[]int64 — get latest prices for specific SKUs",
				},
			},
			{
				"method":      "GET",
				"path":        "/prices/movers",
				"auth":        true,
				"description": "SKUs whose price moved between two completed snapshots, ranked by delta. One price field per request, chosen with price_type. Includes language, printing, and condition names. Each result echoes price_type and change_type, plus previous_snapshot_at and current_snapshot_at as UTC RFC 3339 timestamps of the actual resolved snapshots, not individual listing-change times or the requested dates. Defaults to the two most recent completed snapshots. Prices are reported as previous_price_cents and current_price_cents, with price_change_cents and price_change_percent, all naming the requested price_type rather than a fixed field. These are nullable: a SKU present in only one snapshot, or whose price appeared or disappeared, has no defined delta, and a missing price is never coerced to zero. Null changes sort after every ranked mover. max_price is not implemented and is ignored.",
				"errors": gin.H{
					"400 (empty, repeated, malformed, or impossible set_released_since)": "invalid set_released_since: use YYYY or YYYY-MM-DD with a valid date (years 0001-9999); YYYY means January 1",
					"400 (unknown direction)":   "direction must be 'up' or 'down'",
					"400 (unknown sort_by)":     "sort_by must be 'cents' or 'percent'",
					"400 (unknown price_type)":  "price_type must be 'low' or 'market'",
					"400 (unknown change_type)": "change_type must be a comma-separated list of: changed, new, removed, price_added, price_removed",
				},
				"params": gin.H{
					"from":               "start date YYYY-MM-DD (resolves to latest completed snapshot at or before this date)",
					"to":                 "end date YYYY-MM-DD (same resolution)",
					"direction":          "up or down (default: up)",
					"price_type":         "low (default) or market. Exactly one value; a list is rejected so a SKU cannot occupy two ranks in one response.",
					"sort_by":            "cents (default) or percent",
					"change_type":        "comma-separated subset of changed, new, removed, price_added, price_removed. Each is a statement about the requested price field, not the SKU as a whole. Omit for all.",
					"min_price":          "minimum price on both sides, in cents (filters noise)",
					"is_sealed":          "true/false",
					"group_id":           "filter to a specific set by TCGplayer group ID",
					"group":              "filter to a specific set by code, e.g. LTR (case-insensitive)",
					"set_released_since": "inclusive set release cutoff: YYYY or YYYY-MM-DD; YYYY means January 1. Excludes unmapped groups; uses the earliest release for shared TCGplayer groups. Combined with other filters before ranking and pagination. Omit to keep all sets.",
					"language_id":        "filter to a specific language",
					"printing_id":        "filter to a specific printing (1=Normal, 2=Foil)",
					"condition_id":       "filter to a specific condition (1=Near Mint, etc.)",
					"limit":              "1-200 (default 50)",
					"offset":             "pagination offset (default 0)",
				},
			},
			{
				"method":      "GET",
				"path":        "/conditions",
				"auth":        true,
				"description": "List card conditions (Near Mint, Lightly Played, etc.)",
			},
			{
				"method":      "GET",
				"path":        "/languages",
				"auth":        true,
				"description": "List available languages",
			},
			{
				"method":      "GET",
				"path":        "/printings",
				"auth":        true,
				"description": "List printing types (Normal, Foil)",
			},
			{
				"method":      "GET",
				"path":        "/rarities",
				"auth":        true,
				"description": "List card rarities",
			},
			{
				"method":      "GET",
				"path":        "/ingestion/runs",
				"auth":        true,
				"description": "List data ingestion runs",
				"params": gin.H{
					"status": "filter by status (loading, complete, error)",
					"limit":  "1-200 (default 50)",
					"offset": "pagination offset",
				},
			},
			{
				"method":      "GET",
				"path":        "/ingestion/runs/:id",
				"auth":        true,
				"description": "Get detail for a single ingestion run",
			},
		},
	})
}
