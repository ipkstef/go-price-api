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
					"name":   "search by set name (ILIKE)",
					"limit":  "1-200 (default 50)",
					"offset": "pagination offset",
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
				"description": "Bulk price lookup by product or SKU IDs. Reads one snapshot, keyed on the price table's primary key, so cost scales with the ids requested rather than the size of the table. Every row echoes the snapshot_at it came from. Call twice with different as_of values to compute price movement for the SKUs you care about.",
				"body": gin.H{
					"product_ids": "[]int64 — prices for all SKUs of these products",
					"sku_ids":     "[]int64 — prices for specific SKUs",
					"as_of":       "string — YYYY-MM-DD or RFC3339; returns the newest complete snapshot at or before it. Omit for the latest. A bare date includes snapshots taken during that day. 400 if nothing exists at or before it.",
					"stream":      "bool — newline-delimited JSON, one row per line; raises the id cap from 1000 to 50000",
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
