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
	Admission             *AdmissionContract     `json:"admission,omitempty"`
	Version               string                 `json:"version"`
	UpdatedAt             string                 `json:"updatedAt"`
	Source                string                 `json:"source"`
	Demo                  bool                   `json:"demo"`
	RankingAuthority      string                 `json:"rankingAuthority,omitempty"`
	SourceSnapshot        *SourceSnapshot        `json:"sourceSnapshot,omitempty"`
	DefaultRanking        string                 `json:"defaultRanking,omitempty"`
	RankingKey            string                 `json:"rankingKey,omitempty"`
	Rankings              map[string]Ranking     `json:"rankings,omitempty"`
	Models                []Model                `json:"models"`
	OfficialModels        []Model                `json:"officialModels,omitempty"`
	Sites                 []Site                 `json:"sites"`
	Relays                []Site                 `json:"relays,omitempty"`
	Resources             []ResourceOffer        `json:"resources,omitempty"`
	Offers                []Offer                `json:"offers"`
	OfficialSubscriptions []OfficialSubscription `json:"officialSubscriptions,omitempty"`
	SubscriptionMerchants []MerchantOffer        `json:"subscriptionMerchants,omitempty"`
	// CollectionVocabulary is the Chaos navigation projection attached on read.
	// Normalize drops any imported copy so the stored snapshot cannot replace the contract.
	CollectionVocabulary *CollectionVocabulary `json:"collectionVocabulary,omitempty"`
	// Summary is a read projection of this admitted snapshot, never imported claims.
	Summary *CatalogSummary `json:"summary,omitempty"`
	Home    *HomeProjection `json:"home,omitempty"`
}
type Model struct {
	SubjectGovernance
	ID               string             `json:"id"`
	EntityID         string             `json:"entityId,omitempty"`
	CanonicalModelID string             `json:"canonicalModelId,omitempty"`
	ModelPermaslug   string             `json:"modelPermaslug,omitempty"`
	SourceModelID    string             `json:"sourceModelId,omitempty"`
	SourceKey        string             `json:"sourceKey,omitempty"`
	DeveloperID      string             `json:"developerId,omitempty"`
	DeveloperName    string             `json:"developerName,omitempty"`
	Name             string             `json:"name"`
	Provider         string             `json:"provider"`
	Family           string             `json:"family"`
	Description      string             `json:"description"`
	Context          *float64           `json:"context"`
	MaxInputTokens   *float64           `json:"maxInputTokens"`
	MaxOutputTokens  *float64           `json:"maxOutputTokens"`
	Modalities       *ModelModalities   `json:"modalities"`
	Capabilities     *ModelCapabilities `json:"capabilities,omitempty"`
	Status           string             `json:"status,omitempty"`
	LastUpdated      *string            `json:"lastUpdated"`
	Input            *float64           `json:"input"`
	Output           *float64           `json:"output"`
	Tags             []string           `json:"tags"`
	Released         *string            `json:"released"`
	Rank             *int               `json:"rank"`
	Score            *float64           `json:"score"`
	SourceURL        string             `json:"sourceUrl,omitempty"`
	SourceName       string             `json:"sourceName,omitempty"`
	SourceAuthority  string             `json:"sourceAuthority,omitempty"`
	ObservedAt       *string            `json:"observedAt"`
	Benchmarks       *ModelBenchmarks   `json:"benchmarks,omitempty"`
	Billing          json.RawMessage    `json:"billing,omitempty"`
	ModelDial        *Evaluation        `json:"modelDial,omitempty"`
	Evaluations      []Evaluation       `json:"evaluations,omitempty"`
	QuoteID          string             `json:"quoteId,omitempty"`
	QuotePeriod      string             `json:"quotePeriod,omitempty"`
	QuoteTimeZone    string             `json:"quoteTimeZone,omitempty"`
}
type Site struct {
	SubjectGovernance
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Initials        string   `json:"initials"`
	Description     string   `json:"description"`
	Color           string   `json:"color"`
	Ratio           *float64 `json:"ratio"`
	Uptime          *float64 `json:"uptime"`
	Latency         *float64 `json:"latency"`
	Models          int      `json:"models"`
	Tags            []string `json:"tags"`
	Region          string   `json:"region"`
	Website         string   `json:"website"`
	Rank            *int     `json:"rank"`
	Score           *float64 `json:"score"`
	EntityID        string   `json:"entityId,omitempty"`
	ProviderKey     string   `json:"providerKey,omitempty"`
	ProviderKind    string   `json:"providerKind,omitempty"`
	SourceURL       string   `json:"sourceUrl,omitempty"`
	SourceName      string   `json:"sourceName,omitempty"`
	SourceAuthority string   `json:"sourceAuthority,omitempty"`
	ObservedAt      *string  `json:"observedAt"`
}
type Offer struct {
	SubjectGovernance
	ID              string          `json:"id"`
	ModelID         string          `json:"modelId"`
	SiteID          string          `json:"siteId"`
	Input           *float64        `json:"input"`
	Output          *float64        `json:"output"`
	Currency        string          `json:"currency"`
	Unit            string          `json:"unit"`
	UpdatedAt       *string         `json:"updatedAt"`
	Source          string          `json:"source"`
	SourceModelID   string          `json:"sourceModelId,omitempty"`
	SourceURL       string          `json:"sourceUrl,omitempty"`
	SourceName      string          `json:"sourceName,omitempty"`
	SourceAuthority string          `json:"sourceAuthority,omitempty"`
	CacheRead       *float64        `json:"cacheRead"`
	CacheWrite      *float64        `json:"cacheWrite"`
	Condition       json.RawMessage `json:"condition,omitempty"`
	SourceMetadata  json.RawMessage `json:"sourceMetadata,omitempty"`
	Billing         json.RawMessage `json:"billing,omitempty"`
}

