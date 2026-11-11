package tokencenter

// CatalogSummary counts published records in the same admitted catalog revision.
// API sites are collected providers, not approved merchant applications. These
// numbers make no claims about unique users, service quality or sponsorship.
type CatalogSummary struct {
	PublishedBoards int `json:"publishedBoards"`
	ModelRecords    int `json:"modelRecords"`
	APISites        int `json:"apiSites"`
	APIQuotes       int `json:"apiQuotes"`
}

func summarizeCatalog(c Catalog) *CatalogSummary {
	s := &CatalogSummary{ModelRecords: len(c.Models), APISites: len(c.Sites), APIQuotes: len(c.Offers)}
	for _, board := range c.Rankings {
		if len(board.Entries) > 0 {
			s.PublishedBoards++
		}
	}
	return s
}
