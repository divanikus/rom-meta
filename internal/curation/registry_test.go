package curation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedSourcesStableIdentityAndRevision(t *testing.T) {
	root := t.TempDir()
	r, e := Load(root)
	if e != nil {
		t.Fatal(e)
	}
	first := r.Revision
	r, a, e := Save(root, r.Revision, "", "Русский вариант")
	if e != nil {
		t.Fatal(e)
	}
	r, b, e := Save(root, r.Revision, "", "Short descriptions")
	if e != nil {
		t.Fatal(e)
	}
	if a.ID == b.ID || !IsManual(a.ID) || !r.Contains(a.ID) {
		t.Fatal(r)
	}
	r, renamed, e := Save(root, r.Revision, a.ID, "Personal collection")
	if e != nil || renamed.ID != a.ID || r.Labels()[a.ID] != "Personal collection" {
		t.Fatal(r, e)
	}
	if _, _, e = Save(root, first, "", "Stale"); e == nil {
		t.Fatal("stale accepted")
	}
	if _, _, e = Save(root, r.Revision, "", " personal COLLECTION "); e == nil {
		t.Fatal("duplicate name accepted")
	}
	for _, name := range []string{"", "\n", "bad\x00name", strings.Repeat("я", 121)} {
		if _, _, e = Save(root, r.Revision, "", name); e == nil {
			t.Fatal(name)
		}
	}
	if _, e = os.Stat(filepath.Join(root, "config", "sources.yaml")); !os.IsNotExist(e) {
		t.Fatal("source priorities modified")
	}
	if _, e = os.Stat(filepath.Join(root, "platforms")); !os.IsNotExist(e) {
		t.Fatal("card data created")
	}
}
