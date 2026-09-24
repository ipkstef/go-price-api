package models

import "time"

type SKUPrice struct {
	SnapshotAt          time.Time `json:"snapshot_at"`
	SKUID               int64     `json:"sku_id"`
	ProductID           int64     `json:"product_id"`
	LanguageID          *int16    `json:"language_id,omitempty"`
	PrintingID          *int16    `json:"printing_id,omitempty"`
	ConditionID         *int16    `json:"condition_id,omitempty"`
	LowPriceCents       *int32    `json:"low_price_cents,omitempty"`
	MidPriceCents       *int32    `json:"mid_price_cents,omitempty"`
	HighPriceCents      *int32    `json:"high_price_cents,omitempty"`
	MarketPriceCents    *int32    `json:"market_price_cents,omitempty"`
	DirectLowPriceCents *int32    `json:"direct_low_price_cents,omitempty"`
}

type PricePoint struct {
	Bucket              time.Time `json:"bucket"`
	AvgLowPriceCents    *float64  `json:"avg_low_price_cents,omitempty"`
	AvgMidPriceCents    *float64  `json:"avg_mid_price_cents,omitempty"`
	AvgHighPriceCents   *float64  `json:"avg_high_price_cents,omitempty"`
	AvgMarketPriceCents *float64  `json:"avg_market_price_cents,omitempty"`
}

type BulkPriceRequest struct {
	ProductIDs []int64 `json:"product_ids,omitempty"`
	SKUIDs     []int64 `json:"sku_ids,omitempty"`
	// Stream switches the response to newline-delimited JSON, one SKUPrice per
	// line, and raises the id limit. Repricing a whole CSV is the reason this
	// endpoint exists, and buffering tens of thousands of rows to serialise them
	// in one array costs memory on a small host for no benefit to the caller,
	// who is reading them one at a time anyway.
	Stream bool `json:"stream,omitempty"`
	// AsOf selects the newest complete snapshot at or before this instant,
	// accepting YYYY-MM-DD or RFC3339. Empty means the latest snapshot.
	AsOf string `json:"as_of,omitempty"`
}

type SKU struct {
	SKUID       int64  `json:"sku_id"`
	LanguageID  *int16 `json:"language_id,omitempty"`
	PrintingID  *int16 `json:"printing_id,omitempty"`
	ConditionID *int16 `json:"condition_id,omitempty"`
}
