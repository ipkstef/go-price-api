package setcatalog

import (
	"reflect"
	"testing"
	"time"
)

func TestGroupsReleasedSinceUsesEarliestDateAndInclusiveBoundary(t *testing.T) {
	c, err := parse([]byte(`{"schema_version":1,"sets":{
	"old":{"tcgplayer_id":1,"released_at":"2019-01-01"},
	"reprint":{"tcgplayer_id":1,"released_at":"2022-01-01"},
	"boundary":{"tcgplayer_id":2,"released_at":"2020-01-01"},
	"new":{"tcgplayer_id":3,"released_at":"2021-01-01"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	cutoff, _ := time.Parse(time.DateOnly, "2020-01-01")
	if got := c.GroupsReleasedSince(cutoff); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Fatalf("eligible groups = %v, want [2 3]", got)
	}
}

func TestLoadEmbeddedCatalog(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.releases[7].Format(time.DateOnly); got != "1993-08-05" {
		t.Fatalf("Alpha release = %s", got)
	}
	if got := c.releases[2422].Format(time.DateOnly); got != "2019-06-14" {
		t.Fatalf("shared group release = %s", got)
	}
}

func TestRejectInvalidCatalog(t *testing.T) {
	for _, input := range []string{
		`{`, `{"schema_version":2,"sets":{}}`, `{"schema_version":1,"sets":{}}`,
		`{"schema_version":1,"sets":{"x":{"released_at":"2020-01-01"}}}`,
		`{"schema_version":1,"sets":{"x":{"tcgplayer_id":7,"released_at":"2020-02-30"}}}`,
	} {
		if _, err := parse([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}
