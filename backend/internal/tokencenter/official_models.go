package tokencenter

import "net/url"

// A published relation, never inferred from a source name by this consumer.
type OfficialModel struct {
	EntityID          string `json:"entityId"`
	CanonicalModelID  string `json:"canonicalModelId"`
	Name              string `json:"name"`
	DeveloperEntityID string `json:"developerEntityId"`
	DeveloperID       string `json:"developerId"`
	DeveloperName     string `json:"developerName"`
	LogoURL           string `json:"logoUrl,omitempty"`
	SourceMaterialID  string `json:"sourceMaterialId"`
	SourceURL         string `json:"sourceUrl"`
	SourceContentHash string `json:"sourceContentHash"`
}

func applyOfficialModel(model *Model, ref *OfficialModel) {
	if ref == nil || ref.EntityID == "" {
		return
	}
	model.OfficialModel = ref
	model.ID, model.EntityID, model.CanonicalModelID = ref.EntityID, ref.EntityID, ref.CanonicalModelID
	model.Name, model.DisplayName = ref.Name, ref.Name
	model.DeveloperID, model.DeveloperName, model.Provider = ref.DeveloperID, ref.DeveloperName, ref.DeveloperName
	model.DeveloperLogoURL = ref.LogoURL
}

func rankedModelID(entry RankedEntry) string {
	if entry.ModelID != "" {
		return entry.ModelID
	}
	return entry.EntityID
}

func resolveCatalogLogoURLs(c *Catalog, source string) {
	base, err := url.Parse(source)
	if err != nil {
		return
	}
	resolve := func(target string) string {
		if target == "" {
			return ""
		}
		u, err := url.Parse(target)
		if err != nil {
			return ""
		}
		if u.IsAbs() {
			return target
		}
		if u.Host != "" || u.Path == "" || u.Path[0] != '/' {
			return ""
		}
		return base.ResolveReference(u).String()
	}
	for i := range c.Models {
		m := &c.Models[i]
		m.DeveloperLogoURL = resolve(m.DeveloperLogoURL)
		if m.OfficialModel != nil {
			m.OfficialModel.LogoURL = resolve(m.OfficialModel.LogoURL)
		}
	}
	for i := range c.Sites {
		c.Sites[i].LogoURL = resolve(c.Sites[i].LogoURL)
	}
}
