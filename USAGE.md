# go-price-api

REST API for TCGPlayer Magic: The Gathering price data, backed by TimescaleDB.

## Setup

```sh
cp .env.example .env
# Edit .env with your database URL and a JWT secret
```

Required environment variables:

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | yes | | PostgreSQL connection string |
| `JWT_SECRET` | yes | | Secret for signing JWTs |
| `PORT` | no | `8080` | Server listen port |
| `JWT_EXPIRY` | no | `24h` | Token expiry duration |

## Run

```sh
go run ./cmd/server
```

## Authentication

Register or login to get a JWT. Pass it as a Bearer token on all other requests.

```sh
# Register
curl -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"myuser","password":"mypassword"}'

# Login
curl -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"myuser","password":"mypassword"}'

# Response
{"token":"eyJ...","expires_at":1787444214}
```

Use the token on subsequent requests:

```sh
curl http://localhost:8080/products \
  -H 'Authorization: Bearer eyJ...'
```

Refresh before expiry:

```sh
curl -X POST http://localhost:8080/auth/refresh \
  -H 'Authorization: Bearer eyJ...'
```

## Endpoints

Hit `GET /` for a self-describing JSON route map with all parameters.

### Products

**List products** — search and filter cards.

```sh
GET /products?name=lightning+bolt&limit=10
GET /products?group_id=7&rarity_id=3
GET /products?subtype=creature&is_sealed=false
```

Params: `name`, `group_id`, `rarity_id`, `subtype`, `is_sealed`, `limit` (1-200, default 50), `offset`.

Returns paginated results with `data` and `pagination` fields.

**Get product detail** — full card data including oracle text and extended data (P/T, flavor).

```sh
GET /products/1174
```

**List SKU variants** — all language × printing × condition combinations for a product.

```sh
GET /products/1174/skus
```

**Latest prices** — most recent completed-snapshot prices for all SKUs of a product.

```sh
GET /products/1174/prices
```

**Price history** — time-bucketed averages over a date range.

```sh
GET /products/1174/prices/history?interval=1+day
GET /products/1174/prices/history?sku_id=14602&from=2026-08-01&to=2026-08-21
```

Params: `interval` (1 hour, 6 hours, 12 hours, 1 day, 7 days, 30 days), `sku_id`, `from`, `to` (YYYY-MM-DD).

### Groups (Sets)

```sh
GET /groups?name=alpha&is_current=true&limit=10
GET /groups/7
GET /groups/7/products?limit=20
```

### Bulk Price Lookup

Get the latest prices for multiple products or SKUs in one request. Max 500 IDs.

```sh
POST /prices/latest
Content-Type: application/json

{"product_ids": [86, 87, 88, 1174]}
```

Or by SKU IDs:

```sh
{"sku_ids": [14602, 14603, 14604]}
```

### Price Movers

Compare prices between two completed snapshots. Defaults to the two most recent.

```sh
GET /prices/movers?direction=up&limit=20
GET /prices/movers?from=2026-08-20&to=2026-08-21&sort_by=percent
GET /prices/movers?change_type=new,changed&direction=down
```

Params:
- `from` / `to` — YYYY-MM-DD, resolves to the latest completed snapshot before midnight UTC of the following day
- `direction` — `up` (default) or `down`
- `sort_by` — `cents` (default) or `percent`
- `change_type` — comma-separated filter: `changed`, `new`, `removed`, `price_added`, `price_removed`
- `limit` — 1-200 (default 50)

Response includes `change_type`, all five price fields (prev/curr), `market_delta_cents`, and `market_delta_percent`. Deltas are null when either side lacks a market price.

### Reference Data

```sh
GET /conditions    # Near Mint, Lightly Played, etc.
GET /languages     # English, Japanese, etc.
GET /printings     # Normal, Foil
GET /rarities      # Common, Uncommon, Rare, etc.
```

### Ingestion Runs

```sh
GET /ingestion/runs?status=complete
GET /ingestion/runs/1
```

## Pagination

List endpoints return:

```json
{
  "data": [...],
  "pagination": {
    "limit": 50,
    "offset": 0,
    "total": 1234
  }
}
```

Use `limit` (max 200) and `offset` query params.

## Errors

All errors return JSON:

```json
{"error": "description of what went wrong"}
```

## Data Notes

- Prices are in cents (USD). A `market_price_cents` of `4900` is $49.00.
- Only data from completed ingestion runs is returned. Partial loads are excluded.
- This API serves Magic: The Gathering data only (`category_id = 1`).
