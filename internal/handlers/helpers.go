package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func latestCompleteSnapshot(ctx context.Context, db *pgxpool.Pool) (time.Time, error) {
	var t time.Time
	err := db.QueryRow(ctx,
		`SELECT max(snapshot_at) FROM ingestion_runs WHERE status = 'complete'`).Scan(&t)
	return t, err
}

// resolveSnapshot picks the snapshot instant a price read is answered from.
// Empty asOf means the newest complete run. Otherwise it is the newest complete
// run at or before the given instant, so a caller asking for a date gets the
// last observation on or before it rather than nothing.
func resolveSnapshot(ctx context.Context, db *pgxpool.Pool, asOf string) (time.Time, error) {
	if asOf == "" {
		return latestCompleteSnapshot(ctx, db)
	}

	var cutoff time.Time
	if t, err := time.Parse(time.RFC3339, asOf); err == nil {
		cutoff = t
	} else if d, err := time.Parse("2006-01-02", asOf); err == nil {
		// A bare date means the end of that day, so the last snapshot taken on
		// it is included rather than only ones before midnight.
		cutoff = d.AddDate(0, 0, 1)
	} else {
		return time.Time{}, fmt.Errorf("invalid as_of, use YYYY-MM-DD or RFC3339")
	}

	var t time.Time
	err := db.QueryRow(ctx,
		`SELECT snapshot_at FROM ingestion_runs
		 WHERE status = 'complete' AND snapshot_at <= $1
		 ORDER BY snapshot_at DESC LIMIT 1`, cutoff).Scan(&t)
	if err != nil {
		return time.Time{}, fmt.Errorf("no completed snapshot at or before %s", asOf)
	}
	return t, nil
}

func resolveGroupID(c *gin.Context, db *pgxpool.Pool) (int64, bool, error) {
	if v := c.Query("group_id"); v != "" {
		gid, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("invalid group_id")
		}
		return gid, true, nil
	}
	if v := c.Query("group"); v != "" {
		var gid int64
		err := db.QueryRow(context.Background(),
			`SELECT group_id FROM groups WHERE abbr = $1`, strings.ToUpper(v)).Scan(&gid)
		if err != nil {
			return 0, false, fmt.Errorf("set code '%s' not found", v)
		}
		return gid, true, nil
	}
	return 0, false, nil
}

func parsePagination(c *gin.Context) (limit, offset int) {
	limit = 50
	offset = 0

	if v := c.Query("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}
	if v := c.Query("offset"); v != "" {
		if o, err := strconv.Atoi(v); err == nil && o >= 0 {
			offset = o
		}
	}

	return
}

type whereBuilder struct {
	clauses []string
	args    []any
	nextArg int
}

func newWhereBuilder() *whereBuilder {
	return &whereBuilder{nextArg: 1}
}

func (w *whereBuilder) Add(column string, op string, value any) {
	w.clauses = append(w.clauses, fmt.Sprintf("%s %s $%d", column, op, w.nextArg))
	w.args = append(w.args, value)
	w.nextArg++
}

func (w *whereBuilder) SQL() string {
	if len(w.clauses) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(w.clauses, " AND ")
}

func (w *whereBuilder) Args(extra ...any) []any {
	out := make([]any, len(w.args), len(w.args)+len(extra))
	copy(out, w.args)
	return append(out, extra...)
}

func (w *whereBuilder) NextArg() int {
	return w.nextArg
}
