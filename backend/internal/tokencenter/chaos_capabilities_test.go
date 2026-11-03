package tokencenter

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestChaosModelCapabilitiesPreserveTrueFalseAndUnknown(t *testing.T) {
	var accumulator chaosAccumulator
	pageJSON, err := json.Marshal(chaosFixturePage(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeChaosPage(pageJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err = accumulator.initialize(page); err != nil {
		t.Fatal(err)
	}
	err = accumulator.appendProvider(json.RawMessage(`{"schemaVersion":1,"provider":{"entityId":"10","name":"Source","sourceAuthority":"first_party","observedAt":"2026-11-03T01:00:00Z"},"models":[{"entityId":"20","key":"provider:source:declared","id":"declared","name":"Declared","capabilities":{"reasoning":true,"toolCall":false},"cost":{}},{"entityId":"21","key":"provider:source:unknown","id":"unknown","name":"Unknown","cost":{}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := accumulator.finish()
	if err != nil {
		t.Fatal(err)
	}
	capabilities := catalog.Models[0].Capabilities
	if capabilities == nil || capabilities.Reasoning == nil || !*capabilities.Reasoning || capabilities.ToolCall == nil || *capabilities.ToolCall || capabilities.StructuredOutput != nil || catalog.Models[1].Capabilities != nil || catalog.Models[0].SourceAuthority != "first_party" {
		t.Fatalf("source true/false/unknown or authority changed: %+v", catalog.Models)
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := DecodeStored(bytes.NewReader(encoded))
	if err != nil || stored.Models[0].Capabilities == nil || stored.Models[0].Capabilities.ToolCall == nil || *stored.Models[0].Capabilities.ToolCall || stored.Models[1].Capabilities != nil {
		t.Fatalf("stored capabilities round trip changed declarations: %+v %v", stored.Models, err)
	}
}
