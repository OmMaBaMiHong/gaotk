package tokencenter

import (
	"net/url"
	"strings"
)

func governedLogoPath(value string) bool {
	u, err := url.Parse(value)
	return err == nil && !u.IsAbs() && u.Host == "" && strings.HasPrefix(u.Path, "/public/v1/products/") && strings.HasSuffix(u.Path, "/logo")
}

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

// applyManufacturerRegistryLogos makes the registered manufacturer brand asset
// the product-wide default developer mark. A model row's developer logo is the
// same fact as the manufacturer's registry logo, so every model published under
// a registered developer (providerKey) reuses that governed asset instead of
// keeping a source-specific directory icon or no logo at all. Models without a
// registered developer keep whatever the sources published. Run before
// resolveCatalogLogoURLs so governed relative paths still resolve.
func applyManufacturerRegistryLogos(c *Catalog) {
	registry := map[string]string{}
	for _, site := range c.Sites {
		if site.ProviderKind == "manufacturer" && site.ProviderKey != "" && governedLogoPath(site.LogoURL) {
			registry[site.ProviderKey] = site.LogoURL
		}
	}
	if len(registry) == 0 {
		return
	}
	for i := range c.Models {
		if logo, ok := registry[c.Models[i].DeveloperID]; ok {
			c.Models[i].DeveloperLogoURL = logo
		}
	}
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
