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

type PriceMover struct {
	SKUID          int64     `json:"sku_id"`
	ProductID      int64     `json:"product_id"`
	ProductName    *string   `json:"product_name"`
	Language       *string   `json:"language"`
	Printing       *string   `json:"printing"`
	Condition      *string   `json:"condition"`
	PrevSnapshotAt time.Time `json:"prev_snapshot_at"`
	CurrSnapshotAt time.Time `json:"curr_snapshot_at"`
	ChangeType     string    `json:"change_type"`
	// Nullable by contract: a SKU present in only one snapshot, or whose price
	// appeared or disappeared, has no defined delta. A missing price is never
	// coerced to zero, and null deltas sort after defined ones.
	PrevLow      *int32   `json:"prev_low_price_cents"`
	CurrLow      *int32   `json:"curr_low_price_cents"`
	DeltaCents   *int32   `json:"delta_cents"`
	DeltaPercent *float64 `json:"delta_percent"`
}

type BulkPriceRequest struct {
	ProductIDs []int64 `json:"product_ids,omitempty"`
	SKUIDs     []int64 `json:"sku_ids,omitempty"`
}

type SKU struct {
	SKUID       int64  `json:"sku_id"`
	LanguageID  *int16 `json:"language_id,omitempty"`
	PrintingID  *int16 `json:"printing_id,omitempty"`
	ConditionID *int16 `json:"condition_id,omitempty"`
}
