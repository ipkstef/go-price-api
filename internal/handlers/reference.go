package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-price-api/internal/models"
)

type ReferenceHandler struct {
	DB *pgxpool.Pool
}

func (h *ReferenceHandler) Conditions(c *gin.Context) {
	rows, err := h.DB.Query(c.Request.Context(),
		`SELECT condition_id, name FROM conditions ORDER BY condition_id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	items := make([]models.Condition, 0)
	for rows.Next() {
		var v models.Condition
		if err := rows.Scan(&v.ConditionID, &v.Name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query interrupted"})
		return
	}

	c.JSON(http.StatusOK, items)
}

func (h *ReferenceHandler) Languages(c *gin.Context) {
	rows, err := h.DB.Query(c.Request.Context(),
		`SELECT language_id, name FROM languages ORDER BY language_id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	items := make([]models.Language, 0)
	for rows.Next() {
		var v models.Language
		if err := rows.Scan(&v.LanguageID, &v.Name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query interrupted"})
		return
	}

	c.JSON(http.StatusOK, items)
}

func (h *ReferenceHandler) Printings(c *gin.Context) {
	rows, err := h.DB.Query(c.Request.Context(),
		`SELECT printing_id, name FROM printings ORDER BY printing_id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	items := make([]models.Printing, 0)
	for rows.Next() {
		var v models.Printing
		if err := rows.Scan(&v.PrintingID, &v.Name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query interrupted"})
		return
	}

	c.JSON(http.StatusOK, items)
}

func (h *ReferenceHandler) Rarities(c *gin.Context) {
	rows, err := h.DB.Query(c.Request.Context(),
		`SELECT rarity_id, name FROM rarities ORDER BY rarity_id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	items := make([]models.Rarity, 0)
	for rows.Next() {
		var v models.Rarity
		if err := rows.Scan(&v.RarityID, &v.Name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query interrupted"})
		return
	}

	c.JSON(http.StatusOK, items)
}
