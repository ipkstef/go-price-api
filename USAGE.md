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

Get prices for multiple products or SKUs in one request. Up to 1,000 ids buffered,
or 50,000 streamed. Reads one snapshot: the latest by default, or any earlier one
with `as_of`.

```sh
POST /prices/latest
Content-Type: application/json

{"product_ids": [86, 87, 88, 1174]}
```

Or by SKU IDs:

```sh
{"sku_ids": [14602, 14603, 14604]}
```

Up to 1,000 ids per request. The response is a JSON array.

#### Streaming large batches

Repricing a spreadsheet means asking about far more than 1,000 SKUs. Set
`"stream": true` to raise the limit to 50,000 and receive newline-delimited JSON
instead — one price object per line, written as rows are read, so neither side
holds the whole result in memory:

```sh
POST /prices/latest
Content-Type: application/json

{"sku_ids": [14602, 14603, ...], "stream": true}
```

```
HTTP/1.1 200 OK
Content-Type: application/x-ndjson
Transfer-Encoding: chunked

{"snapshot_at":"...","sku_id":14602,"low_price_cents":150,...}
{"snapshot_at":"...","sku_id":14603,"low_price_cents":90,...}
```

The rows are identical to the buffered form; only the framing differs. The
practical ceiling is the 1 MB request body, which 50,000 ids fit inside.

**Errors after the first row cannot use an HTTP status**, because 200 has already
been sent. They arrive as a final line carrying an `error` key:

```
{"error":"query interrupted"}
```

Treat any line containing `error` as a failed request. Do not infer success from
a stream simply ending — a truncated connection and a complete response look the
same until you check.

### Price Movement

There is no movers endpoint. You compute movement yourself from two price reads,
which is why `POST /prices/latest` accepts `as_of`.

```sh
# prices now
POST /prices/latest
{"sku_ids": [2249, 2255, 2279]}

# the same SKUs three days earlier
POST /prices/latest
{"sku_ids": [2249, 2255, 2279], "as_of": "2026-09-24"}
```

Diff the two responses on whichever price field you care about. Both reads are
keyed on `(sku_id, snapshot_at)`, so cost scales with the number of ids you ask
about, not with the size of the price history.

`as_of` accepts `YYYY-MM-DD` or RFC3339 and resolves to the **newest complete
snapshot at or before** that instant:

- A bare date is end-of-day, so `2026-09-24` includes a snapshot taken at
  `2026-09-24T20:05:58Z`.
- A future instant resolves to the latest snapshot.
- Nothing at or before the instant returns HTTP `400`
  (`no completed snapshot at or before 2020-01-01`).
- An unparseable value returns HTTP `400`
  (`invalid as_of, use YYYY-MM-DD or RFC3339`).

Every row echoes its `snapshot_at`, so you always know which instant you were
served. `as_of` works with `"stream": true` as well.

Two snapshots are taken per day, so consecutive instants are roughly 12 hours
apart. Movement is unavailable until at least two complete snapshots exist.

#### What this does not do

Ranking movement across the whole catalog — "the top 20 gainers in Magic today" —
is not available. That requires the server to compare every SKU in two snapshots,
which is the expensive operation this design removes. You can only diff SKUs you
name.

If you need a ranked list, hold your own set of ids and rank locally. Filtering by
set, rarity, language, printing, condition or sealed status is available on
`GET /products`, so build your id set there first, then price it.

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
Unopened. A UPC does not make a product sealed. To find sealed SKUs, use
`condition_id=6` or omit the condition filter. Catalog metadata is always current,
so classification reflects the catalog as it stands now, not as it stood at the
time of a historical snapshot.

`GET /prices/movers` was removed on 2026-09-27, along with the precomputed delta
table behind it. Compute movement from two `POST /prices/latest` calls using
`as_of`; see **Price Movement** above. Clients calling the old endpoint receive
HTTP `404`.

Deployment requires the `replicatemtg` schema at `202609210001_initial_schema`
and a full export/load. See that repository's `docs/database-contract.md`.
