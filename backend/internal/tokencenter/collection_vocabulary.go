package tokencenter

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

//go:embed tokencenter-collection-vocabulary-v1.contract.json
var collectionVocabularyContractJSON []byte

// The embedded JSON is the Chaos consumer contract, not a second slug list.
// Declared source: OmMaBaMiHong/chaos docs/standards/tokencenter-collection-vocabulary-v1.contract.json
// at 9de086c159179dfca2eaf65099027b1176d8d61b.
const (
	collectionVocabularyContractCommit = "9de086c159179dfca2eaf65099027b1176d8d61b"
	collectionVocabularyContractFile   = "docs/standards/tokencenter-collection-vocabulary-v1.contract.json"
	collectionDictionaryTimeout        = 3 * time.Second
	collectionDictionaryMaxBytes       = 1 << 20
)

// CollectionVocabulary is the public navigation dictionary projected from the
// Chaos Tag contract. Draft rows stay in the embedded contract and out of this
// object. Numeric Tag.id is filled only from a published dictionary.
type CollectionVocabulary struct {
	Dictionary       string           `json:"dictionary"`
	Dimension        string           `json:"dimension"`
	StableIdentity   string           `json:"stableIdentity"`
	Version          string           `json:"version"`
	Revision         string           `json:"revision"`
	ProposalFile     string           `json:"proposalFile"`
	ContractFile     string           `json:"contractFile"`
	ContractCommit   string           `json:"contractCommit"`
	ApplicableKinds  []string         `json:"applicableKinds"`
	TopNav           []string         `json:"topNav"`
	BillingUnits     []string         `json:"billingUnits"`
	PurchaseSurfaces []string         `json:"purchaseSurfaces"`
	Entries          []CollectionTerm `json:"entries"`
}

// CollectionTerm is one public navigation tag. Slug is the stable identity.
// ID is omitted until Chaos publishes Tag.id for that slug.
type CollectionTerm struct {
	ID                  string   `json:"id,omitempty"`
	Slug                string   `json:"slug"`
	Name                string   `json:"name"`
	Dimension           string   `json:"dimension"`
	Revision            string   `json:"revision"`
	ParentSlug          *string  `json:"parentSlug"`
	GovernanceStatus    string   `json:"governanceStatus"`
	Surface             string   `json:"surface"`
	NavOrder            int      `json:"navOrder"`
	BoardRule           string   `json:"boardRule"`
	BillingUnit         *string  `json:"billingUnit"`
	PurchaseSurface     string   `json:"purchaseSurface"`
	MemberSlugs         []string `json:"memberSlugs"`
	ExternalFilterSlugs []string `json:"externalFilterSlugs"`
}

type collectionContract struct {
	Version          string                  `json:"version"`
	Dictionary       string                  `json:"dictionary"`
	Dimension        string                  `json:"dimension"`
	StableIdentity   string                  `json:"stableIdentity"`
	NumericID        string                  `json:"numericId"`
	ProposalFile     string                  `json:"proposalFile"`
	ApplicableKinds  []string                `json:"applicableKinds"`
	BillingUnits     []string                `json:"billingUnits"`
	PurchaseSurfaces []string                `json:"purchaseSurfaces"`
	TopNav           []string                `json:"topNav"`
	Entries          []collectionContractRow `json:"entries"`
}

type collectionContractRow struct {
	Slug                string   `json:"slug"`
	Name                string   `json:"name"`
	ParentSlug          *string  `json:"parentSlug"`
	GovernanceStatus    string   `json:"governanceStatus"`
	Surface             string   `json:"surface"`
	NavOrder            int      `json:"navOrder"`
	BoardRule           string   `json:"boardRule"`
	BillingUnit         *string  `json:"billingUnit"`
	PurchaseSurface     string   `json:"purchaseSurface"`
	MemberSlugs         []string `json:"memberSlugs"`
	ExternalFilterSlugs []string `json:"externalFilterSlugs"`
}

type publishedTag struct {
	ID       string
	Slug     string
	Revision string
}

func init() {
	if _, err := projectCollectionVocabulary(nil, ""); err != nil {
		panic("tokencenter collection vocabulary: " + err.Error())
	}
}

