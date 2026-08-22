package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-price-api/internal/models"
)

type PriceHandler struct {
	DB *pgxpool.Pool
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

	if len(req.ProductIDs) > 500 || len(req.SKUIDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "maximum 500 ids per request"})
		return
	}

	var query string
	var args []any

	if len(req.SKUIDs) > 0 {
		query = `SELECT DISTINCT ON (s.sku_id)
		            s.snapshot_at, s.sku_id, s.product_id,
		            s.language_id, s.printing_id, s.condition_id,
		            s.low_price_cents, s.mid_price_cents, s.high_price_cents,
		            s.market_price_cents, s.direct_low_price_cents
		         FROM sku_price_snapshots s
		         JOIN ingestion_runs ir ON ir.ingestion_id = s.ingestion_id AND ir.status = 'complete'
		         WHERE s.sku_id = ANY($1)
		         ORDER BY s.sku_id, s.snapshot_at DESC`
		args = append(args, req.SKUIDs)
	} else {
		query = `SELECT DISTINCT ON (s.sku_id)
		            s.snapshot_at, s.sku_id, s.product_id,
		            s.language_id, s.printing_id, s.condition_id,
		            s.low_price_cents, s.mid_price_cents, s.high_price_cents,
		            s.market_price_cents, s.direct_low_price_cents
		         FROM sku_price_snapshots s
		         JOIN ingestion_runs ir ON ir.ingestion_id = s.ingestion_id AND ir.status = 'complete'
		         WHERE s.product_id = ANY($1)
		         ORDER BY s.sku_id, s.snapshot_at DESC`
		args = append(args, req.ProductIDs)
	}

	rows, err := h.DB.Query(c.Request.Context(), query, args...)
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

