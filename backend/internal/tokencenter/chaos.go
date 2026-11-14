package tokencenter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type chaosPage struct {
	Admission           *AdmissionContract `json:"admission"`
	SchemaVersion       int                `json:"schemaVersion"`
	Product             string             `json:"product"`
	Version             string             `json:"version"`
	PublicationRevision string             `json:"publicationRevision"`
	Config              json.RawMessage    `json:"config"`
	Domains             []chaosDomain      `json:"domains"`
	NextCursor          *string            `json:"nextCursor"`
}

type chaosDomain struct {
	Key          string                                `json:"key"`
	Materials    *chaosMaterials                       `json:"materials"`
	Rankings     map[string]json.RawMessage            `json:"rankings"`
	RankingViews map[string]map[string]json.RawMessage `json:"rankingViews"`
}

type chaosMaterials struct {
	SchemaVersion int               `json:"schemaVersion"`
	Items         []json.RawMessage `json:"items"`
	Total         *int              `json:"total"`
}

type chaosPortal struct {
	Product string `json:"product"`
	Domains []struct {
		Key       string `json:"key"`
		HomeTopic string `json:"homeTopic"`
		Enabled   bool   `json:"enabled"`
		Topics    []struct {
			Key  string `json:"key"`
			Unit string `json:"unit"`
		} `json:"topics"`
	} `json:"domains"`
	Delivery struct {
		Subscriptions []struct {
			DomainKey string `json:"domainKey"`
			Enabled   bool   `json:"enabled"`
		} `json:"subscriptions"`
	} `json:"delivery"`
}

type chaosBoard struct {
	ranking       Ranking
	signature     []byte
	total         *int
	excludedTotal *int
	seen          map[string]bool
}

type chaosAccumulator struct {
	requireAdmission bool
	admissionJSON    []byte
	catalog          Catalog
	config           []byte
	topics           map[string]map[string]string
	models           map[string]int
	sites            map[string]int
	offers           map[string]bool
	boards           map[string]*chaosBoard
	materialIDs      map[string]bool
	materialTotal    map[string]*int
	materialCount    map[string]int
}

// Increment when the same source revision needs a new consumer projection.
const chaosProjectionVersion = 6