func projectCollectionVocabulary(published map[string]publishedTag, publishedRevision string) (*CollectionVocabulary, error) {
	return projectContractData(collectionVocabularyContractJSON, published, publishedRevision)
}

func projectContractData(data []byte, published map[string]publishedTag, publishedRevision string) (*CollectionVocabulary, error) {
	contract, err := parseCollectionContract(data)
	if err != nil {
		return nil, err
	}
	revision := contract.Version
	if publishedRevision != "" {
		revision = publishedRevision
	}
	vocab := &CollectionVocabulary{
		Dictionary:       contract.Dictionary,
		Dimension:        contract.Dimension,
		StableIdentity:   contract.StableIdentity,
		Version:          contract.Version,
		Revision:         revision,
		ProposalFile:     contract.ProposalFile,
		ContractFile:     collectionVocabularyContractFile,
		ContractCommit:   collectionVocabularyContractCommit,
		ApplicableKinds:  copyStrings(contract.ApplicableKinds),
		TopNav:           copyStrings(contract.TopNav),
		BillingUnits:     copyStrings(contract.BillingUnits),
		PurchaseSurfaces: copyStrings(contract.PurchaseSurfaces),
		Entries:          []CollectionTerm{},
	}
	for _, entry := range contract.Entries {
		if !publicGovernance(entry.GovernanceStatus) {
			continue
		}
		term := CollectionTerm{
			Slug:                entry.Slug,
			Name:                entry.Name,
			Dimension:           contract.Dimension,
			Revision:            contract.Version,
			ParentSlug:          cloneStringPtr(entry.ParentSlug),
			GovernanceStatus:    entry.GovernanceStatus,
			Surface:             entry.Surface,
			NavOrder:            entry.NavOrder,
			BoardRule:           entry.BoardRule,
			BillingUnit:         cloneStringPtr(entry.BillingUnit),
			PurchaseSurface:     entry.PurchaseSurface,
			MemberSlugs:         copyStrings(entry.MemberSlugs),
			ExternalFilterSlugs: copyStrings(entry.ExternalFilterSlugs),
		}
		if tag, ok := published[entry.Slug]; ok && tag.ID != "" {
			term.ID = tag.ID
			if tag.Revision != "" {
				term.Revision = tag.Revision
			}
		}
		vocab.Entries = append(vocab.Entries, term)
	}
	return vocab, nil
}

func parseCollectionContract(data []byte) (collectionContract, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var contract collectionContract
	if err := dec.Decode(&contract); err != nil {
		return contract, fmt.Errorf("collection vocabulary contract: %w", err)
	}
	if err := dec.Decode(new(struct{})); err != io.EOF {
		return contract, errors.New("collection vocabulary contract must be one JSON object")
	}
	if err := validateCollectionContract(contract); err != nil {
		return contract, err
	}
	return contract, nil
}

