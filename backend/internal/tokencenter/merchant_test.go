package tokencenter

import (
	"encoding/json"
	"testing"
)

func TestMerchantQuotesPreserveNativeUnitUnknownStockAndExplicitZero(t *testing.T) {
	a := chaosAccumulator{catalog: Catalog{}}
	raw := json.RawMessage(`{"schemaVersion":1,"merchant":{"entityId":"1","name":"Source shop","website":"https://shop.example/shop/S1","sourceUrl":"https://shop.example/shop/S1","observedAt":"2026-11-03T00:00:00Z"},"products":[{"entityId":"2","nativeID":"G1","name":"7 day source claim","price":0,"currency":"","stock":null,"period":"","delivery":{"label":"Manual"}},{"entityId":"3","nativeID":"G2","name":"Unknown quote","price":null,"stock":false,"currency":"CNY","period":"Source stated period"}],"evidence":[{"bodySHA256":"source-hash"}]}`)
	if err := a.appendMerchant(raw); err != nil {
		t.Fatal(err)
	}
	rows := a.catalog.SubscriptionMerchants
	if len(rows) != 2 || rows[0].Price == nil || *rows[0].Price != 0 || rows[0].Currency != "" || rows[0].Stock != nil || rows[0].Period != "" || rows[1].Price != nil || rows[1].Stock == nil || *rows[1].Stock || rows[1].Period != "Source stated period" {
		t.Fatalf("merchant source facts were invented/converted: %+v", rows)
	}
	if err := a.appendMerchant(raw); err == nil {
		t.Fatal("duplicate source product was accepted")
	}
}
