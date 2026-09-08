// Command update-sets refreshes the local Scryfall set catalog.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const sourceURL = "https://api.scryfall.com/sets"

type set struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	ReleasedAt    string `json:"released_at"`
	SetType       string `json:"set_type"`
	TCGplayerID   *int64 `json:"tcgplayer_id,omitempty"`
	ParentSetCode string `json:"parent_set_code,omitempty"`
	Digital       bool   `json:"digital"`
}

type catalog struct {
	SchemaVersion   int                `json:"schema_version"`
	Source          string             `json:"source"`
	Sets            map[string]set     `json:"sets"`
	TCGplayerGroups map[int64][]string `json:"tcgplayer_groups"`
}

func main() {
	output := flag.String("output", "data/scryfall-sets.json", "catalog output path")
	flag.Parse()
	if err := refresh(&http.Client{Timeout: 60 * time.Second}, sourceURL, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Updated", *output)
}

func refresh(client *http.Client, source, output string) error {
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-price-api/1.0 (set-catalog updater)")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch sets: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch sets: HTTP %d", resp.StatusCode)
	}
	const maxBytes = 16 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("read sets: %w", err)
	}
	if len(body) > maxBytes {
		return fmt.Errorf("set response exceeds %d bytes", maxBytes)
	}
	var list struct {
		Object  string `json:"object"`
		HasMore *bool  `json:"has_more"`
		Data    []set  `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return fmt.Errorf("decode sets: %w", err)
	}
	if list.Object != "list" || list.HasMore == nil || *list.HasMore || len(list.Data) == 0 {
		return fmt.Errorf("expected a complete, nonempty Scryfall set list")
	}
	c := catalog{SchemaVersion: 1, Source: source, Sets: make(map[string]set), TCGplayerGroups: make(map[int64][]string)}
	ids := make(map[string]bool)
	for _, s := range list.Data {
		if s.ID == "" || s.Code == "" || s.Code != strings.ToLower(strings.TrimSpace(s.Code)) || s.Name == "" || s.SetType == "" {
			return fmt.Errorf("set %q has missing or invalid metadata", s.Code)
		}
		released, err := time.Parse(time.DateOnly, s.ReleasedAt)
		if err != nil {
			return fmt.Errorf("set %q has invalid release date: %w", s.Code, err)
		}
		if released.Year() < 1 {
			return fmt.Errorf("set %q has invalid release year", s.Code)
		}
		if _, exists := c.Sets[s.Code]; exists || ids[s.ID] {
			return fmt.Errorf("duplicate set code or ID: %q", s.Code)
		}
		ids[s.ID] = true
		if s.TCGplayerID == nil {
			continue
		}
		if *s.TCGplayerID <= 0 {
			return fmt.Errorf("set %q has invalid TCGplayer ID", s.Code)
		}
		c.Sets[s.Code] = s
		c.TCGplayerGroups[*s.TCGplayerID] = append(c.TCGplayerGroups[*s.TCGplayerID], s.Code)
	}
	if len(c.Sets) == 0 {
		return fmt.Errorf("set response contains no sets with a TCGplayer ID")
	}
	for _, codes := range c.TCGplayerGroups {
		sort.Strings(codes)
	}
	// encoding/json sorts map keys, making refreshes stable across source ordering.
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(output), ".sets-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0644); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), output)
}
