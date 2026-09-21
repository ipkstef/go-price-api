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
GET /groups?name=alpha&limit=10
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

SKUs whose price moved between two completed snapshots, ranked by the size of the change. One price field per request, chosen with `price_type`. Each result includes the language, printing, and condition so you can identify the variant.

```sh
GET /prices/movers?direction=up&min_price=100&limit=20
GET /prices/movers?direction=up&is_sealed=false&language_id=1&printing_id=1&condition_id=1&min_price=500
GET /prices/movers?direction=down&group_id=7&limit=10
GET /prices/movers?group=LTR&direction=up
GET /prices/movers?set_released_since=2020&limit=20
GET /prices/movers?set_released_since=2020-06-01&direction=up
GET /prices/movers?price_type=market&sort_by=percent
GET /prices/movers?change_type=new,price_added&limit=100
```

Params:
- `from` / `to` — YYYY-MM-DD, resolves to the latest completed snapshot at or before this date
- `direction` — `up` (default) or `down`
- `price_type` — `low` (default) or `market`. Exactly one value; a list is rejected so a SKU can never occupy two ranks in one response.
- `sort_by` — `cents` (default) or `percent`
- `change_type` — comma-separated subset of `changed`, `new`, `removed`, `price_added`, `price_removed`. Omit for all.
- `min_price` — minimum price on both sides, in cents (filters out noise)
- `is_sealed` — `true`/`false`
- `group_id` — filter to a specific set by TCGplayer group ID
- `group` — filter to a specific set by code, e.g. `LTR` (case-insensitive)
- `set_released_since` — inclusive set release cutoff, `YYYY` or `YYYY-MM-DD`; a year means January 1. Separate from the price snapshot `from` / `to` dates.
- `language_id` — filter to a specific language
- `printing_id` — filter to a specific printing (1=Normal, 2=Foil)
- `condition_id` — filter to a specific condition (1=Near Mint, 2=Lightly Played, etc.)
- `limit` — 1-200 (default 50)
- `offset` — pagination offset (default 0)

`max_price` is not implemented and is ignored if supplied.

Response includes `price_type`, `change_type`, `previous_price_cents`, `current_price_cents`, `price_change_cents`, `price_change_percent`, plus `language`, `printing`, and `condition` names. `price_type` is echoed on every row, so a response says which price field its numbers describe.

Prices and changes are nullable. A SKU present in only one snapshot, or whose
price appeared or disappeared, has no defined change, and a missing price is
never coerced to zero. `price_change_percent` is additionally null when the
earlier price is not greater than zero. Null changes sort after every ranked
mover, so
`new`, `removed`, `price_added` and `price_removed` stay in the response without
polluting the ranking.

`change_type` values, each a statement about the requested price field rather
than the SKU as a whole:

- `changed` — present in both snapshots with two distinct non-null prices
- `new` — present only in the later snapshot, with a price for this field
- `removed` — present only in the earlier snapshot, with a price for this field
- `price_added` — present in both; this field went from null to a price
- `price_removed` — present in both; this field went from a price to null

Each result also includes `previous_snapshot_at` and `current_snapshot_at`: UTC
RFC 3339 timestamps identifying the actual snapshots used for the previous and
current prices. These are snapshot times, not individual listing-change times.
They reflect the resolved snapshots, which can precede the requested `from` or
`to` date. All results in one response share the same comparison timestamps.
The response remains an array; an empty result is still `[]`.

Example timestamp fields (illustrative):

```json
{
  "previous_snapshot_at": "2026-09-08T15:00:13Z",
  "current_snapshot_at": "2026-09-09T02:08:56Z"
}
```

When `set_released_since` is provided, groups missing from the bundled Scryfall
catalog are excluded. If multiple Scryfall sets share a TCGplayer group, its
earliest release date determines eligibility. The cutoff combines with the
other filters before ranking and pagination. No eligible results returns `[]`.
Omitting the parameter preserves the existing behavior. Future releases can
qualify if their products have prices in both comparison snapshots.

Empty, repeated, malformed, or impossible cutoff values return HTTP `400`:

```json
{"error":"invalid set_released_since: use YYYY or YYYY-MM-DD with a valid date (years 0001-9999); YYYY means January 1"}
```

For example, `2020` and `2020-01-01` are equivalent; `2020-01`, `2023-02-29`, and
`0000` are invalid. Refresh the catalog with `go run ./cmd/update-sets`, then
rebuild and restart the server to use the new embedded metadata.

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

## Deployment

### systemd service

Copy the service file and enable it:

```sh
sudo cp go-price-api.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable go-price-api
sudo systemctl start go-price-api
```

The service reads environment variables from `.env` in the project directory. It restarts automatically on failure.

Useful commands:

```sh
sudo systemctl status go-price-api    # check status
sudo systemctl restart go-price-api   # restart after a rebuild
sudo journalctl -u go-price-api -f    # follow logs
```

### Deploy workflow

```sh
cd go-price-api
git pull origin main
/usr/local/go/bin/go build -o go-price-api ./cmd/server
sudo systemctl restart go-price-api
```

## Data Notes

- Prices are in cents (USD). A `market_price_cents` of `4900` is $49.00.
- Only data from completed ingestion runs is returned. Partial loads are excluded.
- This API serves Magic: The Gathering data only (`category_id = 1`).

## Catalog contract

Group responses contain only `group_id`, `name`, and `abbr`. The former
`is_current` filter returns HTTP 400 when supplied (including an empty value).
It did not represent print status and has been removed.

Rarity IDs match TCGplayer: 1 Mythic, 2 Rare, 3 Uncommon, 4 Common, 5 Promo,
107 Land, 108 Token, 111 Special. Update clients that previously sent local IDs.

`is_sealed` remains a query parameter and a response field, but it is no longer a
stored column: the API derives it from SKU conditions, where condition 6 is
Unopened. A UPC does not make a product sealed. To query sealed SKU prices or
movers directly, use `condition_id=6` or omit the condition filter. Historical
movers use current catalog metadata, so classification reflects the catalog as it
stands now, not as it stood at the time of the snapshot.

Movers over an arbitrary `from`/`to` range are answered by composing the
adjacent-snapshot change log rather than comparing two whole snapshots. When that
chain has a gap the API falls back to the direct comparison, which is correct but
markedly slower.

Deployment requires the `replicatemtg` schema at `202609210001_initial_schema`
and a full export/load. See that repository's `docs/database-contract.md`.
