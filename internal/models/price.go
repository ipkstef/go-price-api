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
	SKUID              int64    `json:"sku_id"`
	ProductID          int64    `json:"product_id"`
	ProductName        *string  `json:"product_name"`
	ChangeType         string   `json:"change_type"`
	PrevLow            *int32   `json:"prev_low_price_cents"`
	CurrLow            *int32   `json:"curr_low_price_cents"`
	PrevMid            *int32   `json:"prev_mid_price_cents"`
	CurrMid            *int32   `json:"curr_mid_price_cents"`
	PrevHigh           *int32   `json:"prev_high_price_cents"`
	CurrHigh           *int32   `json:"curr_high_price_cents"`
	PrevMarket         *int32   `json:"prev_market_price_cents"`
	CurrMarket         *int32   `json:"curr_market_price_cents"`
	PrevDirectLow      *int32   `json:"prev_direct_low_price_cents"`
	CurrDirectLow      *int32   `json:"curr_direct_low_price_cents"`
	MarketDeltaCents   *int32   `json:"market_delta_cents"`
	MarketDeltaPercent *float64 `json:"market_delta_percent"`
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