func (s *Service) syncChaos(ctx context.Context, previous Snapshot) (Catalog, error) {
	var result chaosAccumulator
	result.requireAdmission = s.cfg.RequireAdmission
	var etag, modified string
	seen := map[string]bool{}
	cursor := ""
	for {
		target, err := url.Parse(s.cfg.SourceURL)
		if err != nil {
			return Catalog{}, errors.New("invalid configured Chaos snapshot source")
		}
		if cursor != "" {
			q := target.Query()
			q.Set("cursor", cursor)
			target.RawQuery = q.Encode()
		}
		var validators *Snapshot
		if cursor == "" && previous.AccessStatus == 0 && (!s.cfg.RequireAdmission || qualifiedContract(previous.Catalog.Admission)) && previous.SourceURL == s.cfg.SourceURL && previous.Catalog.RankingAuthority == "chaos" &&
			previous.Catalog.SourceSnapshot != nil && previous.Catalog.SourceSnapshot.ProjectionVersion == chaosProjectionVersion {
			validators = &previous
		}
		response, err := s.sourceRequest(ctx, target.String(), validators)
		if err != nil {
			return Catalog{}, err
		}
		if response.StatusCode == http.StatusNotModified {
			response.Body.Close()
			if validators == nil || previous.ETag == "" && previous.LastModified == "" {
				return Catalog{}, errors.New("Chaos source returned 304 without a successful matching snapshot")
			}
			return previous.Catalog, nil
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			if s.cfg.RequireAdmission && (response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusAccepted) {
				return Catalog{}, s.denySource(ctx, 409)
			}
			if response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 404 || response.StatusCode == 410 {
				return Catalog{}, s.denySource(ctx, response.StatusCode)
			}
			return Catalog{}, fmt.Errorf("Chaos snapshot source returned HTTP %d", response.StatusCode)
		}
		// The public owner controls page size and cursor progress. There is no
		// all-pages/item cap that could silently truncate the published catalog.
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return Catalog{}, err
		}
		page, err := decodeChaosPage(data)
		if err != nil {
			return Catalog{}, err
		}
		if cursor != "" && s.cfg.RequireAdmission {
			contractJSON, _ := json.Marshal(page.Admission)
			if !bytes.Equal(contractJSON, result.admissionJSON) {
				return Catalog{}, s.denySource(ctx, 409)
			}
		}
		if cursor == "" {
			if s.cfg.RequireAdmission {
				if !qualifiedContract(page.Admission) {
					return Catalog{}, s.denySource(ctx, 409)
				}
				if previous.Catalog.Admission != nil && previous.Catalog.Admission.Version != page.Admission.Version {
					if err = s.store.SetAccessStatus(ctx, s.cfg.SourceURL, 409); err != nil {
						return Catalog{}, err
					}
				}
			}
			etag, modified = response.Header.Get("ETag"), response.Header.Get("Last-Modified")
			parts := strings.Split(strings.Trim(target.Path, "/"), "/")
			if len(parts) >= 5 && parts[len(parts)-5] == "public" && parts[len(parts)-4] == "v1" && parts[len(parts)-3] == "products" && parts[len(parts)-1] == "snapshot" && parts[len(parts)-2] != page.Product {
				return Catalog{}, errors.New("Chaos snapshot product does not match its configured endpoint")
			}
			if err = result.initialize(page); err != nil {
				return Catalog{}, err
			}
		} else if err = result.pin(page); err != nil {
			return Catalog{}, err
		}
		if err = result.append(page); err != nil {
			return Catalog{}, err
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = *page.NextCursor
		if seen[cursor] {
			return Catalog{}, errors.New("Chaos snapshot repeated a pagination cursor")
		}
		seen[cursor] = true
	}
	catalog, err := result.finish()
	if err != nil {
		return Catalog{}, err
	}
	resolveCatalogLogoURLs(&catalog, s.cfg.SourceURL)
	if err = s.store.Save(ctx, Snapshot{Catalog: catalog, SourceURL: s.cfg.SourceURL, ETag: etag, LastModified: modified}, false); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func decodeChaosPage(data []byte) (chaosPage, error) {
	var page chaosPage
	if err := json.Unmarshal(data, &page); err != nil {
		return page, fmt.Errorf("invalid Chaos snapshot JSON: %w", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return page, err
	}
	if page.SchemaVersion != 1 || page.Product == "" || page.Version == "" || page.PublicationRevision == "" || len(page.Config) == 0 || bytes.Equal(page.Config, []byte("null")) || page.Domains == nil || root["nextCursor"] == nil {
		return page, errors.New("Chaos snapshot schema or publication metadata is missing/unsupported")
	}
	return page, nil
}

func canonicalValue(data json.RawMessage) ([]byte, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func (a *chaosAccumulator) initialize(page chaosPage) error {
	parts := strings.SplitN(page.Version, ":", 2)
	if len(parts) != 2 || parts[0] != page.PublicationRevision || parts[1] == "" {
		return errors.New("Chaos snapshot publication/config revision is invalid")
	}
	var portal chaosPortal
	if err := json.Unmarshal(page.Config, &portal); err != nil || portal.Product != page.Product || portal.Domains == nil {
		return errors.New("Chaos snapshot product configuration is invalid")
	}
	config, err := canonicalValue(page.Config)
	if err != nil {
		return err
	}
	a.config = config
	a.admissionJSON, _ = json.Marshal(page.Admission)
	a.catalog = Catalog{Version: page.Version, Source: "Chaos", RankingAuthority: "chaos", Models: []Model{}, Sites: []Site{}, Offers: []Offer{}, Rankings: map[string]Ranking{}, SourceSnapshot: &SourceSnapshot{ProjectionVersion: chaosProjectionVersion, Product: page.Product, Version: page.Version, PublicationRevision: page.PublicationRevision, ConfigRevision: parts[1], Config: page.Config}}
	a.models, a.sites, a.offers = map[string]int{}, map[string]int{}, map[string]bool{}
	a.catalog.Admission = page.Admission
	a.boards, a.topics = map[string]*chaosBoard{}, map[string]map[string]string{}
	a.materialIDs, a.materialTotal, a.materialCount = map[string]bool{}, map[string]*int{}, map[string]int{}
	active := map[string]bool{}
	for _, sub := range portal.Delivery.Subscriptions {
		if sub.Enabled {
			active[sub.DomainKey] = true
		}
	}
	for _, domain := range portal.Domains {
		if !domain.Enabled || !active[domain.Key] {
			continue
		}
		if domain.Key == "models" {
			a.catalog.DefaultRanking = domain.HomeTopic
		}
		a.topics[domain.Key] = map[string]string{}
		for _, topic := range domain.Topics {
			a.topics[domain.Key][topic.Key] = topic.Unit
		}
	}
	return nil
}

func (a *chaosAccumulator) pin(page chaosPage) error {
	snapshot := a.catalog.SourceSnapshot
	config, err := canonicalValue(page.Config)
	if err != nil || page.Product != snapshot.Product || page.Version != snapshot.Version || page.PublicationRevision != snapshot.PublicationRevision || !bytes.Equal(config, a.config) {
		return errors.New("Chaos publication or product configuration changed during pagination")
	}
	admissionJSON, _ := json.Marshal(page.Admission)
	if !bytes.Equal(admissionJSON, a.admissionJSON) {
		return errors.New("Chaos data admission contract changed during pagination")
	}
	return nil
}

func (a *chaosAccumulator) append(page chaosPage) error {
	for _, domain := range page.Domains {
		topics, exists := a.topics[domain.Key]
		if !exists {
			return errors.New("Chaos snapshot includes an unpublished domain")
		}
		if domain.Materials != nil {
			m := domain.Materials
			if m.SchemaVersion != 6 || m.Items == nil {
				return errors.New("Chaos material schema is missing/unsupported")
			}
			if previous, exists := a.materialTotal[domain.Key]; exists && !sameOptionalInt(previous, m.Total) {
				return errors.New("Chaos material total changed during pagination")
			}
			a.materialTotal[domain.Key] = m.Total
			for _, raw := range m.Items {
				if a.requireAdmission {
					if err := validateAdmittedMaterial(raw, a.catalog.Admission); err != nil {
						return err
					}
				}
				var material struct {
					ID                    string          `json:"id"`
					ContentHash           string          `json:"contentHash"`
					ModelCatalog          json.RawMessage `json:"modelCatalog"`
					ModelBenchmark        json.RawMessage `json:"modelBenchmark"`
					OfficialSubscription  json.RawMessage `json:"officialSubscription"`
					RegionalSubscriptions json.RawMessage `json:"regionalSubscriptions"`
					MerchantCatalog       json.RawMessage `json:"merchantCatalog"`
					ResourceOffer         json.RawMessage `json:"resourceOffer"`
				}
				if err := json.Unmarshal(raw, &material); err != nil || material.ID == "" {
					return errors.New("Chaos material identity is missing/invalid")
				}
				key := domain.Key + "\x00" + material.ID
				if a.materialIDs[key] {
					return errors.New("Chaos material appeared on more than one snapshot page")
				}
				a.materialIDs[key] = true
				a.materialCount[domain.Key]++
				if len(material.ResourceOffer) > 0 && !bytes.Equal(material.ResourceOffer, []byte("null")) {
					if err := a.appendResource(material.ResourceOffer); err != nil {
						return err
					}
				}
				if len(material.MerchantCatalog) > 0 && !bytes.Equal(material.MerchantCatalog, []byte("null")) {
					if err := a.appendMerchant(material.MerchantCatalog); err != nil {
						return err
					}
				}
				if len(material.ModelCatalog) > 0 && !bytes.Equal(material.ModelCatalog, []byte("null")) {
					if err := a.appendProviderMaterial(material.ID, material.ModelCatalog, material.ContentHash); err != nil {
						return err
					}
				}
				if len(material.ModelBenchmark) > 0 && !bytes.Equal(material.ModelBenchmark, []byte("null")) {
					if err := a.appendEvaluation(material.ModelBenchmark); err != nil {
						return err
					}
				}
				if len(material.OfficialSubscription) > 0 && !bytes.Equal(material.OfficialSubscription, []byte("null")) {
					if err := a.appendSubscription(material.OfficialSubscription); err != nil {
						return err
					}
				}
				if len(material.RegionalSubscriptions) > 0 && !bytes.Equal(material.RegionalSubscriptions, []byte("null")) {
					if err := a.appendRegionalSubscriptions(material.RegionalSubscriptions); err != nil {
						return err
					}
				}
			}
		}
		for topic, raw := range domain.Rankings {
			unit, exists := topics[topic]
			if !exists {
				return errors.New("Chaos snapshot ranking topic is not published")
			}
			if err := a.appendRanking(domain.Key, topic, "", unit, raw); err != nil {
				return err
			}
		}
		for topic, views := range domain.RankingViews {
			unit, exists := topics[topic]
			if !exists {
				return errors.New("Chaos snapshot ranking view topic is not published")
			}
			for view, raw := range views {
				if err := a.appendRanking(domain.Key, topic, view, unit, raw); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func sameOptionalInt(a, b *int) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func (a *chaosAccumulator) appendRanking(domain, topic, view, configuredUnit string, raw json.RawMessage) error {
	var body struct {
		SchemaVersion int `json:"schemaVersion"`
		List          struct {
			Title   string                       `json:"title"`
			Slug    string                       `json:"slug"`
			Unit    string                       `json:"unit"`
			Columns []struct{ Key, Unit string } `json:"columns"`
		} `json:"list"`
		Snapshot   *struct{ ID, GeneratedAt string } `json:"snapshot"`
		ComputedAt string                            `json:"computedAt"`
		Source     string                            `json:"source"`
		Unit       string                            `json:"unit"`
		Total      *int                              `json:"total"`
		Admission  *struct {
			SchemaVersion   int    `json:"schemaVersion"`
			ContractVersion string `json:"contractVersion"`
			Eligible        int    `json:"eligible"`
		} `json:"admission"`
		Items         []json.RawMessage `json:"items"`
		Excluded      []json.RawMessage `json:"excluded"`
		ExcludedCount *int              `json:"excludedCount"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("invalid Chaos ranking: %w", err)
	}
	if body.SchemaVersion != 1 && body.SchemaVersion != 2 || body.List.Title == "" || body.List.Slug == "" || body.Source == "" || body.Items == nil {
		return errors.New("Chaos ranking schema is missing/unsupported")
	}
	if a.requireAdmission && (body.Admission == nil || body.Admission.SchemaVersion != 1 || body.Admission.ContractVersion != a.catalog.Admission.Version || body.Total == nil || body.Admission.Eligible != *body.Total) {
		return errors.New("Chaos ranking has no current admission qualification")
	}
	unit := body.Unit
	if unit == "" {
		unit = body.List.Unit
	}
	if unit == "" {
		for _, column := range body.List.Columns {
			if column.Key == "score" {
				unit = column.Unit
				break
			}
		}
	}
	if unit == "" {
		unit = configuredUnit
	}
	version, asOf := "", body.ComputedAt
	if body.Snapshot != nil {
		version = body.Snapshot.ID
		if asOf == "" {
			asOf = body.Snapshot.GeneratedAt
		}
	}
	if version == "" {
		version = body.ComputedAt
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return err
	}
	delete(metadata, "items")
	delete(metadata, "excluded")
	// The public payload has publication metadata inside metadata. Expose its
	// fields at the consumer metadata level while retaining the source envelope.
	var publishedMetadata map[string]json.RawMessage
	if err := json.Unmarshal(metadata["metadata"], &publishedMetadata); err == nil {
		for key, value := range publishedMetadata {
			if _, exists := metadata[key]; !exists {
				metadata[key] = value
			}
		}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	signature, err := json.Marshal([]any{body.SchemaVersion, body.List, version, asOf, body.Source, unit, body.Total, body.ExcludedCount})
	if err != nil {
		return err
	}
	key := topic
	if view != "" {
		key += "/" + view
	}
	board := a.boards[key]
	if board == nil {
		board = &chaosBoard{ranking: Ranking{Domain: domain, Topic: topic, View: view, Title: body.List.Title, Slug: body.List.Slug, Version: version, AsOf: asOf, Source: body.Source, Unit: unit, Entries: []RankedEntry{}, Excluded: []RankedEntry{}, Metadata: metadataJSON}, total: body.Total, excludedTotal: body.ExcludedCount, signature: signature, seen: map[string]bool{}}
		a.boards[key] = board
	} else if board.ranking.Domain != domain || !bytes.Equal(board.signature, signature) {
		return errors.New("Chaos ranking publication changed during pagination")
	}
	appendEntries := func(items []json.RawMessage, entries *[]RankedEntry) error {
		for _, item := range items {
			var entry RankedEntry
			if err := json.Unmarshal(item, &entry); err != nil || entry.EntityID == "" || !finiteScore(entry.Score, true) {
				return errors.New("Chaos ranking entry is missing/invalid")
			}
			if board.seen[entry.EntityID] {
				return errors.New("Chaos ranking entry repeated during pagination")
			}
			board.seen[entry.EntityID] = true
			entry.Metadata = append(json.RawMessage(nil), item...)
			*entries = append(*entries, entry)
		}
		return nil
	}
	if err := appendEntries(body.Items, &board.ranking.Entries); err != nil {
		return err
	}
	if err := appendEntries(body.Excluded, &board.ranking.Excluded); err != nil {
		return err
	}
	a.observe(asOf)
	return nil
}

type chaosProvider struct {
	SubjectGovernance
	EntityID, Key, Kind, Name, DocumentationURL, APIURL, SourceURL, SourceName, SourceAuthority string
	LogoURL, LogoSourceURL                                                                      string
	WebsiteURL, PricingURL, PurchaseURL                                                         string
	ObservedAt                                                                                  *string  `json:"observedAt"`
	Tags                                                                                        []string `json:"tags"`
}

type chaosSourceModel struct {
	OfficialModel *OfficialModel `json:"officialModel,omitempty"`
	SubjectGovernance
	EntityID, Key, ID, SourceKey, CanonicalModelID, Name, Description, Family, ReleaseDate, DeveloperID, DeveloperName, Status, LastUpdated string
	DisplayName, DeveloperLogoURL, LogoURL, ConfigurationKey, WebsiteURL, PricingURL, PurchaseURL                                           string
	Billing                                                                                                                                 json.RawMessage    `json:"billing"`
	SourceMetadata                                                                                                                          json.RawMessage    `json:"sourceMetadata"`
	MaxInputTokens                                                                                                                          *float64           `json:"maxInputTokens"`
	MaxOutputTokens                                                                                                                         *float64           `json:"maxOutputTokens"`
	Modalities                                                                                                                              *ModelModalities   `json:"modalities"`
	Capabilities                                                                                                                            *ModelCapabilities `json:"capabilities,omitempty"`
	ContextWindow                                                                                                                           *float64           `json:"contextWindow"`
	Benchmarks                                                                                                                              *ModelBenchmarks   `json:"benchmarks"`
	Cost                                                                                                                                    struct {
		Input, Output, CacheRead, CacheWrite *float64
		Tiers                                []struct {
			Input, Output, CacheRead, CacheWrite *float64
			Condition                            json.RawMessage `json:"condition"`
			SourceMetadata                       json.RawMessage `json:"sourceMetadata"`
		} `json:"tiers"`
	} `json:"cost"`
}

func (a *chaosAccumulator) appendProvider(raw json.RawMessage) error {
	return a.appendProviderMaterial("", raw, "")
}

func (a *chaosAccumulator) appendProviderMaterial(sourceMaterialID string, raw json.RawMessage, sourceContentHash string) error {
	var data struct {
		SchemaVersion int                `json:"schemaVersion"`
		Provider      chaosProvider      `json:"provider"`
		Models        []chaosSourceModel `json:"models"`
	}
	if err := json.Unmarshal(raw, &data); err != nil || data.SchemaVersion != 1 || data.Provider.EntityID == "" || data.Provider.Name == "" || data.Models == nil {
		return errors.New("Chaos model catalog schema is missing/unsupported")
	}
	p := data.Provider
	if p.ObservedAt != nil && !timestamp(*p.ObservedAt) {
		return errors.New("Chaos provider observation timestamp is invalid")
	}
	website := p.WebsiteURL
	if website == "" {
		website = p.DocumentationURL
	}
	if website == "" {
		website = p.APIURL
	}
	site := Site{ID: p.EntityID, EntityID: p.EntityID, ProviderKey: p.Key, ProviderKind: p.Kind, Name: p.Name, Website: website, Models: len(data.Models), Tags: append([]string{}, p.Tags...), SourceURL: p.SourceURL, SourceName: p.SourceName, SourceAuthority: p.SourceAuthority, ObservedAt: p.ObservedAt}
	site.WebsiteURL, site.PricingURL, site.PurchaseURL = p.WebsiteURL, p.PricingURL, p.PurchaseURL
	if webURL(p.LogoURL) {
		site.LogoURL, site.LogoSourceURL = p.LogoURL, p.LogoSourceURL
	}
	site.SubjectGovernance = p.SubjectGovernance
	if index, exists := a.sites[site.ID]; !exists {
		a.sites[site.ID] = len(a.catalog.Sites)
		a.catalog.Sites = append(a.catalog.Sites, site)
	} else if preferSource(a.catalog.Sites[index].SourceAuthority, site.SourceAuthority) {
		a.catalog.Sites[index] = site
	}
	a.observeOptional(p.ObservedAt)
	for _, source := range data.Models {
		if source.EntityID == "" || source.ID == "" || source.Key == "" || source.Name == "" {
			return errors.New("Chaos source model identity is missing")
		}
		developerID, developerName, developer := publishedModelDeveloper(source)
		family := source.Family
		if family == "" {
			family = developerID
		}
		model := Model{ID: source.EntityID, EntityID: source.EntityID, CanonicalModelID: source.CanonicalModelID, SourceModelID: source.ID, SourceKey: source.SourceKey, DeveloperID: developerID, DeveloperName: developerName, Name: source.Name, Provider: developer, Family: family, Description: source.Description, Context: source.ContextWindow, MaxInputTokens: source.MaxInputTokens, MaxOutputTokens: source.MaxOutputTokens, Modalities: source.Modalities, Status: source.Status, LastUpdated: nonempty(source.LastUpdated), Input: source.Cost.Input, Output: source.Cost.Output, Tags: []string{}, Released: nonempty(source.ReleaseDate), SourceURL: p.SourceURL, SourceName: p.SourceName, SourceAuthority: p.SourceAuthority, ObservedAt: p.ObservedAt, Benchmarks: source.Benchmarks}
		model.Billing = source.Billing
		model.DisplayName = source.DisplayName
		if webURL(source.DeveloperLogoURL) {
			model.DeveloperLogoURL = source.DeveloperLogoURL
		}
		if webURL(source.LogoURL) {
			model.LogoURL = source.LogoURL
		}
		model.SubjectGovernance = source.SubjectGovernance
		model.Capabilities = source.Capabilities
		applyOfficialModel(&model, source.OfficialModel)
		if index, exists := a.models[model.ID]; !exists {
			a.models[model.ID] = len(a.catalog.Models)
			a.catalog.Models = append(a.catalog.Models, model)
		} else if preferSource(a.catalog.Models[index].SourceAuthority, model.SourceAuthority) || a.catalog.Models[index].SourceModelID == "" {
			model.Evaluations = a.catalog.Models[index].Evaluations
			model.ModelPermaslug = a.catalog.Models[index].ModelPermaslug
			if evaluation := a.catalog.Models[index].ModelDial; evaluation != nil {
				applyPublishedModelDial(&model, evaluation)
			}
			a.catalog.Models[index] = model
		}
		a.models[source.EntityID] = a.models[model.ID]
		base := Offer{ModelID: model.ID, SiteID: site.ID, SourceModelID: source.ID, Input: source.Cost.Input, Output: source.Cost.Output, CacheRead: source.Cost.CacheRead, CacheWrite: source.Cost.CacheWrite, Currency: "USD", Unit: "1M tokens", UpdatedAt: p.ObservedAt, Source: p.SourceName, SourceURL: p.SourceURL, SourceName: p.SourceName, SourceAuthority: p.SourceAuthority}
		base.Billing = source.Billing
		base.SourceMetadata, base.SourceContentHash = source.SourceMetadata, sourceContentHash
		base.WebsiteURL, base.PricingURL, base.PurchaseURL = p.WebsiteURL, p.PricingURL, p.PurchaseURL
		if source.WebsiteURL != "" {
			base.WebsiteURL = source.WebsiteURL
		}
		if source.PricingURL != "" {
			base.PricingURL = source.PricingURL
		}
		if source.PurchaseURL != "" {
			base.PurchaseURL = source.PurchaseURL
		}
		base.ReferenceIdentity = &ReferenceOfferIdentity{SourceMaterialID: sourceMaterialID, ProviderKey: p.Key, SourceModelID: source.ID, SourceKey: source.SourceKey, CanonicalModelID: source.CanonicalModelID, ConfigurationKey: source.ConfigurationKey, SourceURL: p.SourceURL}
		base.SubjectGovernance = SubjectGovernance{Admission: source.Admission}
		if err := a.appendOffer(base, p, source, "base"); err != nil {
			return err
		}
		for i, tier := range source.Cost.Tiers {
			offer := base
			offer.Input, offer.Output, offer.CacheRead, offer.CacheWrite = tier.Input, tier.Output, tier.CacheRead, tier.CacheWrite
			offer.Condition, offer.SourceMetadata = tier.Condition, tier.SourceMetadata
			if err := a.appendOffer(offer, p, source, fmt.Sprintf("tier:%d", i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *chaosAccumulator) appendOffer(offer Offer, p chaosProvider, model chaosSourceModel, plan string) error {
	identity, _ := json.Marshal([]string{p.EntityID, p.SourceURL, model.Key, model.ID, model.SourceKey, plan})
	digest := sha256.Sum256(identity)
	offer.ID = "chaos-offer:" + hex.EncodeToString(digest[:])
	if offer.ReferenceIdentity != nil {
		identity := *offer.ReferenceIdentity
		identity.QuotePlanKey = plan
		offer.ReferenceIdentity = &identity
	}
	if a.offers[offer.ID] {
		return errors.New("Chaos catalog repeats the same source offering")
	}
	a.offers[offer.ID] = true
	a.catalog.Offers = append(a.catalog.Offers, offer)
	return nil
}

func preferSource(current, incoming string) bool {
	return current != "first_party" && incoming == "first_party"
}

func publishedModelDeveloper(model chaosSourceModel) (id, name, display string) {
	id, name = model.DeveloperID, model.DeveloperName
	display = name
	if display == "" {
		display = id
	}
	return id, name, display
}
func nonempty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (a *chaosAccumulator) appendEvaluation(raw json.RawMessage) error {
	var data struct {
		SchemaVersion int `json:"schemaVersion"`
		Evaluation
	}
	if err := json.Unmarshal(raw, &data); err != nil || data.SchemaVersion != 1 || data.EntityID == "" || data.Source == "" || data.BenchmarkID == "" || data.ModelPermaslug == "" || data.ProviderName == "" && data.BenchmarkID != "modeldial_five_axis" || data.Score == nil || !finiteScore(data.Score, true) {
		return errors.New("Chaos model benchmark schema is missing/unsupported")
	}
	evaluation := data.Evaluation
	if evaluation.ObservedAt != nil && !timestamp(*evaluation.ObservedAt) || evaluation.MeasuredAt != nil && !timestamp(*evaluation.MeasuredAt) {
		return errors.New("Chaos model benchmark timestamp is invalid")
	}
	modelID := evaluation.EntityID
	if evaluation.OfficialModel != nil {
		modelID = evaluation.OfficialModel.EntityID
	}
	index, exists := a.models[modelID]
	if !exists {
		if evaluation.ModelName == "" {
			return errors.New("Chaos benchmark-only model has no source name")
		}
		index = len(a.catalog.Models)
		a.models[modelID] = index
		model := Model{ID: evaluation.EntityID, EntityID: evaluation.EntityID, CanonicalModelID: evaluation.CanonicalModelID, ModelPermaslug: evaluation.ModelPermaslug, Name: evaluation.ModelName, Tags: []string{}, SourceURL: evaluation.SourceURL, SourceName: evaluation.Source, ObservedAt: evaluation.ObservedAt}
		applyPublishedDeveloper(&model, evaluation.Developer)
		applyOfficialModel(&model, evaluation.OfficialModel)
		a.catalog.Models = append(a.catalog.Models, model)
	}
	a.models[evaluation.EntityID] = index
	if evaluation.BenchmarkID == "modeldial_five_axis" {
		applyPublishedModelDial(&a.catalog.Models[index], &evaluation)
	}
	a.catalog.Models[index].Evaluations = append(a.catalog.Models[index].Evaluations, evaluation)
	if len(a.catalog.Models[index].TagRefs) == 0 {
		a.catalog.Models[index].SubjectGovernance = evaluation.SubjectGovernance
	}
	if a.catalog.Models[index].ModelPermaslug == "" {
		a.catalog.Models[index].ModelPermaslug = evaluation.ModelPermaslug
	}
	a.observeOptional(evaluation.ObservedAt)
	a.observeOptional(evaluation.MeasuredAt)
	return nil
}

func applyPublishedDeveloper(model *Model, developer *ModelDeveloper) {
	if developer == nil {
		return
	}
	model.DeveloperID, model.DeveloperName, model.Provider = publishedModelDeveloper(chaosSourceModel{DeveloperID: developer.ID, DeveloperName: developer.Name})
	model.DisplayName, model.Family = developer.ModelName, developer.ID
	if webURL(developer.LogoURL) {
		model.DeveloperLogoURL = developer.LogoURL
	}
}

func applyPublishedModelDial(model *Model, evaluation *Evaluation) {
	model.ModelDial = evaluation
	model.Released = evaluation.Released
	model.QuoteModelIDs = append([]string(nil), evaluation.QuoteModelIDs...)
	model.QuoteIdentityEvidence = evaluation.QuoteIdentityEvidence
	applyPublishedDeveloper(model, evaluation.Developer)
	applyOfficialModel(model, evaluation.OfficialModel)
}

func (a *chaosAccumulator) appendSubscription(raw json.RawMessage) error {
	var data struct {
		SchemaVersion int `json:"schemaVersion"`
		OfficialSubscription
	}
	if err := json.Unmarshal(raw, &data); err != nil || data.SchemaVersion != 1 || data.EntityID == "" || data.Vendor == "" || data.PlanID == "" || data.PlanName == "" || data.Evidence.SourceURL == "" {
		return errors.New("Chaos official subscription schema is missing/unsupported")
	}
	if !optionalNumber(data.Price) || !optionalNumber(data.AnnualTotal) || !optionalNumber(data.MonthlyEquivalent) || data.Evidence.CapturedAt != nil && !timestamp(*data.Evidence.CapturedAt) {
		return errors.New("Chaos official subscription amount or capture timestamp is invalid")
	}
	// Quoted charges, annual totals and monthly equivalents are distinct source
	// fields. No client calculation or inferred commercial plan is introduced.
	a.catalog.OfficialSubscriptions = append(a.catalog.OfficialSubscriptions, data.OfficialSubscription)
	a.observeOptional(data.Evidence.CapturedAt)
	return nil
}

func (a *chaosAccumulator) observeOptional(value *string) {
	if value != nil {
		a.observe(*value)
	}
}
func (a *chaosAccumulator) observe(value string) {
	incoming, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return
	}
	current, _ := time.Parse(time.RFC3339Nano, a.catalog.UpdatedAt)
	if incoming.After(current) {
		a.catalog.UpdatedAt = value
	}
}

func (a *chaosAccumulator) finish() (Catalog, error) {
	for domain, total := range a.materialTotal {
		if total != nil && *total != a.materialCount[domain] {
			return Catalog{}, errors.New("Chaos material pagination is incomplete")
		}
	}
	for key, board := range a.boards {
		if board.total != nil && *board.total != len(board.ranking.Entries) {
			return Catalog{}, errors.New("Chaos ranking pagination is incomplete")
		}
		if board.excludedTotal != nil && *board.excludedTotal != len(board.ranking.Excluded) {
			return Catalog{}, errors.New("Chaos excluded ranking pagination is incomplete")
		}
		for i := range board.ranking.Entries {
			entry := &board.ranking.Entries[i]
			if index, ok := a.models[entry.EntityID]; ok {
				entry.ModelID = a.catalog.Models[index].ID
			}
		}
		for i := range board.ranking.Excluded {
			entry := &board.ranking.Excluded[i]
			if index, ok := a.models[entry.EntityID]; ok {
				entry.ModelID = a.catalog.Models[index].ID
			}
		}
		a.catalog.Rankings[key] = board.ranking
	}
	// These are copies of published order/ranks, never locally computed scores.
	key := a.catalog.DefaultRanking
	if key == "" {
		key = "intelligence"
	}
	if board := a.boards[key]; board != nil {
		ordered, used := []Model{}, map[string]bool{}
		for _, entry := range board.ranking.Entries {
			index, exists := a.models[entry.EntityID]
			var model Model
			if exists {
				model = a.catalog.Models[index]
			} else {
				model = Model{ID: entry.EntityID, EntityID: entry.EntityID, Name: entry.Name, Tags: []string{}}
				if a.requireAdmission {
					return Catalog{}, errors.New("qualified model ranking has an unresolved catalog reference")
				}
			}
			model.Rank, model.Score = entry.Rank, entry.Score
			ordered = append(ordered, model)
			used[model.ID] = true
		}
		for _, model := range a.catalog.Models {
			if !used[model.ID] {
				ordered = append(ordered, model)
			}
		}
		a.catalog.Models = ordered
	}
	if board := a.boards["providers"]; board != nil {
		ordered, used := []Site{}, map[string]bool{}
		for _, entry := range board.ranking.Entries {
			index, exists := a.sites[entry.EntityID]
			var site Site
			if exists {
				site = a.catalog.Sites[index]
			} else {
				site = Site{ID: entry.EntityID, EntityID: entry.EntityID, Name: entry.Name, Tags: []string{}}
				if a.requireAdmission {
					return Catalog{}, errors.New("qualified site ranking has an unresolved catalog reference")
				}
			}
			site.Rank, site.Score = entry.Rank, entry.Score
			if board.ranking.Unit == "models" && entry.Score != nil && *entry.Score >= 0 && *entry.Score == float64(int(*entry.Score)) {
				site.Models = int(*entry.Score)
			}
			ordered = append(ordered, site)
			used[site.ID] = true
		}
		for _, site := range a.catalog.Sites {
			if !used[site.ID] {
				ordered = append(ordered, site)
			}
		}
		a.catalog.Sites = ordered
	}
	return Normalize(a.catalog)
}
