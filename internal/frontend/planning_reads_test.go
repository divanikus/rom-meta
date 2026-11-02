package frontend

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanningKeepsOriginalDestinationFingerprint(t *testing.T) {
	root := t.TempDir()
	put(t, root, "metadata.xml", "original")
	p := &Plan{Root: root, Package: t.TempDir(), guards: map[string]fingerprint{}}
	if _, err := p.read(root, "metadata.xml", false); err != nil {
		t.Fatal(err)
	}
	put(t, root, "metadata.xml", "external edit during planning")
	if _, err := p.write("metadata.xml", []byte("planned output"), ""); err != nil {
		t.Fatal(err)
	}
	if err := p.validate(); err == nil || !strings.Contains(err.Error(), "changed since planning") {
		t.Fatal("planning forgot the original XML observation", err)
	}
}

func TestPlanningReusesPackageAssetProof(t *testing.T) {
	pkg, root := t.TempDir(), t.TempDir()
	put(t, pkg, "asset.png", "original asset")
	p := &Plan{Root: root, Package: pkg, guards: map[string]fingerprint{}}
	source := filepath.Join(pkg, "asset.png")
	if _, err := p.write("first.png", nil, source); err != nil {
		t.Fatal(err)
	}
	before := p.files[0].hash
	put(t, pkg, "asset.png", "changed asset")
	if _, err := p.write("asset.png", nil, source); err != nil {
		t.Fatal(err)
	}
	if len(p.files) != 2 || p.files[1].hash != before {
		t.Fatal("asset was hashed again during planning")
	}
	if err := p.validate(); err == nil || !strings.Contains(err.Error(), "changed since planning") {
		t.Fatal(err)
	}
}

func TestNoIncomingArtworkDoesNotInspectDestinationImages(t *testing.T) {
	pkg, root := t.TempDir(), t.TempDir()
	// This unrelated, unusable Images path must not block a metadata-only plan.
	put(t, root, "Images", "not an artwork directory")
	p := &Plan{Root: root, Package: pkg, guards: map[string]fingerprint{}}
	added, fields, err := p.media(platform, a, a, map[string]string{}, map[string]string{})
	if err != nil || added != 0 || len(fields) != 0 {
		t.Fatal(added, fields, err)
	}
}
