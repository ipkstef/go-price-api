# Scryfall set catalog

`scryfall-sets.json` is a generated catalog from <https://api.scryfall.com/sets>.
Refresh it from the repository root:

```sh
go run ./cmd/update-sets
```

To preview a refresh separately:

```sh
go run ./cmd/update-sets -output /tmp/scryfall-sets.json
```

The command sends `User-Agent: go-price-api/1.0 (set-catalog updater)` and
`Accept: application/json`. It validates the complete response before replacing
the output with an atomic rename. HTTP errors, incomplete lists, duplicate
identities, invalid dates, and malformed JSON leave the existing file intact.
Map keys and group code lists are sorted so source ordering does not create diffs.
Review the generated diff after refreshing; edit the updater, not the JSON.

## Structure

- `schema_version`: local format version, currently `1`.
- `source`: upstream URL.
- `sets`: keyed by lowercase Scryfall set code. Each record retains the Scryfall
  ID, code, name, release date, set type, digital flag, TCGplayer group ID,
  and optional parent set code.
- `tcgplayer_groups`: keyed by decimal TCGplayer group ID, with a list of
  Scryfall codes for each group. All matching codes are retained.

For example, `sets["lea"].released_at` gives Alpha's release date. Use
`tcgplayer_groups["7"]` to find its Scryfall code via the TCGplayer ID.
Do not assume Scryfall codes equal database abbreviations.

Sets without a TCGplayer ID are excluded on every refresh. A response with no
mapped sets is rejected, preserving the existing catalog. Parent set codes are
upstream references and may refer to excluded sets. Some TCGplayer groups
represent multiple Scryfall sets, sometimes
with different dates; the index deliberately does not choose one release date.
An absent mapping means unknown, not old. Future dates are retained as supplied.

## Access and lifecycle

The filtered catalog contains 374 sets across 349 TCGplayer group IDs from the
initial 1,049-set source response. Refresh is an explicit, occasional
operation: one HTTP read and one full local file replacement, with a 60-second
request timeout. No scheduled refresh or database writes are configured.

The catalog is embedded in the Go server binary and parsed once at startup.
Refresh the JSON, then rebuild and restart the server to activate new metadata.
Startup fails if the bundled metadata is invalid. There is no hot/archived split
for this metadata file; it represents the latest fetched metadata, not a
historical series.

> **No consumer as of 2026-09-27.** `set_released_since` existed only on
> `GET /prices/movers`, which was removed along with the handler code that read
> this catalog. The package is still loaded and passed to `PriceHandler`, but
> nothing reads it. Keep it only if a set-release filter is coming back on
> another endpoint; otherwise this package, `cmd/update-sets`, and the
> `SetCatalog` field are all removable.

`set_released_since` excluded unmapped groups and used the earliest release date
for groups with multiple sets, accepting `YYYY` (January 1) or `YYYY-MM-DD`
inclusively. It is no longer exposed by any endpoint. The historical measurements
behind it are in
[movers validation](../docs/movers-set-filter-validation.md), kept as a record of
why the movers design changed twice.
