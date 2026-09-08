// Package setcatalog provides the set metadata bundled with the server.
package setcatalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

//go:embed scryfall-sets.json
var embedded []byte

// Catalog is immutable after loading and safe for concurrent requests.
type Catalog struct {
	releases map[int64]time.Time
}

// Load parses the catalog embedded at build time. Call once at startup.
func Load() (*Catalog, error) { return parse(embedded) }

func parse(data []byte) (*Catalog, error) {
	var document struct {
		SchemaVersion int `json:"schema_version"`
		Sets          map[string]struct {
			TCGplayerID int64  `json:"tcgplayer_id"`
			ReleasedAt  string `json:"released_at"`
		} `json:"sets"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if document.SchemaVersion != 1 || len(document.Sets) == 0 {
		return nil, fmt.Errorf("unsupported or empty set catalog")
	}
	c := &Catalog{releases: make(map[int64]time.Time)}
	for code, s := range document.Sets {
		d, err := time.Parse(time.DateOnly, s.ReleasedAt)
		if s.TCGplayerID <= 0 || err != nil || d.Year() < 1 {
			return nil, fmt.Errorf("invalid release metadata for set %q", code)
		}
		if prev, ok := c.releases[s.TCGplayerID]; !ok || d.Before(prev) {
			c.releases[s.TCGplayerID] = d
		}
	}
	return c, nil
}

// GroupsReleasedSince returns groups whose earliest known release meets cutoff.
// Missing groups never qualify. The returned slice belongs to the caller.
func (c *Catalog) GroupsReleasedSince(cutoff time.Time) []int64 {
	groups := make([]int64, 0)
	for id, released := range c.releases {
		if !released.Before(cutoff) {
			groups = append(groups, id)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i] < groups[j] })
	return groups
}
