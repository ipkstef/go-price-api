# DB Maintainer Handoff — go-price-api

## Questions

1. Should the API filter price queries to completed ingestion runs only (like the `complete_sku_price_snapshots` view does), or is querying `sku_price_snapshots` directly fine?

2. Will other TCG categories (Pokémon, Yu-Gi-Oh) be ingested, or is this Magic-only?

3. Should the `users` table and new indexes be tracked as dbmate migrations? If so, is there a naming convention?

4. Is there a fixed daily ingestion window? (e.g., "data lands around 2am")

5. What's the chunk interval on `sku_price_snapshots`?

## Commands to run

### 1. Install pg_trgm (superuser required)

Product name search (`ILIKE '%lightning bolt%'`) currently does a full sequential scan on 115K rows — 2.4 seconds per query. pg_trgm enables a GIN index that makes substring search use an index scan.

```sql
CREATE EXTENSION pg_trgm;
```

### 2. Product name search index

Once pg_trgm is installed, this index covers the `ILIKE '%substring%'` pattern the API uses for card name search.

```sql
CREATE INDEX CONCURRENTLY products_clean_name_trgm_idx
ON app.products USING gin (clean_name gin_trgm_ops);
```

### 3. Price-by-product index for hot chunks

The PK is `(sku_id, snapshot_at)` which can't serve `WHERE product_id = ?`. Every price-by-product query currently scans the full 5.16M-row chunk (8.8 seconds). This index covers the hot window before the compression policy converts chunks to columnstore — once compressed, the segmentby on `product_id` handles filtering.

```sql
CREATE INDEX CONCURRENTLY sku_price_snapshots_product_snapshot_idx
ON app.sku_price_snapshots (product_id, snapshot_at DESC);
```

### 4. Continuous aggregate for price movers

The movers endpoint compares every SKU across two snapshots — a full-table operation no index can help. This aggregate pre-computes the last `market_price_cents` per SKU per day so the comparison query hits a small summary instead of 5M+ rows.

```sql
CREATE MATERIALIZED VIEW app.daily_sku_market_prices
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 day', snapshot_at) AS day,
       sku_id,
       product_id,
       last(market_price_cents, snapshot_at) AS market_price_cents
FROM app.sku_price_snapshots
GROUP BY day, sku_id, product_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy('app.daily_sku_market_prices',
  start_offset => INTERVAL '7 days',
  end_offset   => INTERVAL '1 hour',
  schedule_interval => INTERVAL '1 hour');
```
