package tokencenter

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
)

func TestAccountsMigrationsExcludeTokenCenterCatalog(t *testing.T) {
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		content, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "tokencenter_catalog") {
			t.Fatalf("account migrations still create the TokenCenter catalog: %s", name)
		}
	}
}