type SourceSnapshot struct {
	Product             string          `json:"product"`
	Version             string          `json:"version"`
	PublicationRevision string          `json:"publicationRevision"`
	ConfigRevision      string          `json:"configRevision"`
	Config              json.RawMessage `json:"config"`
}

type Ranking struct {
	Domain   string          `json:"domain"`
	Topic    string          `json:"topic"`
	View     string          `json:"view,omitempty"`
	Title    string          `json:"title"`
	Slug     string          `json:"slug"`
	Version  string          `json:"version"`
	AsOf     string          `json:"asOf"`
	Source   string          `json:"source"`
	Unit     string          `json:"unit"`
	Entries  []RankedEntry   `json:"entries"`
	Metadata json.RawMessage `json:"metadata"`
}

type RankedEntry struct {
	EntityID string          `json:"entityId"`
	Name     string          `json:"name"`
	Rank     *int            `json:"rank"`
	Score    *float64        `json:"score"`
	Metadata json.RawMessage `json:"metadata"`
}

type ModelBenchmarks struct {
	ArtificialAnalysis *ArtificialAnalysis `json:"artificialAnalysis,omitempty"`
}

type ModelModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type ArtificialAnalysis struct {
	IntelligenceIndex *float64 `json:"intelligenceIndex"`
	CodingIndex       *float64 `json:"codingIndex"`
	AgenticIndex      *float64 `json:"agenticIndex"`
}

type Evaluation struct {
	SubjectGovernance
	EntityID               string                    `json:"entityId"`
	Source                 string                    `json:"source"`
	BenchmarkID            string                    `json:"benchmarkId"`
	CanonicalModelID       string                    `json:"canonicalModelId,omitempty"`
	ModelPermaslug         string                    `json:"modelPermaslug"`
	ModelName              string                    `json:"modelName"`
	ProviderName           string                    `json:"providerName"`
	ConfigurationID        string                    `json:"configurationId,omitempty"`
	ReasoningEffort        string                    `json:"reasoningEffort,omitempty"`
	Dimensions             *EvaluationDimensions     `json:"dimensions,omitempty"`
	Configurations         []EvaluationConfiguration `json:"configurations,omitempty"`
	Released               *string                   `json:"released,omitempty"`
	Metric                 string                    `json:"metric"`
	Score                  *float64                  `json:"score"`
	ScoreUnit              string                    `json:"scoreUnit"`
	ScoreStdDev            *float64                  `json:"scoreStdDev"`
	RunCount               *int64                    `json:"runCount"`
	TotalTasks             *int64                    `json:"totalTasks"`
	MeasuredAt             *string                   `json:"measuredAt"`
	ObservedAt             *string                   `json:"observedAt"`
	SourceURL              string                    `json:"sourceUrl"`
	AvgCostPerTaskUSD      *float64                  `json:"avgCostPerTaskUsd"`
	AvgDurationPerTaskMS   *float64                  `json:"avgDurationPerTaskMs"`
	AvgOutputTokensPerTask *float64                  `json:"avgOutputTokensPerTask"`
	Publication            EvaluationPublication     `json:"publication"`
}

