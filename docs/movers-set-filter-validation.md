# Movers set release filter validation

## Access patterns and assumptions

- Metadata write: occasional manual catalog refresh; 374 set records, 349
  group IDs, one atomic file replacement. No database writes or migrations.
  The request timeout is 60 seconds; refresh is outside the API request path.
- Metadata read: once at server startup, then an immutable in-memory map of
  each group's earliest release date. Each filtered request scans 349 dates.
- Core read: compare two completed snapshots, join products, apply set and
  SKU filters, rank by absolute price delta, return 1–200 rows (default 50).
  Frequency and production concurrency are unknown; benchmark serial requests
  initially. Proposed interactive target: under 1 second of database execution
  time, assessed against measurements below. This is a target, not an established SLO.
- Lifecycle: recent snapshots are uncompressed; older snapshots are compressed.
  Measure recent/recent, compressed/compressed, and mixed comparisons separately.
  No assumption that filtering products avoids scanning compressed price data.
- Existing indexes include `products(group_id)` and snapshot indexes on
  `(sku_id, snapshot_at)` and `(product_id, snapshot_at DESC)`. This feature
  adds no indexes, cached query results, partitions, or derived database tables.

The cutoff is inclusive. Unknown groups are excluded when filtering, and a
shared group qualifies only if its earliest release qualifies. Catalog dates
are bundled current metadata even when comparing historical snapshots.
The existing definition of completed snapshots and price comparisons is
unchanged. The set filter itself does not change endpoint output fields;
the subsequent timestamp addition is documented below.

## Validation status

Unit and race-instrumented tests passed for year shorthand, full dates,
invalid/empty cutoff errors, inclusive boundaries, earliest shared-group dates,
and empty eligible results. Regression tests also cover malformed URL escapes,
repeated parameters, and rejection of year zero during catalog refresh.
The read-only live handler integration test passed on an isolated rerun.

## Database measurements (2026-09-08)

Read-only `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` against the existing database,
using the handler's SELECT, `direction=up`, `limit=20`, and no other filters.
Compare omitted cutoff with `set_released_since=2020`. These are single-run
observations; part of this batch overlapped the integration test, so they are
not isolated latency measurements or evidence of a guaranteed speedup.

Database: 116,167 products, 455 groups, 16 completed snapshot timestamps.
The catalog matches 348 database groups; 107 are unmapped and excluded by an
active cutoff. The catalog contains one additional group ID absent from the
current database. Snapshot storage including indexes: 2,862,170,112 bytes;
products including indexes: 226,361,344 bytes. No additional database storage.

Snapshot pairs (UTC):

- Recent: September 7 14:26:05 → September 8 15:00:13; both uncompressed.
- Compressed: August 21 17:19:51 → August 22 05:08:19; both compressed.
- Mixed: August 21 17:19:51 → September 8 15:00:13.

| Lifecycle | Cutoff | Execution ms | Rows returned | Shared buffer hits | Shared buffer reads |
| --- | --- | ---: | ---: | ---: | ---: |
| recent | omitted | 47399.265 | 20 | 9,404 | 120,162 |
| recent | 2020-01-01 | 41248.119 | 20 | 1,238,661 | 100,317 |
| compressed | omitted | 22917.121 | 20 | 158,443 | 53,329 |
| compressed | 2020-01-01 | 18890.863 | 20 | 364,316 | 47,330 |
| mixed | omitted | 23945.372 | 20 | 6,967 | 81,599 |
| mixed | 2020-01-01 | 20336.835 | 20 | 8,050 | 80,516 |

The proposed 1-second interactive target is not met by these observations.
Both existing and filtered queries are slow in this batch. Query performance
needs separate investigation; no index, storage, or query-plan tuning was
included in this feature.

Scan evidence from the plans:

- Recent baseline: approximately 5.17 million tuples examined in each price
  chunk, computed from `(Actual Rows + Rows Removed by Filter) × Actual Loops`.
  708,633 prior and 708,903 current price rows survive the scan predicates.
- Recent filtered: the prior chunk still has approximately 5.17 million tuples
  examined. Products are filtered from 116,167 to 53,343; the current chunk
  uses 315,868 index-scan loops. Adding a group cutoff does not guarantee a
  narrow snapshot scan.
- Compressed baseline: the columnar scans emit 704,884 prior and 1,412,012
  current rows across their loops; underlying compressed scans read 115,651
  and 231,302 compressed records respectively. These are compressed records,
  not counts of individual SKU prices.
- Compressed filtered: 53,343 product-driven prior-chunk probes; the current
  columnar scan still emits 1,412,012 rows. Earliest-release filtering does
  not eliminate broad compressed-data reads.
- Mixed filtered: prior columnar scan emits 704,884 rows and current sequential
  scan emits 708,903 rows; product filtering retains 53,343 products.

Counts derived from averaged EXPLAIN counters are approximate where rounded.
Full raw plans for this investigation are in
`/private/tmp/movers-bench-results.json` on the development machine.

## Result checks and remaining limits

The initial live handler run verified full 20-result pages for omitted cutoff,
`2020`, and `2020-01-01`, checking returned product group IDs and actual before/
after prices against the database. Its combined language/printing/condition/
minimum-price request returned HTTP 500 `query interrupted` while benchmarks
were also running. The test connection used a 60-second statement timeout. The underlying error
was not captured in that initial run, so a timeout is an unconfirmed explanation.

An isolated rerun passed in 147.11 seconds for the complete test suite. It
verified full pages for omitted cutoff, year shorthand, full date, combined
SKU/product filters, and offset pagination. It verified empty results for
Alpha by both group ID and code, unmapped group 9, shared group 2422 with its
2019 earliest release, and a year-9999 cutoff. Returned prices were checked
in bulk against actual snapshot values. For example, the combined-filter
request returned SKU 7451611 with 12,399 → 11,000 cents, matching the database.
This establishes the tested result behavior, not an interactive latency SLO.

Reproduce local checks with `go test ./...`. For the opt-in integration test,
set `MOVERS_TEST_DATABASE_URL` through your secret-management environment and
run `go test ./internal/handlers -run TestMoversLiveSetFilter -v -count=1`.
The test enforces read-only sessions and requires representative existing
snapshots with enough eligible movers to fill a 20-result page.

No deployment has been performed. Catalog refreshes require a rebuild and
restart. No writes, retries of writes, or partial-write states are introduced
by the read-only filter. Ingestion concurrency and sustained request load
have not been validated. The existing snapshot-completeness behavior was
preserved rather than redesigned.

## Snapshot timestamp fields (2026-09-09)

Each mover now includes `prev_snapshot_at` and `curr_snapshot_at`, populated
from the resolved snapshot variables used by the price query and serialized
in UTC. This adds no database queries or changes to the existing SQL.

`go test ./...` and the server build passed. A focused read-only integration
check verified all 20 returned movers used the exact comparison snapshots:
`2026-09-08T15:00:13Z` and `2026-09-09T15:21:57Z`. That test completed in
6.24 seconds. Run it with the integration database environment configured:
`go test ./internal/handlers -run TestMoversLiveSetFilter/snapshot_timestamps -v -count=1`.

The attempted broad live suite hit its 60-second database statement timeout
on the full-market query, so that suite did not pass during this change.
The explicit-date test fixture now selects the latest completed snapshot on
each of two distinct UTC days, matching the endpoint's date resolution.
