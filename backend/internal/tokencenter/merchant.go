package tokencenter

import (
	"encoding/json"
	"errors"
)

type MerchantOffer struct {
	SubjectGovernance
	ID              string          `json:"id"`
	MerchantID      string          `json:"merchantId"`
	Name            string          `json:"name"`
	Initials        string          `json:"initials"`
	Family          string          `json:"family"`
	Plan            string          `json:"plan"`
	Price           *float64        `json:"price"`
	Currency        string          `json:"currency"`
	Stock           *bool           `json:"stock"`
	StockQuantity   *int64          `json:"stockQuantity"`
	Delivery        string          `json:"delivery"`
	Type            string          `json:"type"`
	Description     string          `json:"description"`
	Aftercare       string          `json:"aftercare"`
	Period          string          `json:"period"`
	SourceURL       string          `json:"sourceUrl"`
	MerchantURL     string          `json:"merchantUrl"`
	ObservedAt      *string         `json:"observedAt"`
	NativeProductID string          `json:"nativeProductId"`
	SourceName      string          `json:"sourceName"`
	Conditions      json.RawMessage `json:"conditions,omitempty"`
	Evidence        json.RawMessage `json:"evidence,omitempty"`
}

func (a *chaosAccumulator) appendMerchant(raw json.RawMessage) error {
	var data struct {
		SchemaVersion int `json:"schemaVersion"`
		Merchant      struct {
			SubjectGovernance
			EntityID, Name, Website, SourceURL string
			ObservedAt                         *string `json:"observedAt"`
		} `json:"merchant"`
		Products []struct {
			SubjectGovernance
			EntityID, NativeID, Name, Description, Currency, Period, Aftercare, ProductURL string
			Price                                                                          *float64               `json:"price"`
			Stock                                                                          *bool                  `json:"stock"`
			StockQuantity                                                                  *int64                 `json:"stockQuantity"`
			Delivery                                                                       struct{ Label string } `json:"delivery"`
			Conditions                                                                     json.RawMessage        `json:"conditions"`
		} `json:"products"`
		Evidence json.RawMessage `json:"evidence"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	if data.SchemaVersion != 1 || data.Merchant.EntityID == "" || data.Merchant.Name == "" || data.Products == nil {
		return errors.New("Chaos merchant catalog identity/schema missing")
	}
	for _, product := range data.Products {
		if product.EntityID == "" || product.Name == "" || !optionalNumber(product.Price) {
			return errors.New("Chaos merchant product identity/price invalid")
		}
		for _, old := range a.catalog.SubscriptionMerchants {
			if old.ID == product.EntityID {
				return errors.New("Chaos merchant product duplicated")
			}
		}
		name := []rune(data.Merchant.Name)
		url := product.ProductURL
		if url == "" {
			url = data.Merchant.SourceURL
		}
		a.catalog.SubscriptionMerchants = append(a.catalog.SubscriptionMerchants, MerchantOffer{SubjectGovernance: product.SubjectGovernance, ID: product.EntityID, MerchantID: data.Merchant.EntityID, Name: data.Merchant.Name, Initials: string(name[:1]), Family: "未分类", Plan: product.Name, Price: product.Price, Currency: product.Currency, Stock: product.Stock, StockQuantity: product.StockQuantity, Delivery: product.Delivery.Label, Type: "商家商品声明", Description: product.Description, Aftercare: product.Aftercare, Period: product.Period, SourceURL: url, MerchantURL: data.Merchant.Website, ObservedAt: data.Merchant.ObservedAt, NativeProductID: product.NativeID, SourceName: data.Merchant.Name, Conditions: product.Conditions, Evidence: data.Evidence})
	}
	a.observeOptional(data.Merchant.ObservedAt)
	return nil
}
