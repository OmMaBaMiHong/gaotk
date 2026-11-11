package tokencenter

import "encoding/json"

// ResourceOffer is an admitted Chaos projection. Names, classifications,
// amounts, conditions and evidence are copied without product-side governance.
type ResourceOffer struct {
	SubjectGovernance
	EntityID        string   `json:"entityId"`
	Key             string   `json:"key"`
	Provider        string   `json:"provider"`
	Name            string   `json:"name"`
	Category        string   `json:"category"`
	CollectionSlug  string   `json:"collectionSlug"`
	Price           *float64 `json:"price"`
	PriceBasis      string   `json:"priceBasis,omitempty"`
	Currency        string   `json:"currency"`
	Unit            string   `json:"unit"`
	Condition       string   `json:"condition"`
	MinimumQuantity *float64 `json:"minimumQuantity,omitempty"`
	QuantityUnit    string   `json:"quantityUnit,omitempty"`
	Region          string   `json:"region,omitempty"`
	Availability    string   `json:"availability,omitempty"`
	Evidence        struct {
		SourceURL  string `json:"sourceUrl"`
		CapturedAt string `json:"capturedAt"`
		SHA256     string `json:"sha256"`
		Quote      string `json:"quote"`
		Field      string `json:"field"`
	} `json:"evidence"`
}

func (a *chaosAccumulator) appendResource(raw json.RawMessage) error {
	var offer ResourceOffer
	if err := json.Unmarshal(raw, &offer); err != nil {
		return err
	}
	a.catalog.Resources = append(a.catalog.Resources, offer)
	return nil
}
