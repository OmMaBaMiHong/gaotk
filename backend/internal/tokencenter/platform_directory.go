package tokencenter

import "net/url"

// This is a read projection of published platform entities, not a new identity
// registry. Multiple offers from the same entity count once. A model or an app
// product is not itself a merchant/platform. Unresolved provider identities stay
// visible as a missing-coverage count rather than being merged by display name.
type PlatformItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	LogoURL string `json:"logoUrl,omitempty"`
	Href    string `json:"href"`
}
type PlatformGroup struct {
	Key   string         `json:"key"`
	Label string         `json:"label"`
	Items []PlatformItem `json:"items"`
}
type PlatformDirectory struct {
	Total                int             `json:"total"`
	Groups               []PlatformGroup `json:"groups"`
	UnresolvedOfferCount int             `json:"unresolvedOfferCount"`
}

func projectPlatformDirectory(c Catalog) *PlatformDirectory {
	d := &PlatformDirectory{Groups: []PlatformGroup{
		{Key: "manufacturer", Label: "厂商官方", Items: []PlatformItem{}},
		{Key: "aggregator", Label: "聚合平台", Items: []PlatformItem{}},
		{Key: "relay", Label: "第三方中转站", Items: []PlatformItem{}},
		{Key: "merchant", Label: "订阅与数字服务商家", Items: []PlatformItem{}},
		{Key: "other", Label: "其他平台", Items: []PlatformItem{}},
	}}
	seen := map[string]bool{}
	add := func(kind string, p PlatformItem) {
		if p.ID == "" || seen[p.ID] {
			return
		}
		seen[p.ID] = true
		index := len(d.Groups) - 1
		for i := range d.Groups {
			if d.Groups[i].Key == kind {
				index = i
				break
			}
		}
		d.Groups[index].Items = append(d.Groups[index].Items, p)
		d.Total++
	}
	for _, site := range c.Sites {
		id := site.EntityID
		if id == "" {
			id = site.ID
		}
		add(site.ProviderKind, PlatformItem{ID: id, Name: site.Name, LogoURL: site.LogoURL, Href: "/api-transit/" + url.PathEscape(site.ID)})
	}
	for _, offer := range c.SubscriptionMerchants {
		if offer.MerchantID == "" {
			d.UnresolvedOfferCount++
			continue
		}
		href := offer.MerchantURL
		if href == "" {
			href = "/channels"
		}
		add("merchant", PlatformItem{ID: offer.MerchantID, Name: offer.Name, Href: href})
	}
	// Resource products currently carry no canonical platform ID. Do not infer
	// identity from names or source hostnames in the consumer service.
	d.UnresolvedOfferCount += len(c.Resources)
	return d
}
