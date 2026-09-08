package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const sampleSet = `{"object":"set","id":"set-a","code":"aaa","name":"Set A","released_at":"2020-01-01","set_type":"expansion","tcgplayer_id":42}`

func TestRefreshPreservesSharedGroupsAndExcludesUnmappedSets(t *testing.T) {
	body := `{"object":"list","has_more":false,"data":[` + sampleSet + `,
	{"object":"set","id":"set-b","code":"bbb","name":"Set B","released_at":"2021-01-01","set_type":"promo","tcgplayer_id":42,"parent_set_code":"aaa"},
	{"object":"set","id":"set-c","code":"ccc","name":"Set C","released_at":"2022-01-01","set_type":"token","digital":true},
	{"object":"set","id":"set-d","code":"ddd","name":"Set D","released_at":"2022-01-01","set_type":"token","tcgplayer_id":null}]}`
	out := filepath.Join(t.TempDir(), "sets.json")
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.Header.Get("User-Agent"), "go-price-api") || r.Header.Get("Accept") != "application/json" {
			t.Fatal("missing identifying request headers")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if err := refresh(client, "https://example.test/sets", out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var c catalog
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Sets) != 2 || c.Sets["aaa"].ReleasedAt != "2020-01-01" || c.Sets["bbb"].ParentSetCode != "aaa" {
		t.Fatalf("lost set metadata: %+v", c.Sets)
	}
	if !reflect.DeepEqual(c.TCGplayerGroups[42], []string{"aaa", "bbb"}) || len(c.TCGplayerGroups) != 1 {
		t.Fatalf("incorrect group mapping: %+v", c.TCGplayerGroups)
	}
	// Upstream ordering must not cause changes to the generated file.
	var upstream struct {
		Object  string            `json:"object"`
		HasMore bool              `json:"has_more"`
		Data    []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &upstream); err != nil {
		t.Fatal(err)
	}
	upstream.Data[0], upstream.Data[2] = upstream.Data[2], upstream.Data[0]
	reordered, err := json.Marshal(upstream)
	if err != nil {
		t.Fatal(err)
	}
	body = string(reordered)
	if err := refresh(client, "https://example.test/sets", out); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(out)
	if string(data) != string(again) {
		t.Fatal("refresh is not deterministic")
	}
}

func TestRefreshRejectsBadResponseWithoutReplacingCatalog(t *testing.T) {
	for name, body := range map[string]string{
		"invalid JSON":         `{`,
		"error response":       `{"object":"error"}`,
		"empty list":           `{"object":"list","has_more":false,"data":[]}`,
		"no mapped sets":       `{"object":"list","has_more":false,"data":[` + strings.Replace(sampleSet, `,"tcgplayer_id":42`, "", 1) + `]}`,
		"partial list":         `{"object":"list","has_more":true,"data":[` + sampleSet + `]}`,
		"missing completeness": `{"object":"list","data":[` + sampleSet + `]}`,
		"duplicate code":       `{"object":"list","has_more":false,"data":[` + sampleSet + `,` + sampleSet + `]}`,
		"invalid date":         `{"object":"list","has_more":false,"data":[` + strings.Replace(sampleSet, "2020-01-01", "2020-02-30", 1) + `]}`,
		"zero year":            `{"object":"list","has_more":false,"data":[` + strings.Replace(sampleSet, "2020-01-01", "0000-01-01", 1) + `]}`,
	} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "sets.json")
			if err := os.WriteFile(out, []byte("existing catalog"), 0644); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			if err := refresh(client, "https://example.test/sets", out); err == nil {
				t.Fatal("accepted invalid catalog")
			}
			data, _ := os.ReadFile(out)
			if string(data) != "existing catalog" {
				t.Fatal("overwrote existing catalog")
			}
		})
	}
}
