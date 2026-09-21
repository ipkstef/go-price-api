package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	snap, err := latestCompleteSnapshot(ctx, h.DB)
	if err != nil {
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

var validChangeTypes = map[string]bool{
	"changed": true, "new": true, "removed": true,
	"price_added": true, "price_removed": true,
}

// priceColumns maps price_type to the snapshot column the live path reads. The
// precomputed path filters on the stored price_type instead.
var priceColumns = map[string]string{
	"low":    "low_price_cents",
	"market": "market_price_cents",
}

// precomputedMoverCTE reads the deltas the loader stored for this snapshot pair
// and price field. Bounded by the number of movers rather than the size of a
// snapshot. Takes the price_type placeholder number.
const precomputedMoverCTE = `
		SELECT c.sku_id, sk.product_id, sk.language_id, sk.printing_id, sk.condition_id,
		       c.change_type,
		       c.prev_price_cents AS prev_price,
		       c.curr_price_cents AS curr_price,
		       c.delta_cents, c.delta_percent
		FROM sku_price_changes c
		JOIN skus sk ON sk.sku_id = c.sku_id
		WHERE c.prev_snapshot_at = $1 AND c.curr_snapshot_at = $2
		  AND c.price_type = $%d`

// chainedMoverCTE answers an arbitrary window from the adjacent-pair change log
// alone, never touching the snapshots table.
//
// sku_price_changes holds a row only where a price actually moved between two
// consecutive snapshots, so it is a change-event log. Within a window (A, B]:
// a SKU's price at A is the prev_price of its FIRST event after A (nothing
// moved it between A and that event), and its price at B is the curr_price of
// its LAST event at or before B. Every SKU with no event in the window has the
// same price at both ends and is not a mover, so it is correctly absent.
//
// This turns "compare two whole snapshots" into "read the events in between",
// which is a small fraction of the data at realistic churn.
//
// Correctness depends on the chain being unbroken across the window; the caller
// probes for that and falls back to the live path when it is not.
// Takes the price_type placeholder number, then the sku scope.
const chainedMoverCTE = `
		WITH ev AS (
		    SELECT c.sku_id, c.curr_snapshot_at, c.prev_price_cents,
		           c.curr_price_cents, c.change_type
		    FROM sku_price_changes c
		    WHERE c.price_type = $%[1]d
		      AND c.prev_snapshot_at >= $1
		      AND c.curr_snapshot_at <= $2
		      AND %[2]s
		),
		first_ev AS (
		    SELECT DISTINCT ON (sku_id) sku_id,
		           prev_price_cents AS prev_price, change_type AS first_type
		    FROM ev ORDER BY sku_id, curr_snapshot_at ASC
		),
		last_ev AS (
		    SELECT DISTINCT ON (sku_id) sku_id,
		           curr_price_cents AS curr_price, change_type AS last_type
		    FROM ev ORDER BY sku_id, curr_snapshot_at DESC
		)
		SELECT f.sku_id, sk.product_id, sk.language_id, sk.printing_id, sk.condition_id,
		       CASE
		           WHEN f.first_type = 'new' THEN 'new'
		           WHEN l.last_type = 'removed' THEN 'removed'
		           WHEN f.prev_price IS NULL THEN 'price_added'
		           WHEN l.curr_price IS NULL THEN 'price_removed'
		           ELSE 'changed'
		       END AS change_type,
		       f.prev_price, l.curr_price,
		       CASE WHEN f.prev_price IS NOT NULL AND l.curr_price IS NOT NULL
		            THEN l.curr_price - f.prev_price END AS delta_cents,
		       CASE WHEN f.prev_price IS NOT NULL AND l.curr_price IS NOT NULL
		             AND f.prev_price > 0
		            THEN round(((l.curr_price - f.prev_price)::numeric
		                        / f.prev_price) * 100, 2) END AS delta_percent
		FROM first_ev f
		JOIN last_ev l ON l.sku_id = f.sku_id
		JOIN skus sk ON sk.sku_id = f.sku_id
		WHERE f.prev_price IS DISTINCT FROM l.curr_price`

// liveMoverCTE derives the same rows straight from the snapshots, for pairs the
// loader has not stored: non-adjacent 'from'/'to' ranges, and snapshots that
// predate the delta table. It must scan both snapshots in full, so it is slow.
const liveMoverCTE = `
		SELECT x.sku_id, sk.product_id, sk.language_id, sk.printing_id, sk.condition_id,
		       CASE
		           WHEN x.prev_missing THEN 'new'
		           WHEN x.curr_missing THEN 'removed'
		           WHEN x.prev_price IS NULL THEN 'price_added'
		           WHEN x.curr_price IS NULL THEN 'price_removed'
		           ELSE 'changed'
		       END AS change_type,
		       x.prev_price, x.curr_price,
		       CASE WHEN x.prev_price IS NOT NULL AND x.curr_price IS NOT NULL
		            THEN x.curr_price - x.prev_price END AS delta_cents,
		       CASE WHEN x.prev_price IS NOT NULL AND x.curr_price IS NOT NULL
		             AND x.prev_price > 0
		            THEN round(((x.curr_price - x.prev_price)::numeric
		                        / x.prev_price) * 100, 2) END AS delta_percent
		FROM (
		    SELECT COALESCE(t.sku_id, f.sku_id) AS sku_id,
		           (f.sku_id IS NULL) AS prev_missing,
		           (t.sku_id IS NULL) AS curr_missing,
		           f.%[1]s AS prev_price,
		           t.%[1]s AS curr_price
		    FROM (SELECT sku_id, %[1]s
		          FROM sku_price_snapshots
		          WHERE snapshot_at = $1 AND %[2]s) f
		    FULL OUTER JOIN
		         (SELECT sku_id, %[1]s
		          FROM sku_price_snapshots
		          WHERE snapshot_at = $2 AND %[2]s) t
		      ON f.sku_id = t.sku_id
		) x
		JOIN skus sk ON sk.sku_id = x.sku_id
		WHERE x.prev_price IS DISTINCT FROM x.curr_price`

func (h *PriceHandler) Movers(c *gin.Context) {
	limit, offset := parsePagination(c)
	direction := c.DefaultQuery("direction", "up")

	if direction != "up" && direction != "down" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "direction must be 'up' or 'down'"})
		return
	}

	sortBy := c.DefaultQuery("sort_by", "cents")
	if sortBy != "cents" && sortBy != "percent" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sort_by must be 'cents' or 'percent'"})
		return
	}

	// Exactly one price field per request, so a SKU can never appear twice in
	// one response. Accepting a list would allow the same SKU at two different
	// ranks, which existing callers do not expect.
	priceType := c.DefaultQuery("price_type", "low")
	if priceType != "low" && priceType != "market" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "price_type must be 'low' or 'market'"})
		return
	}

	// Comma-separated subset of the change taxonomy, e.g. change_type=changed,new.
	var changeTypes []string
	if v := c.Query("change_type"); v != "" {
		for _, raw := range strings.Split(v, ",") {
			ct := strings.TrimSpace(raw)
			if !validChangeTypes[ct] {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "change_type must be a comma-separated list of: changed, new, removed, price_added, price_removed",
				})
				return
			}
			changeTypes = append(changeTypes, ct)
		}
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

	// The loader precomputes each consecutive snapshot pair into
	// sku_price_changes. Reading that is bounded by the number of movers;
	// deriving the same answer from sku_price_snapshots has no snapshot-scoped
	// index and scans both snapshots in full. Fall back to the live comparison
	// for pairs the loader has not stored — non-adjacent 'from'/'to' ranges, and
	// snapshots that predate the table, and price fields it has not computed.
	// price_type is part of the probe: a pair stored before market prices were
	// tracked has 'low' rows only, and a market request must still fall back.
	precomputed := false
	if err := h.DB.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM sku_price_changes
		 WHERE curr_snapshot_at = $1 AND prev_snapshot_at = $2
		   AND price_type = $3)`,
		toSnap, fromSnap, priceType).Scan(&precomputed); err != nil {
		precomputed = false
	}

	// For a window the loader did not store as a single pair, the adjacent-pair
	// chain can still answer it -- but only if the chain is unbroken. Every
	// complete snapshot after 'from' up to 'to' must contribute events for this
	// price field; a gap would leave a SKU's boundary price unknown and produce
	// a wrong delta rather than a slow one.
	//
	// A snapshot in which nothing moved contributes no events and reads as a
	// gap. That is a false negative: it costs a fallback to the live path, which
	// is correct, so the check stays conservative on purpose.
	chained := false
	if !precomputed {
		var linked, expected int
		if err := h.DB.QueryRow(ctx,
			`SELECT count(DISTINCT curr_snapshot_at) FROM sku_price_changes
			 WHERE price_type = $3 AND prev_snapshot_at >= $1 AND curr_snapshot_at <= $2`,
			fromSnap, toSnap, priceType).Scan(&linked); err == nil {
			if err := h.DB.QueryRow(ctx,
				`SELECT count(*) FROM ingestion_runs
				 WHERE status = 'complete' AND snapshot_at > $1 AND snapshot_at <= $2`,
				fromSnap, toSnap).Scan(&expected); err == nil {
				chained = expected > 0 && linked == expected
			}
		}
	}

	queryArgs := []any{fromSnap, toSnap}
	argN := 3

	// The precomputed and chained paths bind price_type; the live path selects
	// the matching snapshot column instead, so it must not be given the parameter.
	// The CTE itself is built after the filters, because the live one needs the
	// product scope pushed into its snapshot scans.
	priceTypeArg := 0
	if precomputed || chained {
		priceTypeArg = argN
		queryArgs = append(queryArgs, priceType)
		argN++
	}

	// Product-level filters are collected unqualified so they can be rendered
	// twice: once against the products join in the outer WHERE, and once as a
	// scope pushed down into the live path's snapshot scans. Without the
	// pushdown the full outer join materialises both entire snapshots before any
	// product filtering, which turns a narrow group request into a full scan.
	var productClauses []string
	var outerClauses []string
	if eligibleGroups != nil {
		productClauses = append(productClauses, fmt.Sprintf("group_id = ANY($%d::bigint[])", argN))
		queryArgs = append(queryArgs, eligibleGroups)
		argN++
	}

	if v := c.Query("is_sealed"); v != "" {
		productClauses = append(productClauses, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM skus sealed_sk WHERE sealed_sk.product_id = p.product_id "+
				"AND sealed_sk.condition_id = 6) = $%d", argN))
		queryArgs = append(queryArgs, v == "true")
		argN++
	}
	if gid, ok, err := resolveGroupID(c, h.DB); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else if ok {
		productClauses = append(productClauses, fmt.Sprintf("group_id = $%d", argN))
		queryArgs = append(queryArgs, gid)
		argN++
	}
	if v := c.Query("language_id"); v != "" {
		if lid, err := strconv.ParseInt(v, 10, 16); err == nil {
			outerClauses = append(outerClauses, fmt.Sprintf("m.language_id = $%d", argN))
			queryArgs = append(queryArgs, int16(lid))
			argN++
		}
	}
	if v := c.Query("printing_id"); v != "" {
		if pid, err := strconv.ParseInt(v, 10, 16); err == nil {
			outerClauses = append(outerClauses, fmt.Sprintf("m.printing_id = $%d", argN))
			queryArgs = append(queryArgs, int16(pid))
			argN++
		}
	}
	if v := c.Query("condition_id"); v != "" {
		if cid, err := strconv.ParseInt(v, 10, 16); err == nil {
			outerClauses = append(outerClauses, fmt.Sprintf("m.condition_id = $%d", argN))
			queryArgs = append(queryArgs, int16(cid))
			argN++
		}
	}
	if len(changeTypes) > 0 {
		outerClauses = append(outerClauses, fmt.Sprintf("m.change_type = ANY($%d::text[])", argN))
		queryArgs = append(queryArgs, changeTypes)
		argN++
	}
	if minPrice > 0 {
		outerClauses = append(outerClauses, fmt.Sprintf("m.prev_price >= $%d AND m.curr_price >= $%d", argN, argN+1))
		queryArgs = append(queryArgs, int32(minPrice), int32(minPrice))
		argN += 2
	}

	// One definition of "which products are in scope", used twice. Postgres
	// allows a placeholder to be referenced more than once, so the pushdown
	// reuses the same argument numbers as the outer WHERE.
	productScope := "TRUE"
	if len(productClauses) > 0 {
		matching := "SELECT p.product_id FROM products p WHERE " +
			strings.Join(productClauses, " AND ")
		// Outer: the mover's product is in scope.
		outerClauses = append(outerClauses, "m.product_id IN ("+matching+")")
		// Pushed into the live path's snapshot scans, in sku_id terms, so the
		// full outer join never materialises two whole snapshots.
		productScope = "sku_id IN (SELECT scope_sk.sku_id FROM skus scope_sk " +
			"WHERE scope_sk.product_id IN (" + matching + "))"
	}

	outerWhere := ""
	if len(outerClauses) > 0 {
		outerWhere = "AND " + strings.Join(outerClauses, " AND ")
	}

	var moverSource string
	switch {
	case precomputed:
		moverSource = fmt.Sprintf(precomputedMoverCTE, priceTypeArg)
	case chained:
		moverSource = fmt.Sprintf(chainedMoverCTE, priceTypeArg, productScope)
	default:
		moverSource = fmt.Sprintf(liveMoverCTE, priceColumns[priceType], productScope)
	}

	orderDir := "DESC"
	if direction == "down" {
		orderDir = "ASC"
	}
	sortCol := "m.delta_cents"
	if sortBy == "percent" {
		sortCol = "m.delta_percent"
	}

	limitClause := fmt.Sprintf("LIMIT $%d OFFSET $%d", argN, argN+1)
	queryArgs = append(queryArgs, limit, offset)

	// Both sources expose the same columns, so only the CTE differs. The outer
	// query — filters, ordering, pagination — is shared, which is what keeps the
	// precomputed and live paths from drifting apart.

	// NULLS LAST keeps new/removed and price-availability events in the response
	// but after every ranked mover. sku_id breaks ties so paging is stable.
	query := fmt.Sprintf(`
	WITH m AS (%s)
	SELECT m.sku_id, m.product_id, p.name,
	       l.name, pr.name, co.name,
	       m.change_type, m.prev_price, m.curr_price,
	       m.delta_cents, m.delta_percent
	FROM m
	JOIN products p ON m.product_id = p.product_id
	LEFT JOIN languages l ON m.language_id = l.language_id
	LEFT JOIN printings pr ON m.printing_id = pr.printing_id
	LEFT JOIN conditions co ON m.condition_id = co.condition_id
	WHERE TRUE
	  %s
	ORDER BY %s %s NULLS LAST, m.sku_id
	%s`, moverSource, outerWhere, sortCol, orderDir, limitClause)

	rows, err := h.DB.Query(ctx, query, queryArgs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	movers := make([]models.PriceMover, 0)
	for rows.Next() {
		m := models.PriceMover{
			PriceType:          priceType,
			PreviousSnapshotAt: fromSnap.UTC(),
			CurrentSnapshotAt:  toSnap.UTC(),
		}
		if err := rows.Scan(
			&m.SKUID, &m.ProductID, &m.ProductName,
			&m.Language, &m.Printing, &m.Condition,
			&m.ChangeType, &m.PreviousPriceCents, &m.CurrentPriceCents,
			&m.PriceChangeCents, &m.PriceChangePercent,
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