func (h *PriceHandler) Movers(c *gin.Context) {
	limit, offset := parsePagination(c)
	direction := c.DefaultQuery("direction", "up")
	sortBy := c.DefaultQuery("sort_by", "cents")

	if direction != "up" && direction != "down" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "direction must be 'up' or 'down'"})
		return
	}
	if sortBy != "cents" && sortBy != "percent" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sort_by must be 'cents' or 'percent'"})
		return
	}

	ctx := c.Request.Context()
	var fromSnap, toSnap time.Time

	if v := c.Query("from"); v != "" {
		fromDate, err := time.Parse(time.DateOnly, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date, use YYYY-MM-DD"})
			return
		}
		err = h.DB.QueryRow(ctx,
			`SELECT snapshot_at FROM ingestion_runs
			 WHERE status = 'complete' AND snapshot_at < $1
			 ORDER BY snapshot_at DESC LIMIT 1`,
			fromDate.AddDate(0, 0, 1)).Scan(&fromSnap)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "no completed snapshot at or before 'from' date"})
			return
		}
	}

	if v := c.Query("to"); v != "" {
		toDate, err := time.Parse(time.DateOnly, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to date, use YYYY-MM-DD"})
			return
		}
		err = h.DB.QueryRow(ctx,
			`SELECT snapshot_at FROM ingestion_runs
			 WHERE status = 'complete' AND snapshot_at < $1
			 ORDER BY snapshot_at DESC LIMIT 1`,
			toDate.AddDate(0, 0, 1)).Scan(&toSnap)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "no completed snapshot at or before 'to' date"})
			return
		}
	}

	if fromSnap.IsZero() && toSnap.IsZero() {
		rows, err := h.DB.Query(ctx,
			`SELECT DISTINCT snapshot_at FROM ingestion_runs
			 WHERE status = 'complete'
			 ORDER BY snapshot_at DESC LIMIT 2`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		var snaps []time.Time
		for rows.Next() {
			var t time.Time
			if err := rows.Scan(&t); err != nil {
				rows.Close()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
				return
			}
			snaps = append(snaps, t)
		}
		rows.Close()

		if len(snaps) < 2 {
			c.JSON(http.StatusOK, make([]models.PriceMover, 0))
			return
		}
		toSnap = snaps[0]
		fromSnap = snaps[1]
	} else if fromSnap.IsZero() {
		err := h.DB.QueryRow(ctx,
			`SELECT snapshot_at FROM ingestion_runs
			 WHERE status = 'complete' AND snapshot_at < $1
			 ORDER BY snapshot_at DESC LIMIT 1`, toSnap).Scan(&fromSnap)
		if err != nil {
			c.JSON(http.StatusOK, make([]models.PriceMover, 0))
			return
		}
	} else if toSnap.IsZero() {
		err := h.DB.QueryRow(ctx,
			`SELECT snapshot_at FROM ingestion_runs
			 WHERE status = 'complete'
			 ORDER BY snapshot_at DESC LIMIT 1`).Scan(&toSnap)
		if err != nil {
			c.JSON(http.StatusOK, make([]models.PriceMover, 0))
			return
		}
	}

	if !fromSnap.Before(toSnap) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "'from' snapshot must be before 'to' snapshot"})
		return
	}

	var changeTypes []string
	if v := c.Query("change_type"); v != "" {
		valid := map[string]bool{"changed": true, "new": true, "removed": true, "price_added": true, "price_removed": true}
		changeTypes = strings.Split(v, ",")
		for _, t := range changeTypes {
			if !valid[t] {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":       "invalid change_type value: " + t,
					"valid_types": []string{"changed", "new", "removed", "price_added", "price_removed"},
				})
				return
			}
		}
	}

	orderDir := "DESC"
	if direction == "down" {
		orderDir = "ASC"
	}

	orderExpr := "market_delta"
	if sortBy == "percent" {
		orderExpr = "market_delta_pct"
	}

	queryArgs := []any{fromSnap, toSnap}
	argN := 3

	var outerClauses []string
	if len(changeTypes) > 0 {
		outerClauses = append(outerClauses, fmt.Sprintf("c.change_type = ANY($%d::text[])", argN))
		queryArgs = append(queryArgs, changeTypes)
		argN++
	}
	if v := c.Query("is_sealed"); v != "" {
		outerClauses = append(outerClauses, fmt.Sprintf("p.is_sealed = $%d", argN))
		queryArgs = append(queryArgs, v == "true")
		argN++
	}
	if v := c.Query("group_id"); v != "" {
		if gid, err := strconv.ParseInt(v, 10, 64); err == nil {
			outerClauses = append(outerClauses, fmt.Sprintf("p.group_id = $%d", argN))
			queryArgs = append(queryArgs, gid)
			argN++
		}
	}

	outerWhere := ""
	if len(outerClauses) > 0 {
		outerWhere = "WHERE " + strings.Join(outerClauses, " AND ")
	}

	limitClause := fmt.Sprintf("LIMIT $%d OFFSET $%d", argN, argN+1)
	queryArgs = append(queryArgs, limit, offset)

	query := fmt.Sprintf(`
		WITH from_prices AS (
			SELECT sku_id, product_id,
			       low_price_cents, mid_price_cents, high_price_cents,
			       market_price_cents, direct_low_price_cents
			FROM sku_price_snapshots
			WHERE snapshot_at = $1
		),
		to_prices AS (
			SELECT sku_id, product_id,
			       low_price_cents, mid_price_cents, high_price_cents,
			       market_price_cents, direct_low_price_cents
			FROM sku_price_snapshots
			WHERE snapshot_at = $2
		),
		combined AS (
			SELECT COALESCE(t.sku_id, f.sku_id) AS sku_id,
			       COALESCE(t.product_id, f.product_id) AS product_id,
			       CASE
			           WHEN f.sku_id IS NULL THEN 'new'
			           WHEN t.sku_id IS NULL THEN 'removed'
			           WHEN NOT (f.market_price_cents IS NOT NULL OR f.low_price_cents IS NOT NULL
			                     OR f.mid_price_cents IS NOT NULL OR f.high_price_cents IS NOT NULL
			                     OR f.direct_low_price_cents IS NOT NULL)
			                AND (t.market_price_cents IS NOT NULL OR t.low_price_cents IS NOT NULL
			                     OR t.mid_price_cents IS NOT NULL OR t.high_price_cents IS NOT NULL
			                     OR t.direct_low_price_cents IS NOT NULL)
			                THEN 'price_added'
			           WHEN (f.market_price_cents IS NOT NULL OR f.low_price_cents IS NOT NULL
			                 OR f.mid_price_cents IS NOT NULL OR f.high_price_cents IS NOT NULL
			                 OR f.direct_low_price_cents IS NOT NULL)
			            AND NOT (t.market_price_cents IS NOT NULL OR t.low_price_cents IS NOT NULL
			                     OR t.mid_price_cents IS NOT NULL OR t.high_price_cents IS NOT NULL
			                     OR t.direct_low_price_cents IS NOT NULL)
			                THEN 'price_removed'
			           ELSE 'changed'
			       END AS change_type,
			       f.low_price_cents AS prev_low, t.low_price_cents AS curr_low,
			       f.mid_price_cents AS prev_mid, t.mid_price_cents AS curr_mid,
			       f.high_price_cents AS prev_high, t.high_price_cents AS curr_high,
			       f.market_price_cents AS prev_market, t.market_price_cents AS curr_market,
			       f.direct_low_price_cents AS prev_direct_low, t.direct_low_price_cents AS curr_direct_low,
			       CASE WHEN f.market_price_cents IS NOT NULL AND t.market_price_cents IS NOT NULL
			            THEN t.market_price_cents - f.market_price_cents
			       END AS market_delta,
			       CASE WHEN f.market_price_cents IS NOT NULL AND f.market_price_cents > 0
			                 AND t.market_price_cents IS NOT NULL
			            THEN round(((t.market_price_cents - f.market_price_cents)::numeric
			                        / f.market_price_cents) * 100, 2)
			       END AS market_delta_pct
			FROM to_prices t
			FULL OUTER JOIN from_prices f ON t.sku_id = f.sku_id
			WHERE f.sku_id IS NULL
			   OR t.sku_id IS NULL
			   OR t.low_price_cents IS DISTINCT FROM f.low_price_cents
			   OR t.mid_price_cents IS DISTINCT FROM f.mid_price_cents
			   OR t.high_price_cents IS DISTINCT FROM f.high_price_cents
			   OR t.market_price_cents IS DISTINCT FROM f.market_price_cents
			   OR t.direct_low_price_cents IS DISTINCT FROM f.direct_low_price_cents
		)
		SELECT c.sku_id, c.product_id, p.name, c.change_type,
		       c.prev_low, c.curr_low, c.prev_mid, c.curr_mid,
		       c.prev_high, c.curr_high, c.prev_market, c.curr_market,
		       c.prev_direct_low, c.curr_direct_low,
		       c.market_delta, c.market_delta_pct
		FROM combined c
		JOIN products p ON c.product_id = p.product_id
		%s
		ORDER BY %s %s NULLS LAST
		%s`, outerWhere, orderExpr, orderDir, limitClause)

	rows, err := h.DB.Query(ctx, query, queryArgs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	movers := make([]models.PriceMover, 0)
	for rows.Next() {
		var m models.PriceMover
		if err := rows.Scan(
			&m.SKUID, &m.ProductID, &m.ProductName, &m.ChangeType,
			&m.PrevLow, &m.CurrLow,
			&m.PrevMid, &m.CurrMid,
			&m.PrevHigh, &m.CurrHigh,
			&m.PrevMarket, &m.CurrMarket,
			&m.PrevDirectLow, &m.CurrDirectLow,
			&m.MarketDeltaCents, &m.MarketDeltaPercent,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		movers = append(movers, m)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query interrupted"})
		return
	}

	c.JSON(http.StatusOK, movers)
}
