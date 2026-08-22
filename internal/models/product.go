package models

import (
	"encoding/json"
	"time"
)

type Product struct {
	ProductID         int64            `json:"product_id"`
	GroupID           int64            `json:"group_id"`
	Name              *string          `json:"name"`
	CleanName         *string          `json:"clean_name"`
	ImageURL          *string          `json:"image_url"`
	URL               *string          `json:"url"`
	IsSealed          bool             `json:"is_sealed"`
	UPC               *string          `json:"upc,omitempty"`
	RarityID          *int16           `json:"rarity_id,omitempty"`
	CollectorNumber   *string          `json:"collector_number,omitempty"`
	Subtype           *string          `json:"subtype,omitempty"`
	OracleTextRaw     *string          `json:"oracle_text_raw,omitempty"`
	OracleTextPlain   *string          `json:"oracle_text_plain,omitempty"`
	PresaleIsPresale  *bool            `json:"presale_is_presale,omitempty"`
	PresaleReleasedOn *time.Time       `json:"presale_released_on,omitempty"`
	ExtendedData      json.RawMessage  `json:"extended_data,omitempty"`
	ModifiedOn        *time.Time       `json:"modified_on,omitempty"`
}

type ProductSummary struct {
	ProductID       int64   `json:"product_id"`
	GroupID         int64   `json:"group_id"`
	Name            *string `json:"name"`
	CleanName       *string `json:"clean_name"`
	ImageURL        *string `json:"image_url"`
	URL             *string `json:"url"`
	IsSealed        bool    `json:"is_sealed"`
	RarityID        *int16  `json:"rarity_id,omitempty"`
	CollectorNumber *string `json:"collector_number,omitempty"`
	Subtype         *string `json:"subtype,omitempty"`
}
