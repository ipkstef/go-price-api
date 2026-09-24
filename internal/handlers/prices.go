package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	setcatalog "go-price-api/data"
	"go-price-api/internal/models"
)

const (
	// A buffered response is materialised in full before it is written, so this
	// bounds server memory. A streamed one is bounded by the 1 MB request body.
	maxBufferedBulkIDs = 1000
	maxStreamedBulkIDs = 50000
)

type PriceHandler struct {
	DB         *pgxpool.Pool
	SetCatalog *setcatalog.Catalog
}

func (h *PriceHandler) BulkLatest(c *gin.Context) {
	var req models.BulkPriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.ProductIDs) == 0 && len(req.SKUIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provide product_ids or sku_ids"})
		return
	}

	// Buffered responses are held whole in memory before serialising, so they
	// stay modest; a streamed response never holds more than one row, so its
	// ceiling is set by the 1 MB request body instead (~450 KB at 50k ids).
	limit := maxBufferedBulkIDs
	if req.Stream {
		limit = maxStreamedBulkIDs
	}
	if len(req.ProductIDs) > limit || len(req.SKUIDs) > limit {
		msg := fmt.Sprintf("maximum %d ids per request", limit)
		if !req.Stream {
			msg += fmt.Sprintf("; set \"stream\": true for up to %d", maxStreamedBulkIDs)
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	var query string
	var args []any

	ctx := c.Request.Context()
	// as_of lets a caller read a past snapshot with the same keyed access path
	// as the latest one, which is what makes diffing two instants client-side
	// cheap: both reads are primary-key lookups scoped to the ids requested.
	snap, err := resolveSnapshot(ctx, h.DB, req.AsOf)
	if err != nil {
		if req.AsOf != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, make([]models.SKUPrice, 0))
		return
	}

	if len(req.SKUIDs) > 0 {
		// Keyed straight on the fact table's primary key; skus supplies the
		// identity columns the response carries.
		query = `SELECT s.snapshot_at, s.sku_id, sk.product_id,
		            sk.language_id, sk.printing_id, sk.condition_id,
		            s.low_price_cents, s.mid_price_cents, s.high_price_cents,
		            s.market_price_cents, s.direct_low_price_cents
		         FROM sku_price_snapshots s
		         JOIN skus sk ON sk.sku_id = s.sku_id
		         WHERE s.snapshot_at = $1 AND s.sku_id = ANY($2)`
		args = append(args, snap, req.SKUIDs)
	} else {
		// Resolve products to sku_ids first, then read the fact table by key.
		query = `SELECT s.snapshot_at, s.sku_id, sk.product_id,
		            sk.language_id, sk.printing_id, sk.condition_id,
		            s.low_price_cents, s.mid_price_cents, s.high_price_cents,
		            s.market_price_cents, s.direct_low_price_cents
		         FROM skus sk
		         JOIN sku_price_snapshots s ON s.sku_id = sk.sku_id
		         WHERE sk.product_id = ANY($2) AND s.snapshot_at = $1`
		args = append(args, snap, req.ProductIDs)
	}

	rows, err := h.DB.Query(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	if req.Stream {
		streamBulkPrices(c, rows)
		return
	}

	prices := make([]models.SKUPrice, 0)
	for rows.Next() {
		p, err := scanSKUPrice(rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		prices = append(prices, p)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query interrupted"})
		return
	}

	c.JSON(http.StatusOK, prices)
}

func scanSKUPrice(rows pgx.Rows) (models.SKUPrice, error) {
	var p models.SKUPrice
	err := rows.Scan(
		&p.SnapshotAt, &p.SKUID, &p.ProductID,
		&p.LanguageID, &p.PrintingID, &p.ConditionID,
		&p.LowPriceCents, &p.MidPriceCents, &p.HighPriceCents,
		&p.MarketPriceCents, &p.DirectLowPriceCents,
	)
	return p, err
}

// streamBulkPrices writes one JSON object per line as rows arrive, so neither
// the server nor the client holds the whole result.
//
// The status line goes out before the first row, so a failure partway through
// cannot be reported as an HTTP status. It is reported as a final object
// carrying an "error" key instead: a consumer must treat a line with "error"
// as a failed request, and must not assume a truncated stream is a short one.
func streamBulkPrices(c *gin.Context, rows pgx.Rows) {
	c.Writer.Header().Set("Content-Type", "application/x-ndjson")
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	c.Writer.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(c.Writer)
	written := 0
	for rows.Next() {
		p, err := scanSKUPrice(rows)
		if err != nil {
			_ = enc.Encode(gin.H{"error": "scan failed"})
			c.Writer.Flush()
			return
		}
		if err := enc.Encode(p); err != nil {
			// The client hung up. Nothing useful left to say on this connection.
			return
		}
		written++
		// Flush periodically rather than per row: the caller wants to start work
		// early, not to pay a syscall for every price.
		if written%500 == 0 {
			c.Writer.Flush()
		}
	}
	if err := rows.Err(); err != nil {
		_ = enc.Encode(gin.H{"error": "query interrupted"})
	}
	c.Writer.Flush()
}
