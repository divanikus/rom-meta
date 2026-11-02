package catalogapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportMediaInventoryExpiresWithoutCatalogRevision(t *testing.T) {
	s, q, game := exportFixture(t)
	token := strings.Repeat("s", 32)
	selected := func() string {
		t.Helper()
		response := lookupRequest(s, "POST", "/v1/export", token, q)
		var result ExportResponse
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatal(response.Code, response.Body.String())
		}
		return result.Plan.Records[0].Media["cover"].SourcePath
	}
	before := selected()
	resolver, revision := s.exportResolver, s.version
	name := filepath.Join(game, "media", "box-front.local.png")
	// Ordinary external filesystem edits do not publish a RomMeta revision.
	if err := os.WriteFile(name, []byte("new local artwork"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := selected(); !strings.HasSuffix(got, "box-front.local.png") {
		t.Fatal("new media was hidden", got)
	}
	if s.exportResolver != resolver || s.version != revision {
		t.Fatal("test did not reuse the resolver")
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if got := selected(); got != before {
		t.Fatal("removed media did not fall back", got, before)
	}
}
