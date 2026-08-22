package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-price-api/internal/models"
)

type GroupHandler struct {
	DB *pgxpool.Pool
}

func (h *GroupHandler) List(c *gin.Context) {
	limit, offset := parsePagination(c)
	wb := newWhereBuilder()

	if v := c.Query("is_current"); v != "" {
		wb.Add("is_current", "=", v == "true")
	}
	if v := c.Query("name"); v != "" {
		wb.Add("name", "ILIKE", "%"+v+"%")
	}

	where := wb.SQL()

	var total int
	if err := h.DB.QueryRow(c.Request.Context(), "SELECT count(*) FROM groups "+where, wb.args...).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT group_id, name, abbr, is_current
		 FROM groups %s
		 ORDER BY name
		 LIMIT $%d OFFSET $%d`,
		where, wb.NextArg(), wb.NextArg()+1,
	)

	rows, err := h.DB.Query(c.Request.Context(), query, wb.Args(limit, offset)...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	groups := make([]models.Group, 0)
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.GroupID, &g.Name, &g.Abbr, &g.IsCurrent); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		groups = append(groups, g)
	}

	c.JSON(http.StatusOK, models.PaginatedResponse{
		Data:       groups,
		Pagination: models.Pagination{Limit: limit, Offset: offset, Total: total},
	})
}

func (h *GroupHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group id"})
		return
	}

	var g models.Group
	err = h.DB.QueryRow(c.Request.Context(),
		`SELECT group_id, name, abbr, is_current FROM groups WHERE group_id = $1`, id,
	).Scan(&g.GroupID, &g.Name, &g.Abbr, &g.IsCurrent)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	c.JSON(http.StatusOK, g)
}

func (h *GroupHandler) Products(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group id"})
		return
	}

	limit, offset := parsePagination(c)

	var total int
	if err := h.DB.QueryRow(c.Request.Context(),
		"SELECT count(*) FROM products WHERE group_id = $1", id).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	rows, err := h.DB.Query(c.Request.Context(),
		`SELECT product_id, group_id, name, clean_name, image_url, url,
		        is_sealed, rarity_id, collector_number, subtype
		 FROM products
		 WHERE group_id = $1
		 ORDER BY product_id
		 LIMIT $2 OFFSET $3`, id, limit, offset,
	)
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

	c.JSON(http.StatusOK, models.PaginatedResponse{
		Data:       products,
		Pagination: models.Pagination{Limit: limit, Offset: offset, Total: total},
	})
}
