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
	SKUID       int64   `json:"sku_id"`
	ProductID   int64   `json:"product_id"`
	ProductName *string `json:"product_name"`
	Language    *string `json:"language"`
	Printing    *string `json:"printing"`
	Condition   *string `json:"condition"`

	// PriceType names the price field the prices and changes below describe.
	// It is echoed on every row so a response is self-describing: the same SKU
	// can move on one price field and not another, and reading a market price
	// out of a field named "low" was a trap worth closing.
	PriceType  string `json:"price_type"`
	ChangeType string `json:"change_type"`

	PreviousSnapshotAt time.Time `json:"previous_snapshot_at"`
	CurrentSnapshotAt  time.Time `json:"current_snapshot_at"`

	// Nullable by contract: a SKU present in only one snapshot, or whose price
	// appeared or disappeared, has no defined change. A missing price is never
	// coerced to zero, and null changes sort after defined ones. Percent is
	// additionally null when the earlier price is not greater than zero.
	PreviousPriceCents *int32   `json:"previous_price_cents"`
	CurrentPriceCents  *int32   `json:"current_price_cents"`
	PriceChangeCents   *int32   `json:"price_change_cents"`
	PriceChangePercent *float64 `json:"price_change_percent"`
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
}

type SKU struct {
	SKUID       int64  `json:"sku_id"`
	LanguageID  *int16 `json:"language_id,omitempty"`
	PrintingID  *int16 `json:"printing_id,omitempty"`
	ConditionID *int16 `json:"condition_id,omitempty"`
}
