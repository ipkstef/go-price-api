package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-price-api/internal/models"
)

type ProductHandler struct {
	DB *pgxpool.Pool
}

// sealedExpr derives is_sealed from SKU conditions instead of reading a stored
// column, so it can never be stale. Condition 6 is Unopened. Served by
// skus_condition_product_idx as an index-only probe, so it stays cheap both as
// a projected column (one probe per returned row) and as a filter.
const sealedExpr = `EXISTS (SELECT 1 FROM skus sk
	WHERE sk.product_id = p.product_id AND sk.condition_id = 6)`

func (h *ProductHandler) List(c *gin.Context) {
	limit, offset := parsePagination(c)
	wb := newWhereBuilder()

	if name := c.Query("name"); name != "" {
		wb.Add("clean_name", "ILIKE", "%"+name+"%")
	}
	if gid, ok, err := resolveGroupID(c, h.DB); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else if ok {
		wb.Add("group_id", "=", gid)
	}
	if v := c.Query("rarity_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 16); err == nil {
			wb.Add("rarity_id", "=", int16(id))
		}
	}
	if v := c.Query("subtype"); v != "" {
		wb.Add("subtype", "ILIKE", "%"+v+"%")
	}
	if v := c.Query("is_sealed"); v != "" {
		wb.Add(sealedExpr, "=", v == "true")
	}

	where := wb.SQL()

	var total int
	if err := h.DB.QueryRow(c.Request.Context(), "SELECT count(*) FROM products p "+where, wb.args...).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT p.product_id, p.group_id, p.name, p.clean_name, p.image_url, p.url,
		        %s, p.rarity_id, p.collector_number, p.subtype
		 FROM products p %s
		 ORDER BY p.product_id
		 LIMIT $%d OFFSET $%d`,
		sealedExpr, where, wb.NextArg(), wb.NextArg()+1,
	)

	rows, err := h.DB.Query(c.Request.Context(), query, wb.Args(limit, offset)...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	products := make([]models.ProductSummary, 0)
	for rows.Next() {
		var p models.ProductSummary
		if err := rows.Scan(
			&p.ProductID, &p.GroupID, &p.Name, &p.CleanName,
			&p.ImageURL, &p.URL, &p.IsSealed,
			&p.RarityID, &p.CollectorNumber, &p.Subtype,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query interrupted"})
		return
	}

	c.JSON(http.StatusOK, models.PaginatedResponse{
		Data:       products,
		Pagination: models.Pagination{Limit: limit, Offset: offset, Total: total},
	})
}

func (h *ProductHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id"})
		return
	}

	var p models.Product
	var extData *string
	var presaleDate *time.Time
	err = h.DB.QueryRow(c.Request.Context(),
		fmt.Sprintf(`SELECT p.product_id, p.group_id, p.name, p.clean_name, p.image_url, p.url, %s,
		        p.upc, p.rarity_id, p.collector_number, p.subtype,
		        p.oracle_text_raw, p.oracle_text_plain,
		        p.presale_is_presale, p.presale_released_on,
		        p.extended_data, p.modified_on
		 FROM products p WHERE p.product_id = $1`, sealedExpr), id,
	).Scan(
		&p.ProductID, &p.GroupID, &p.Name, &p.CleanName,
		&p.ImageURL, &p.URL, &p.IsSealed,
		&p.UPC, &p.RarityID, &p.CollectorNumber, &p.Subtype,
		&p.OracleTextRaw, &p.OracleTextPlain,
		&p.PresaleIsPresale, &presaleDate,
		&extData, &p.ModifiedOn,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		}
		return
	}

	p.PresaleReleasedOn = presaleDate
	if extData != nil {
		p.ExtendedData = json.RawMessage(*extData)
	}

	c.JSON(http.StatusOK, p)
}

func (h *ProductHandler) SKUs(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id"})
		return
	}

	// A product's SKUs are catalog facts, so this no longer depends on a
	// snapshot existing: the answer is the same before any prices are loaded.
	ctx := c.Request.Context()

	rows, err := h.DB.Query(ctx,
		`SELECT sk.sku_id, sk.language_id, sk.printing_id, sk.condition_id
		 FROM skus sk
		 WHERE sk.product_id = $1
		 ORDER BY sk.sku_id`, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	skus := make([]models.SKU, 0)
	for rows.Next() {
		var s models.SKU
		if err := rows.Scan(&s.SKUID, &s.LanguageID, &s.PrintingID, &s.ConditionID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		skus = append(skus, s)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query interrupted"})
		return
	}

	c.JSON(http.StatusOK, skus)
}

func (h *ProductHandler) Prices(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id"})
		return
	}

	ctx := c.Request.Context()
	snap, err := latestCompleteSnapshot(ctx, h.DB)
	if err != nil {
		c.JSON(http.StatusOK, make([]models.SKUPrice, 0))
		return
	}

	rows, err := h.DB.Query(ctx,
		`SELECT s.snapshot_at, s.sku_id, sk.product_id,
		        sk.language_id, sk.printing_id, sk.condition_id,
		        s.low_price_cents, s.mid_price_cents, s.high_price_cents,
		        s.market_price_cents, s.direct_low_price_cents
		 FROM skus sk
		 JOIN sku_price_snapshots s ON s.sku_id = sk.sku_id
		 WHERE sk.product_id = $1 AND s.snapshot_at = $2`, id, snap,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	prices := make([]models.SKUPrice, 0)
	for rows.Next() {
		var p models.SKUPrice
		if err := rows.Scan(
			&p.SnapshotAt, &p.SKUID, &p.ProductID,
			&p.LanguageID, &p.PrintingID, &p.ConditionID,
			&p.LowPriceCents, &p.MidPriceCents, &p.HighPriceCents,
			&p.MarketPriceCents, &p.DirectLowPriceCents,
		); err != nil {
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

func (h *ProductHandler) PriceHistory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id"})
		return
	}

	interval := c.DefaultQuery("interval", "1 day")
	validIntervals := map[string]bool{
		"1 hour": true, "6 hours": true, "12 hours": true,
		"1 day": true, "7 days": true, "30 days": true,
	}
	if !validIntervals[interval] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":           "invalid interval",
			"valid_intervals": []string{"1 hour", "6 hours", "12 hours", "1 day", "7 days", "30 days"},
		})
		return
	}

	wb := newWhereBuilder()
	wb.Add("sk.product_id", "=", id)

	if v := c.Query("sku_id"); v != "" {
		if skuID, err := strconv.ParseInt(v, 10, 64); err == nil {
			wb.Add("s.sku_id", "=", skuID)
		}
	}
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.DateOnly, v); err == nil {
			wb.Add("s.snapshot_at", ">=", t)
		}
	} else {
		wb.Add("s.snapshot_at", ">=", time.Now().UTC().AddDate(0, 0, -30))
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.DateOnly, v); err == nil {
			wb.Add("s.snapshot_at", "<=", t.AddDate(0, 0, 1))
		}
	}

	query := fmt.Sprintf(
		`SELECT time_bucket('%s', s.snapshot_at) AS bucket,
		        avg(s.low_price_cents)    AS avg_low,
		        avg(s.mid_price_cents)    AS avg_mid,
		        avg(s.high_price_cents)   AS avg_high,
		        avg(s.market_price_cents) AS avg_market
		 FROM skus sk
		 JOIN sku_price_snapshots s ON s.sku_id = sk.sku_id
		 %s
		   AND s.snapshot_at IN (SELECT snapshot_at FROM ingestion_runs WHERE status = 'complete')
		 GROUP BY bucket
		 ORDER BY bucket`,
		interval, wb.SQL(),
	)

	rows, err := h.DB.Query(c.Request.Context(), query, wb.args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	points := make([]models.PricePoint, 0)
	for rows.Next() {
		var pp models.PricePoint
		if err := rows.Scan(
			&pp.Bucket, &pp.AvgLowPriceCents, &pp.AvgMidPriceCents,
			&pp.AvgHighPriceCents, &pp.AvgMarketPriceCents,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		points = append(points, pp)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query interrupted"})
		return
	}

	c.JSON(http.StatusOK, points)
}