func validateCollectionContract(contract collectionContract) error {
	if contract.Dictionary != "Tag" || contract.Dimension != "navigation" || contract.StableIdentity != "slug" || contract.Version == "" || contract.ProposalFile == "" {
		return errors.New("collection vocabulary contract identity does not match the Chaos navigation tag dictionary")
	}
	if len(contract.TopNav) == 0 || len(contract.Entries) == 0 {
		return errors.New("collection vocabulary contract has no navigation entries")
	}
	bySlug := map[string]collectionContractRow{}
	for _, entry := range contract.Entries {
		if entry.Slug == "" || strings.TrimSpace(entry.Name) == "" || entry.Surface == "" || entry.BoardRule == "" {
			return fmt.Errorf("collection vocabulary entry %q is incomplete", entry.Slug)
		}
		switch entry.GovernanceStatus {
		case "active", "legacy", "draft":
		default:
			return fmt.Errorf("collection vocabulary entry %s has unknown governance", entry.Slug)
		}
		if _, exists := bySlug[entry.Slug]; exists {
			return fmt.Errorf("collection vocabulary slug %s is duplicated", entry.Slug)
		}
		bySlug[entry.Slug] = entry
	}
	units := map[string]bool{}
	for _, unit := range contract.BillingUnits {
		if unit == "" || bySlug[unit].Slug != "" {
			return fmt.Errorf("billing unit %q must stay a field, not a navigation tag", unit)
		}
		units[unit] = true
	}
	surfaces := map[string]bool{}
	for _, surface := range contract.PurchaseSurfaces {
		if surface == "" {
			return errors.New("collection vocabulary purchase surface is empty")
		}
		surfaces[surface] = true
	}
	seenNav := map[string]bool{}
	for _, slug := range contract.TopNav {
		entry, ok := bySlug[slug]
		if !ok || !publicGovernance(entry.GovernanceStatus) || entry.Surface != "top_nav" || seenNav[slug] {
			return fmt.Errorf("topNav slug %s is not a public top_nav tag", slug)
		}
		seenNav[slug] = true
	}
	for _, entry := range contract.Entries {
		if !surfaces[entry.PurchaseSurface] {
			return fmt.Errorf("collection vocabulary entry %s has unknown purchase surface", entry.Slug)
		}
		if entry.BillingUnit != nil && !units[*entry.BillingUnit] {
			return fmt.Errorf("collection vocabulary entry %s has unknown billing unit", entry.Slug)
		}
		if entry.ParentSlug != nil {
			if _, ok := bySlug[*entry.ParentSlug]; !ok {
				return fmt.Errorf("parent slug %s is not in the collection contract", *entry.ParentSlug)
			}
		}
		for _, member := range entry.MemberSlugs {
			if _, ok := bySlug[member]; !ok {
				return fmt.Errorf("member slug %s is not in the collection contract", member)
			}
		}
		for _, filter := range entry.ExternalFilterSlugs {
			if _, exists := bySlug[filter]; exists {
				return fmt.Errorf("external filter %s collides with a navigation slug", filter)
			}
		}
	}
	return nil
}

func publicGovernance(status string) bool {
	return status == "active" || status == "legacy"
}

func copyStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return append([]string{}, in...)
}

func cloneStringPtr(in *string) *string {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func collectionDictionaryURL(source string) string {
	parsed, err := url.Parse(source)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return ""
	}
	parsed.Path = "/public/v1/tags"
	parsed.RawPath = ""
	parsed.RawQuery = "dimension=navigation"
	parsed.Fragment = ""
	return parsed.String()
}

func (s *Service) present(catalog Catalog) (Catalog, error) {
	s.publishedMu.Lock()
	tags, revision := s.publishedTags, s.publishedRevision
	s.publishedMu.Unlock()
	vocab, err := projectCollectionVocabulary(tags, revision)
	if err != nil {
		return Catalog{}, err
	}
	catalog.CollectionVocabulary = vocab
	return catalog, nil
}

// refreshCollectionDictionary reads published Tag ids when a Chaos source is configured.
// Failure leaves the last accepted overlay in place and never withdraws the catalog.
func (s *Service) refreshCollectionDictionary(ctx context.Context) {
	target := s.cfg.CollectionDictionaryURL
	if target == "" {
		return
	}
	if s.cfg.SourceToken != "" && !strings.HasPrefix(target, "https://") {
		log.Printf("[TokenCenter] collection dictionary skipped: source token requires HTTPS")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, collectionDictionaryTimeout)
	defer cancel()
	response, err := s.sourceRequest(ctx, target, nil)
	if err != nil {
		log.Printf("[TokenCenter] collection dictionary request failed")
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		s.publishedMu.Lock()
		s.publishedTags, s.publishedRevision = nil, ""
		s.publishedMu.Unlock()
		log.Printf("[TokenCenter] collection dictionary HTTP %d; numeric tag ids omitted", response.StatusCode)
		return
	}
	if response.StatusCode != http.StatusOK {
		log.Printf("[TokenCenter] collection dictionary HTTP %d; previous tag ids kept", response.StatusCode)
		return
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, collectionDictionaryMaxBytes+1))
	if err != nil || len(data) > collectionDictionaryMaxBytes {
		log.Printf("[TokenCenter] collection dictionary body unreadable; previous tag ids kept")
		return
	}
	revision, tags, recognized := parsePublishedDictionary(data)
	if !recognized {
		log.Printf("[TokenCenter] collection dictionary payload is not a navigation Tag dictionary; previous tag ids kept")
		return
	}
	s.publishedMu.Lock()
	s.publishedTags, s.publishedRevision = tags, revision
	s.publishedMu.Unlock()
}

