package models

import "time"

type IngestionRun struct {
	IngestionID           int64      `json:"ingestion_id"`
	CategoryID            int32      `json:"category_id"`
	ObjectKey             string     `json:"object_key"`
	ObjectEtag            string     `json:"object_etag"`
	SnapshotAt            time.Time  `json:"snapshot_at"`
	ObjectSizeBytes       int64      `json:"object_size_bytes"`
	ExpectedRows          int64      `json:"expected_rows"`
	LoadedRows            int64      `json:"loaded_rows"`
	LastCompletedRowGroup int32      `json:"last_completed_row_group"`
	Status                string     `json:"status"`
	ErrorMessage          *string    `json:"error_message,omitempty"`
	StartedAt             time.Time  `json:"started_at"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
}
