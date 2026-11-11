package tokencenter

import (
	"encoding/json"
	"errors"
)

// The product stores the published comparison intact, including source order,
// original currencies and ineligible observations. It does not calculate ranks.
type RegionalSubscriptionCatalog struct {
	SubjectGovernance
	EntityID    string          `json:"entityId"`
	Key         string          `json:"key"`
	Vendor      string          `json:"vendor"`
	Channel     string          `json:"channel"`
	Storefronts []string        `json:"storefronts"`
	FX          *ReferenceRates `json:"fx,omitempty"`
	Plans       []RegionalPlan  `json:"plans"`
}

type ReferenceRates struct {
	SourceURL string             `json:"sourceUrl"`
	SHA256    string             `json:"sha256"`
	Date      string             `json:"date"`
	Rates     map[string]float64 `json:"ratesPerEur"`
}

type RegionalPlan struct {
	Key               string          `json:"key"`
	Name              string          `json:"name"`
	BillingInterval   string          `json:"billingInterval"`
	PeriodSourceURL   string          `json:"periodSourceUrl,omitempty"`
	UnrankedReason    string          `json:"unrankedReason,omitempty"`
	Regions           int             `json:"regions"`
	ComparableRegions int             `json:"comparableRegions"`
	SavingsVsUS       *float64        `json:"savingsVsUs,omitempty"`
	Quotes            []RegionalQuote `json:"quotes"`
}

type RegionalQuote struct {
	Key      string               `json:"key"`
	Region   string               `json:"region"`
	Price    float64              `json:"price"`
	Currency string               `json:"currency"`
	Original string               `json:"original"`
	USD      *float64             `json:"usd,omitempty"`
	CNY      *float64             `json:"cny,omitempty"`
	Rank     *int                 `json:"rank,omitempty"`
	Evidence SubscriptionEvidence `json:"evidence"`
}

func (a *chaosAccumulator) appendRegionalSubscriptions(raw json.RawMessage) error {
	var publication struct {
		SchemaVersion int `json:"schemaVersion"`
		RegionalSubscriptionCatalog
	}
	if err := json.Unmarshal(raw, &publication); err != nil || publication.SchemaVersion != 1 || publication.EntityID == "" || publication.Key == "" || publication.Vendor == "" || publication.Channel == "" || len(publication.Plans) == 0 {
		return errors.New("Chaos regional subscription publication is missing or unsupported")
	}
	for _, plan := range publication.Plans {
		for _, quote := range plan.Quotes {
			a.observeOptional(quote.Evidence.CapturedAt)
		}
	}
	a.catalog.RegionalSubscriptions = append(a.catalog.RegionalSubscriptions, publication.RegionalSubscriptionCatalog)
	return nil
}
