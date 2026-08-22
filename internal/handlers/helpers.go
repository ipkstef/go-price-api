package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

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
	return append(w.args, extra...)
}

func (w *whereBuilder) NextArg() int {
	return w.nextArg
}
