package tokencenter

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

var ErrSourceRevoked = errors.New("catalog source access was revoked")
var ErrAdmissionPending = errors.New("catalog is awaiting the current data admission contract")

type TagReference struct {
	TagID     string `json:"tagId"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Relation  string `json:"relation"`
	Revision  int    `json:"revision"`
}
type SubjectGovernance struct {
	TagRefs   []TagReference  `json:"tagRefs,omitempty"`
	Admission json.RawMessage `json:"admission,omitempty"`
}
type AdmissionDataset struct {
	SourceID     string            `json:"sourceId"`
	TemplateKey  string            `json:"templateKey"`
	ContractHash string            `json:"contractHash"`
	Purposes     []string          `json:"purposes"`
	RequiredFor  []json.RawMessage `json:"requiredFor,omitempty"`
}
type AdmissionContract struct {
	TagRevision      string              `json:"tagRevision"`
	PlanVersion      int                 `json:"planVersion"`
	SchemaVersion    int                 `json:"schemaVersion"`
	ValidatorVersion int                 `json:"validatorVersion"`
	Version          string              `json:"version"`
	Datasets         []AdmissionDataset  `json:"datasets"`
	Rankings         map[string][]string `json:"rankings,omitempty"`
}

func (s *Service) denySource(ctx context.Context, status int) error {
	if err := s.store.SetAccessStatus(ctx, s.cfg.SourceURL, status); err != nil {
		return err
	}
	if status == 409 {
		return ErrAdmissionPending
	}
	return ErrSourceRevoked
}

func qualifiedContract(c *AdmissionContract) bool {
	if c == nil || c.SchemaVersion != 1 || c.ValidatorVersion != 1 || len(c.Version) != 64 || len(c.TagRevision) != 64 || c.PlanVersion <= 0 || len(c.Datasets) == 0 {
		return false
	}
	_, err := hex.DecodeString(c.Version)
	return err == nil
}
func validateAdmittedMaterial(raw json.RawMessage, contract *AdmissionContract) error {
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	var source string
	if err := json.Unmarshal(item["sourceId"], &source); err != nil {
		return errors.New("qualified material has no source identity")
	}
	type decision struct {
		SchemaVersion    int                                   `json:"schemaVersion"`
		ValidatorVersion int                                   `json:"validatorVersion"`
		Contracts        map[string]map[string]json.RawMessage `json:"contracts"`
	}
	check := func(meta json.RawMessage, hashes []string) bool {
		var value decision
		if json.Unmarshal(meta, &value) != nil || value.SchemaVersion != 1 || value.ValidatorVersion != 1 {
			return false
		}
		for _, hash := range hashes {
			var qualified bool
			if json.Unmarshal(value.Contracts[hash]["catalog"], &qualified) == nil && qualified {
				return true
			}
		}
		return false
	}
	candidates := []AdmissionDataset{}
	allHashes := []string{}
	for _, d := range contract.Datasets {
		if d.SourceID != source {
			continue
		}
		if len(d.ContractHash) != 64 {
			return errors.New("invalid source contract hash")
		}
		if _, err := hex.DecodeString(d.ContractHash); err != nil {
			return err
		}
		candidates = append(candidates, d)
		allHashes = append(allHashes, d.ContractHash)
	}
	if len(candidates) == 0 || !check(item["admission"], allHashes) {
		return errors.New("material has no current catalog qualification")
	}
	var visit func(json.RawMessage, []string) error
	visit = func(value json.RawMessage, hashes []string) error {
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) != nil {
			var list []json.RawMessage
			if json.Unmarshal(value, &list) == nil {
				for _, child := range list {
					if err := visit(child, hashes); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if _, present := object["entityId"]; present {
			if !check(object["admission"], hashes) {
				return errors.New("nested subject is not qualified for the current contract")
			}
			var refs []TagReference
			if json.Unmarshal(object["tagRefs"], &refs) != nil {
				return errors.New("nested subject has no canonical tag references")
			}
			classified := false
			for _, ref := range refs {
				id, err := strconv.ParseInt(ref.TagID, 10, 64)
				if err != nil || id <= 0 || ref.Slug == "" || ref.Name == "" || ref.Revision <= 0 {
					return errors.New("invalid canonical tag reference")
				}
				if ref.Dimension == "subject_type" && ref.Relation == "classified_as" && ref.Slug != "subject-collected-record" {
					classified = true
				}
			}
			if !classified {
				return errors.New("generic record tag does not classify a precise subject")
			}
		}
		for _, key := range []string{"provider", "models", "merchant", "products"} {
			if child, ok := object[key]; ok {
				if err := visit(child, hashes); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for key, expected := range map[string]string{"resourceOffer": "resource-catalog", "modelCatalog": "model-catalog", "modelBenchmark": "model-benchmark", "officialSubscription": "official-subscription", "merchantCatalog": "merchant-catalog"} {
		if value, ok := item[key]; ok && string(value) != "null" {
			hashes := []string{}
			for _, d := range candidates {
				if d.TemplateKey == expected {
					hashes = append(hashes, d.ContractHash)
				}
			}
			if len(hashes) == 0 {
				return fmt.Errorf("%s is not covered by a published data template", key)
			}
			if err := visit(value, hashes); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	return nil
}