type EvaluationPublication struct {
	ManifestURL   string          `json:"manifestUrl"`
	DataURL       string          `json:"dataUrl"`
	Dataset       string          `json:"dataset"`
	SchemaVersion string          `json:"schemaVersion"`
	SnapshotDate  string          `json:"snapshotDate"`
	RetrievedAt   *string         `json:"retrievedAt"`
	SHA256        string          `json:"sha256"`
	License       SourceLicense   `json:"license"`
	Attribution   string          `json:"attribution"`
	Methodology   string          `json:"methodology"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

type SourceLicense struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type OfficialSubscription struct {
	SubjectGovernance
	EntityID          string               `json:"entityId"`
	Vendor            string               `json:"vendor"`
	PlanID            string               `json:"planId"`
	PlanName          string               `json:"planName"`
	Price             *float64             `json:"price"`
	Currency          string               `json:"currency"`
	BillingInterval   string               `json:"billingInterval"`
	BillingUnit       string               `json:"billingUnit,omitempty"`
	AnnualTotal       *float64             `json:"annualTotal"`
	MonthlyEquivalent *float64             `json:"monthlyEquivalent"`
	Region            string               `json:"region"`
	Conditions        []string             `json:"conditions"`
	Evidence          SubscriptionEvidence `json:"evidence"`
}

type SubscriptionEvidence struct {
	SourceURL   string  `json:"sourceUrl"`
	CapturedAt  *string `json:"capturedAt"`
	BodySHA256  string  `json:"bodySha256"`
	Quote       string  `json:"quote"`
	QuoteSHA256 string  `json:"quoteSha256"`
}

// Decode rejects missing/null fields as well as malformed or oversized snapshots.
func Decode(r io.Reader) (Catalog, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxCatalogBytes+1))
	if err != nil {
		return Catalog{}, err
	}
	if len(data) > MaxCatalogBytes {
		return Catalog{}, errors.New("catalog exceeds 8 MiB limit")
	}
	return decodeCatalog(data)
}

// DecodeStored reads trusted complete catalogs produced by Sync. Public/admin
// import continues to use Decode's existing 8 MiB request boundary.
func DecodeStored(r io.Reader) (Catalog, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Catalog{}, err
	}
	return decodeCatalog(data)
}

func decodeCatalog(data []byte) (Catalog, error) {
	var c Catalog
	var err error
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
	fieldsByKind := map[string]string{
		"models": "id name provider family description context input output tags released score",
		"sites":  "id name initials description color ratio tags region website score",
		"offers": "id modelId siteId input output currency unit updatedAt source",
	}
	if c.RankingAuthority == "chaos" {
		fieldsByKind = map[string]string{"models": "id name", "sites": "id name", "offers": "id modelId siteId currency unit"}
	}
	for key, fields := range fieldsByKind {
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
func optionalNumber(v *float64) bool { return v == nil || finiteNonnegative(*v) }
func finiteScore(v *float64, authoritative bool) bool {
	return v == nil || !math.IsNaN(*v) && !math.IsInf(*v, 0) && (authoritative || *v >= 0)
}
func timestamp(s string) bool  { _, err := time.Parse(time.RFC3339Nano, s); return err == nil }
func identifier(s string) bool { return strings.TrimSpace(s) == s && s != "" && len(s) <= 200 }
func webURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

// Normalize validates facts. Only the legacy catalog derives ranks/coverage;
// authoritative Chaos publications retain their exact scores, ranks and order.
func Normalize(c Catalog) (Catalog, error) {
	c.CollectionVocabulary = nil
	c.Summary = nil
	c.Home = nil
	c.Relays = nil
	c.OfficialModels = nil
	authoritative := c.RankingAuthority == "chaos"
	if strings.TrimSpace(c.Version) == "" || strings.TrimSpace(c.Source) == "" || !(authoritative && c.UpdatedAt == "") && !timestamp(c.UpdatedAt) {
		return c, errors.New("version, source and RFC3339 updatedAt are required")
	}
	if c.Models == nil || c.Sites == nil || c.Offers == nil {
		return c, errors.New("models, sites and offers must be arrays")
	}
	models, sites, offers, pairs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	coverage := map[string]int{}
	for i := range c.Models {
		m := &c.Models[i]
		m.QuoteID, m.QuotePeriod, m.QuoteTimeZone = "", "", ""
		if !identifier(m.ID) || models[m.ID] || strings.TrimSpace(m.Name) == "" || !authoritative && strings.TrimSpace(m.Provider) == "" {
			return c, fmt.Errorf("invalid/duplicate model %q", m.ID)
		}
		if !optionalNumber(m.Context) || !optionalNumber(m.Input) || !optionalNumber(m.Output) || !finiteScore(m.Score, authoritative) || !authoritative && (m.Context == nil || m.Input == nil || m.Output == nil || m.Score == nil) || m.Context != nil && (!authoritative && *m.Context < 1 || *m.Context != math.Trunc(*m.Context) || *m.Context > 9007199254740991) {
			return c, fmt.Errorf("invalid model numbers: %s", m.ID)
		}
		if !authoritative && m.Released == nil {
			return c, fmt.Errorf("release date is required: %s", m.ID)
		}
		if m.Released != nil && !authoritative && func() bool { _, err := time.Parse("2006-01-02", *m.Released); return err != nil }() {
			return c, fmt.Errorf("invalid release date: %s", m.ID)
		}
		if m.Tags == nil {
			m.Tags = []string{}
		}
		models[m.ID] = true
	}
	for i := range c.Sites {
		s := &c.Sites[i]
		if !identifier(s.ID) || sites[s.ID] || strings.TrimSpace(s.Name) == "" || !authoritative && !webURL(s.Website) {
			return c, fmt.Errorf("invalid/duplicate site %q", s.ID)
		}
		if !optionalNumber(s.Ratio) || !finiteScore(s.Score, authoritative) || !authoritative && (s.Ratio == nil || s.Score == nil) || s.Uptime != nil && (!finiteNonnegative(*s.Uptime) || *s.Uptime > 100) || !optionalNumber(s.Latency) {
			return c, fmt.Errorf("invalid site numbers: %s", s.ID)
		}
		if s.Tags == nil {
			s.Tags = []string{}
		}
		sites[s.ID] = true
	}
	for _, o := range c.Offers {
		pair := o.ModelID + "\x00" + o.SiteID
		if !identifier(o.ID) || offers[o.ID] || !authoritative && pairs[pair] || !models[o.ModelID] || !sites[o.SiteID] {
			return c, fmt.Errorf("invalid/duplicate offer or reference: %s", o.ID)
		}
		if !optionalNumber(o.Input) || !optionalNumber(o.Output) || !optionalNumber(o.CacheRead) || !optionalNumber(o.CacheWrite) || !authoritative && (o.Input == nil || o.Output == nil || o.UpdatedAt == nil || strings.TrimSpace(o.Source) == "") || o.Currency != "USD" || o.Unit != "1M tokens" || o.UpdatedAt != nil && !timestamp(*o.UpdatedAt) {
			return c, fmt.Errorf("invalid offer price/metadata: %s", o.ID)
		}
		offers[o.ID], pairs[pair] = true, true
		coverage[o.SiteID]++
	}
	if authoritative {
		return c, nil
	}
	sort.Slice(c.Models, func(i, j int) bool {
		a, b := c.Models[i], c.Models[j]
		if *a.Score == *b.Score {
			return a.ID < b.ID
		}
		return *a.Score > *b.Score
	})
	sort.Slice(c.Sites, func(i, j int) bool {
		a, b := c.Sites[i], c.Sites[j]
		if *a.Score == *b.Score {
			return a.ID < b.ID
		}
		return *a.Score > *b.Score
	})
	sort.Slice(c.Offers, func(i, j int) bool { return c.Offers[i].ID < c.Offers[j].ID })
	for i := range c.Models {
		rank := i + 1
		c.Models[i].Rank = &rank
	}
	for i := range c.Sites {
		rank := i + 1
		c.Sites[i].Rank = &rank
		c.Sites[i].Models = coverage[c.Sites[i].ID]
	}
	return c, nil
}
