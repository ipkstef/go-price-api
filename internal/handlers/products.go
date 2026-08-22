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

func (h *ProductHandler) List(c *gin.Context) {
	limit, offset := parsePagination(c)
	wb := newWhereBuilder()

	if name := c.Query("name"); name != "" {
		wb.Add("clean_name", "ILIKE", "%"+name+"%")
	}
	if v := c.Query("group_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			wb.Add("group_id", "=", id)
		}
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
		wb.Add("is_sealed", "=", v == "true")
	}

	where := wb.SQL()

	var total int
	if err := h.DB.QueryRow(c.Request.Context(), "SELECT count(*) FROM products "+where, wb.args...).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT product_id, group_id, name, clean_name, image_url, url,
		        is_sealed, rarity_id, collector_number, subtype
		 FROM products %s
		 ORDER BY product_id
		 LIMIT $%d OFFSET $%d`,
		where, wb.NextArg(), wb.NextArg()+1,
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
		`SELECT product_id, group_id, name, clean_name, image_url, url, is_sealed,
		        upc, rarity_id, collector_number, subtype,
		        oracle_text_raw, oracle_text_plain,
		        presale_is_presale, presale_released_on,
		        extended_data, modified_on
		 FROM products WHERE product_id = $1`, id,
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

	rows, err := h.DB.Query(c.Request.Context(),
		`SELECT DISTINCT ON (s.sku_id) s.sku_id, s.language_id, s.printing_id, s.condition_id
		 FROM sku_price_snapshots s
		 JOIN ingestion_runs ir ON ir.ingestion_id = s.ingestion_id AND ir.status = 'complete'
		 WHERE s.product_id = $1
		 ORDER BY s.sku_id, s.snapshot_at DESC`, id,
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

	rows, err := h.DB.Query(c.Request.Context(),
		`SELECT DISTINCT ON (s.sku_id)
		        s.snapshot_at, s.sku_id, s.product_id,
		        s.language_id, s.printing_id, s.condition_id,
		        s.low_price_cents, s.mid_price_cents, s.high_price_cents,
		        s.market_price_cents, s.direct_low_price_cents
		 FROM sku_price_snapshots s
		 JOIN ingestion_runs ir ON ir.ingestion_id = s.ingestion_id AND ir.status = 'complete'
		 WHERE s.product_id = $1
		 ORDER BY s.sku_id, s.snapshot_at DESC`, id,
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
	wb.Add("s.product_id", "=", id)

	if v := c.Query("sku_id"); v != "" {
		if skuID, err := strconv.ParseInt(v, 10, 64); err == nil {
			wb.Add("s.sku_id", "=", skuID)
		}
	}
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.DateOnly, v); err == nil {
			wb.Add("s.snapshot_at", ">=", t)
		}
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
		 FROM sku_price_snapshots s
		 JOIN ingestion_runs ir ON ir.ingestion_id = s.ingestion_id AND ir.status = 'complete'
		 %s
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
