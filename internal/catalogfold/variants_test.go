package catalogfold

import (
	"rommeta/internal/catalogyaml"
	"strings"
	"testing"
)

func TestFoldPreservesDerivedVariantsAndBase(t *testing.T) {
	first, err := parseGameYAML([]byte("title: Game\nreleases:\n  Japanese: {}\nvariants:\n  English:\n    kind: translation\n    based_on: Japanese\n    provenance: preserved\n"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseGameYAML([]byte("title: Game\nreleases:\n  USA: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	merged := renderMergedGameYAML([]catalogGame{first, second}, regionRules{})
	doc, err := catalogyaml.Parse([]byte(merged))
	if err != nil {
		t.Fatal(err)
	}
	_, base, err := catalogyaml.VariantChain(doc.Root(), "English")
	if err != nil || base != "Japanese" || !strings.Contains(merged, "provenance: preserved") {
		t.Fatalf("%s %v", merged, err)
	}
}
