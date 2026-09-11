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
