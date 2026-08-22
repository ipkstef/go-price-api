package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-price-api/internal/models"
)

type IngestionHandler struct {
	DB *pgxpool.Pool
}

func (h *IngestionHandler) List(c *gin.Context) {
	limit, offset := parsePagination(c)
	wb := newWhereBuilder()

	if v := c.Query("status"); v != "" {
		wb.Add("status", "=", v)
	}

	where := wb.SQL()

	var total int
	if err := h.DB.QueryRow(c.Request.Context(),
		"SELECT count(*) FROM ingestion_runs "+where, wb.args...).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT ingestion_id, category_id, object_key, object_etag, snapshot_at,
		        object_size_bytes, expected_rows, loaded_rows,
		        last_completed_row_group, status, error_message,
		        started_at, completed_at
		 FROM ingestion_runs %s
		 ORDER BY started_at DESC
		 LIMIT $%d OFFSET $%d`,
		where, wb.NextArg(), wb.NextArg()+1,
	)

	rows, err := h.DB.Query(c.Request.Context(), query, wb.Args(limit, offset)...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()

	runs := make([]models.IngestionRun, 0)
	for rows.Next() {
		var r models.IngestionRun
		if err := rows.Scan(
			&r.IngestionID, &r.CategoryID, &r.ObjectKey, &r.ObjectEtag, &r.SnapshotAt,
			&r.ObjectSizeBytes, &r.ExpectedRows, &r.LoadedRows,
			&r.LastCompletedRowGroup, &r.Status, &r.ErrorMessage,
			&r.StartedAt, &r.CompletedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
			return
		}
		runs = append(runs, r)
	}

	c.JSON(http.StatusOK, models.PaginatedResponse{
		Data:       runs,
		Pagination: models.Pagination{Limit: limit, Offset: offset, Total: total},
	})
}

func (h *IngestionHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ingestion id"})
		return
	}

	var r models.IngestionRun
	err = h.DB.QueryRow(c.Request.Context(),
		`SELECT ingestion_id, category_id, object_key, object_etag, snapshot_at,
		        object_size_bytes, expected_rows, loaded_rows,
		        last_completed_row_group, status, error_message,
		        started_at, completed_at
		 FROM ingestion_runs WHERE ingestion_id = $1`, id,
	).Scan(
		&r.IngestionID, &r.CategoryID, &r.ObjectKey, &r.ObjectEtag, &r.SnapshotAt,
		&r.ObjectSizeBytes, &r.ExpectedRows, &r.LoadedRows,
		&r.LastCompletedRowGroup, &r.Status, &r.ErrorMessage,
		&r.StartedAt, &r.CompletedAt,
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingestion run not found"})
		return
	}

	c.JSON(http.StatusOK, r)
}