func parsePublishedDictionary(data []byte) (string, map[string]publishedTag, bool) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return "", nil, false
	}
	allowed := publicContractSlugs()
	if data[0] == '[' {
		var list []publishedTagWire
		if json.Unmarshal(data, &list) != nil {
			return "", nil, false
		}
		return "", collectPublishedTags(list, "navigation", allowed), true
	}
	var envelope struct {
		Dictionary string             `json:"dictionary"`
		Dimension  string             `json:"dimension"`
		Revision   json.RawMessage    `json:"revision"`
		TagRev     json.RawMessage    `json:"tagRevision"`
		Tags       []publishedTagWire `json:"tags"`
		Entries    []publishedTagWire `json:"entries"`
		Items      []publishedTagWire `json:"items"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return "", nil, false
	}
	if envelope.Dictionary != "Tag" && envelope.Dimension != "navigation" {
		return "", nil, false
	}
	if envelope.Dimension != "" && envelope.Dimension != "navigation" {
		return "", nil, false
	}
	parent := envelope.Dimension
	if parent == "" {
		parent = "navigation"
	}
	list := envelope.Tags
	if list == nil {
		list = envelope.Entries
	}
	if list == nil {
		list = envelope.Items
	}
	if list == nil {
		return "", nil, false
	}
	revision := cleanRevision(rawRevision(envelope.Revision))
	if revision == "" {
		revision = cleanRevision(rawRevision(envelope.TagRev))
	}
	return revision, collectPublishedTags(list, parent, allowed), true
}

func publicContractSlugs() map[string]bool {
	contract, err := parseCollectionContract(collectionVocabularyContractJSON)
	if err != nil {
		return map[string]bool{}
	}
	allowed := map[string]bool{}
	for _, entry := range contract.Entries {
		if publicGovernance(entry.GovernanceStatus) {
			allowed[entry.Slug] = true
		}
	}
	return allowed
}

type publishedTagWire struct {
	ID               json.RawMessage `json:"id"`
	TagID            json.RawMessage `json:"tagId"`
	Slug             string          `json:"slug"`
	Dimension        string          `json:"dimension"`
	Revision         json.RawMessage `json:"revision"`
	GovernanceStatus string          `json:"governanceStatus"`
	Status           string          `json:"status"`
}

func collectPublishedTags(list []publishedTagWire, parentDimension string, allowed map[string]bool) map[string]publishedTag {
	tags := map[string]publishedTag{}
	for _, item := range list {
		dimension := item.Dimension
		if dimension == "" {
			dimension = parentDimension
		}
		if item.Slug == "" || dimension != "navigation" || !allowed[item.Slug] {
			continue
		}
		governance := item.GovernanceStatus
		if governance == "" {
			governance = item.Status
		}
		if governance != "" && !publicGovernance(governance) {
			continue
		}
		identity := item.ID
		if len(bytes.TrimSpace(identity)) == 0 || bytes.Equal(bytes.TrimSpace(identity), []byte("null")) {
			identity = item.TagID
		}
		id, ok := canonicalDecimalID(identity)
		if !ok {
			continue
		}
		if _, exists := tags[item.Slug]; exists {
			continue
		}
		tags[item.Slug] = publishedTag{ID: id, Slug: item.Slug, Revision: cleanRevision(rawRevision(item.Revision))}
	}
	return tags
}

func canonicalDecimalID(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", false
	}
	var text string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &text) != nil {
			return "", false
		}
	} else {
		var number json.Number
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&number) != nil {
			return "", false
		}
		text = number.String()
	}
	if text == "" || text[0] < '1' || text[0] > '9' {
		return "", false
	}
	for _, r := range text[1:] {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != text {
		return "", false
	}
	return text, true
}

func rawRevision(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var text string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &text) != nil {
			return ""
		}
		return text
	}
	var number json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&number) != nil {
		return ""
	}
	text = number.String()
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != text {
		return ""
	}
	return text
}

func cleanRevision(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\t") {
		return ""
	}
	return value
}
