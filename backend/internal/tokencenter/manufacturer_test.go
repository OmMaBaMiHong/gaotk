package tokencenter

import "testing"

func TestUnmappedRelayAliasDoesNotUseRelayAsModelManufacturer(t *testing.T) {
	id, name, display := publishedModelDeveloper(chaosProvider{Key: "relay", Name: "Relay site"}, chaosSourceModel{Name: "unmapped-model"})
	if id != "" || name != "" || display != "" {
		t.Fatalf("relay identity promoted to model manufacturer: %q %q %q", id, name, display)
	}
}
