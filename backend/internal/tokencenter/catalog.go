// Package tokencenter owns the public directory, independently of account billing/pricing.
package tokencenter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

const MaxCatalogBytes = 8 << 20

var ErrEmpty = errors.New("catalog has not been imported")
var ErrNoSource = errors.New("TOKENCENTER_SOURCE_URL is not configured")

type Catalog struct {
	Version   string  `json:"version"`
	UpdatedAt string  `json:"updatedAt"`
	Source    string  `json:"source"`
	Demo      bool    `json:"demo"`
	Models    []Model `json:"models"`
	Sites     []Site  `json:"sites"`
	Offers    []Offer `json:"offers"`
}
type Model struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Provider    string   `json:"provider"`
	Family      string   `json:"family"`
	Description string   `json:"description"`
	Context     float64  `json:"context"`
	Input       float64  `json:"input"`
	Output      float64  `json:"output"`
	Tags        []string `json:"tags"`
	Released    string   `json:"released"`
	Rank        int      `json:"rank"`
	Score       float64  `json:"score"`
}
type Site struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Initials    string   `json:"initials"`
	Description string   `json:"description"`
	Color       string   `json:"color"`
	Ratio       float64  `json:"ratio"`
	Uptime      *float64 `json:"uptime"`
	Latency     *float64 `json:"latency"`
	Models      int      `json:"models"`
	Tags        []string `json:"tags"`
	Region      string   `json:"region"`
	Website     string   `json:"website"`
	Rank        int      `json:"rank"`
	Score       float64  `json:"score"`
}
type Offer struct {
	ID        string  `json:"id"`
	ModelID   string  `json:"modelId"`
	SiteID    string  `json:"siteId"`
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	Currency  string  `json:"currency"`
	Unit      string  `json:"unit"`
	UpdatedAt string  `json:"updatedAt"`
	Source    string  `json:"source"`
}

// Decode rejects missing/null fields as well as malformed or oversized snapshots.
func Decode(r io.Reader) (Catalog, error) {
	var c Catalog
	data, err := io.ReadAll(io.LimitReader(r, MaxCatalogBytes+1))
	if err != nil {
		return c, err
	}
	if len(data) > MaxCatalogBytes {
		return c, errors.New("catalog exceeds 8 MiB limit")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&c); err != nil {
		return c, fmt.Errorf("invalid catalog JSON: %w", err)
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return c, errors.New("expected a single catalog JSON object")
	}
	var root map[string]json.RawMessage
	if err = json.Unmarshal(data, &root); err != nil {
		return c, err
	}
	if err = required(root, "version updatedAt source demo models sites offers", ""); err != nil {
		return c, err
	}
	for key, fields := range map[string]string{
		"models": "id name provider family description context input output tags released score",
		"sites":  "id name initials description color ratio tags region website score",
		"offers": "id modelId siteId input output currency unit updatedAt source",
	} {
		var entries []map[string]json.RawMessage
		if err = json.Unmarshal(root[key], &entries); err != nil {
			return c, err
		}
		for i, entry := range entries {
			if err = required(entry, fields, fmt.Sprintf("%s[%d].", key, i)); err != nil {
				return c, err
			}
		}
	}
	return Normalize(c)
}
func required(m map[string]json.RawMessage, fields, prefix string) error {
	for _, k := range strings.Fields(fields) {
		if len(m[k]) == 0 || bytes.Equal(bytes.TrimSpace(m[k]), []byte("null")) {
			return fmt.Errorf("%s%s is required", prefix, k)
		}
	}
	return nil
}
func finiteNonnegative(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return false
		}
	}
	return true
}
func timestamp(s string) bool  { _, err := time.Parse(time.RFC3339Nano, s); return err == nil }
func identifier(s string) bool { return strings.TrimSpace(s) == s && s != "" && len(s) <= 200 }
func webURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

// Normalize validates references and prices, then derives ranks and coverage from offers.
func Normalize(c Catalog) (Catalog, error) {
	if strings.TrimSpace(c.Version) == "" || strings.TrimSpace(c.Source) == "" || !timestamp(c.UpdatedAt) {
		return c, errors.New("version, source and RFC3339 updatedAt are required")
	}
	if c.Models == nil || c.Sites == nil || c.Offers == nil {
		return c, errors.New("models, sites and offers must be arrays")
	}
	models, sites, offers, pairs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	coverage := map[string]int{}
	for i := range c.Models {
		m := &c.Models[i]
		if !identifier(m.ID) || models[m.ID] || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Provider) == "" {
			return c, fmt.Errorf("invalid/duplicate model %q", m.ID)
		}
		if !finiteNonnegative(m.Context, m.Input, m.Output, m.Score) || m.Context < 1 || m.Context != math.Trunc(m.Context) || m.Context > 9007199254740991 {
			return c, fmt.Errorf("invalid model numbers: %s", m.ID)
		}
		if _, err := time.Parse("2006-01-02", m.Released); err != nil {
			return c, fmt.Errorf("invalid release date: %s", m.ID)
		}
		if m.Tags == nil {
			m.Tags = []string{}
		}
		models[m.ID] = true
	}
	for i := range c.Sites {
		s := &c.Sites[i]
		if !identifier(s.ID) || sites[s.ID] || strings.TrimSpace(s.Name) == "" || !webURL(s.Website) {
			return c, fmt.Errorf("invalid/duplicate site %q", s.ID)
		}
		if !finiteNonnegative(s.Ratio, s.Score) || s.Uptime != nil && (!finiteNonnegative(*s.Uptime) || *s.Uptime > 100) || s.Latency != nil && !finiteNonnegative(*s.Latency) {
			return c, fmt.Errorf("invalid site numbers: %s", s.ID)
		}
		if s.Tags == nil {
			s.Tags = []string{}
		}
		sites[s.ID] = true
	}
	for _, o := range c.Offers {
		pair := o.ModelID + "\x00" + o.SiteID
		if !identifier(o.ID) || offers[o.ID] || pairs[pair] || !models[o.ModelID] || !sites[o.SiteID] {
			return c, fmt.Errorf("invalid/duplicate offer or reference: %s", o.ID)
		}
		if !finiteNonnegative(o.Input, o.Output) || o.Currency != "USD" || o.Unit != "1M tokens" || !timestamp(o.UpdatedAt) || strings.TrimSpace(o.Source) == "" {
			return c, fmt.Errorf("invalid offer price/metadata: %s", o.ID)
		}
		offers[o.ID], pairs[pair] = true, true
		coverage[o.SiteID]++
	}
	sort.Slice(c.Models, func(i, j int) bool {
		a, b := c.Models[i], c.Models[j]
		if a.Score == b.Score {
			return a.ID < b.ID
		}
		return a.Score > b.Score
	})
	sort.Slice(c.Sites, func(i, j int) bool {
		a, b := c.Sites[i], c.Sites[j]
		if a.Score == b.Score {
			return a.ID < b.ID
		}
		return a.Score > b.Score
	})
	sort.Slice(c.Offers, func(i, j int) bool { return c.Offers[i].ID < c.Offers[j].ID })
	for i := range c.Models {
		c.Models[i].Rank = i + 1
	}
	for i := range c.Sites {
		c.Sites[i].Rank = i + 1
		c.Sites[i].Models = coverage[c.Sites[i].ID]
	}
	return c, nil
}
