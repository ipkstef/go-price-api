package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	setcatalog "go-price-api/data"
	"go-price-api/internal/models"
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

	if direction != "up" && direction != "down" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "direction must be 'up' or 'down'"})
		return
	}

	var eligibleGroups []int64
	values, queryErr := setReleasedSinceValues(c.Request.URL.RawQuery)
	if values != nil || queryErr != nil {
		var cutoff time.Time
		var err error
		if len(values) == 1 {
			cutoff, err = parseSetReleasedSince(values[0])
		}
		if queryErr != nil || len(values) != 1 || err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid set_released_since: use YYYY or YYYY-MM-DD with a valid date (years 0001-9999); YYYY means January 1"})
			return
		}
		if h.SetCatalog == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "set release catalog unavailable"})
			return
		}
		eligibleGroups = h.SetCatalog.GroupsReleasedSince(cutoff)
		if len(eligibleGroups) == 0 {
			c.JSON(http.StatusOK, make([]models.PriceMover, 0))
			return
		}
	}

	var minPrice int
	if v := c.Query("min_price"); v != "" {
		if mp, err := strconv.Atoi(v); err == nil && mp >= 0 {
			minPrice = mp
		}
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

	queryArgs := []any{fromSnap, toSnap}
	argN := 3

	// Product/SKU filters applied in the outer WHERE
	var outerClauses []string
	if eligibleGroups != nil {
		outerClauses = append(outerClauses, fmt.Sprintf("p.group_id = ANY($%d::bigint[])", argN))
		queryArgs = append(queryArgs, eligibleGroups)
		argN++
	}

	if v := c.Query("is_sealed"); v != "" {
		outerClauses = append(outerClauses, fmt.Sprintf("p.is_sealed = $%d", argN))
		queryArgs = append(queryArgs, v == "true")
		argN++
	}
	if gid, ok, err := resolveGroupID(c, h.DB); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else if ok {
		outerClauses = append(outerClauses, fmt.Sprintf("p.group_id = $%d", argN))
		queryArgs = append(queryArgs, gid)
		argN++
	}
	if v := c.Query("language_id"); v != "" {
		if lid, err := strconv.ParseInt(v, 10, 16); err == nil {
			outerClauses = append(outerClauses, fmt.Sprintf("f.language_id = $%d", argN))
			queryArgs = append(queryArgs, int16(lid))
			argN++
		}
	}
	if v := c.Query("printing_id"); v != "" {
		if pid, err := strconv.ParseInt(v, 10, 16); err == nil {
			outerClauses = append(outerClauses, fmt.Sprintf("f.printing_id = $%d", argN))
			queryArgs = append(queryArgs, int16(pid))
			argN++
		}
	}
	if v := c.Query("condition_id"); v != "" {
		if cid, err := strconv.ParseInt(v, 10, 16); err == nil {
			outerClauses = append(outerClauses, fmt.Sprintf("f.condition_id = $%d", argN))
			queryArgs = append(queryArgs, int16(cid))
			argN++
		}
	}
	if minPrice > 0 {
		outerClauses = append(outerClauses, fmt.Sprintf("f.low_price_cents >= $%d AND t.low_price_cents >= $%d", argN, argN+1))
		queryArgs = append(queryArgs, int32(minPrice), int32(minPrice))
		argN += 2
	}

	outerWhere := ""
	if len(outerClauses) > 0 {
		outerWhere = "AND " + strings.Join(outerClauses, " AND ")
	}

	orderDir := "DESC"
	if direction == "down" {
		orderDir = "ASC"
	}

	limitClause := fmt.Sprintf("LIMIT $%d OFFSET $%d", argN, argN+1)
	queryArgs = append(queryArgs, limit, offset)

	query := fmt.Sprintf(`
		SELECT f.sku_id, f.product_id, p.name,
		       l.name, pr.name, co.name,
		       f.low_price_cents AS prev_low,
		       t.low_price_cents AS curr_low,
		       (t.low_price_cents - f.low_price_cents) AS delta_cents,
		       round(((t.low_price_cents - f.low_price_cents)::numeric
		              / f.low_price_cents) * 100, 2) AS delta_percent
		FROM sku_price_snapshots f
		JOIN sku_price_snapshots t ON f.sku_id = t.sku_id
		JOIN products p ON f.product_id = p.product_id
		LEFT JOIN languages l ON f.language_id = l.language_id
		LEFT JOIN printings pr ON f.printing_id = pr.printing_id
		LEFT JOIN conditions co ON f.condition_id = co.condition_id
		WHERE f.snapshot_at = $1
		  AND t.snapshot_at = $2
		  AND f.low_price_cents IS NOT NULL
		  AND t.low_price_cents IS NOT NULL
		  AND f.low_price_cents > 0
		  AND t.low_price_cents != f.low_price_cents
		  %s
		ORDER BY (t.low_price_cents - f.low_price_cents) %s
		%s`, outerWhere, orderDir, limitClause)

	rows, err := h.DB.Query(ctx, query, queryArgs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	movers := make([]models.PriceMover, 0)
	for rows.Next() {
		m := models.PriceMover{
			PrevSnapshotAt: fromSnap.UTC(),
			CurrSnapshotAt: toSnap.UTC(),
		}
		if err := rows.Scan(
			&m.SKUID, &m.ProductID, &m.ProductName,
			&m.Language, &m.Printing, &m.Condition,
			&m.PrevLow, &m.CurrLow,
			&m.DeltaCents, &m.DeltaPercent,
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

func parseSetReleasedSince(value string) (time.Time, error) {
	if len(value) == 4 {
		value += "-01-01"
	}
	if len(value) != len(time.DateOnly) {
		return time.Time{}, fmt.Errorf("invalid date length")
	}
	d, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, err
	}
	if d.Year() < 1 {
		return time.Time{}, fmt.Errorf("year must be between 0001 and 9999")
	}
	return d, nil
}

// Decode this parameter explicitly: URL.Query silently drops malformed escapes,
// which could otherwise turn an invalid cutoff into an unfiltered request.
func setReleasedSinceValues(rawQuery string) ([]string, error) {
	var values []string
	for _, pair := range strings.Split(rawQuery, "&") {
		key, value, _ := strings.Cut(pair, "=")
		name, err := url.QueryUnescape(key)
		if err != nil || name != "set_released_since" {
			continue
		}
		decoded, err := url.QueryUnescape(value)
		if err != nil {
			return nil, err
		}
		values = append(values, decoded)
	}
	return values, nil
}
