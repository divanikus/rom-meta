package catalogyaml

import "testing"

func TestMetadataDefaultDoesNotChangeGameOnlyBase(t *testing.T) {
	for _, declared := range []string{"Game (Japan)", "Missing", ""} {
		raw := "releases:\n  Game (Japan): {}\n  Game (USA): {}\nvariants:\n  English:\n    kind: translation\n"
		if declared != "" {
			raw += "default_release: " + declared + "\n"
		}
		doc, err := Parse([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		want := "Game (USA)"
		if declared == "Game (Japan)" {
			want = declared
		}
		if got := DefaultRelease(doc.Root()); got != want {
			t.Fatalf("default %q: %q", declared, got)
		}
		_, base, err := VariantChain(doc.Root(), "English")
		if err != nil || base != "" {
			t.Fatalf("metadata default became ancestry: %q %v", base, err)
		}
	}
	doc, err := Parse([]byte("title: Game\nvariants:\n  English:\n    kind: translation\n"))
	if err != nil || DefaultRelease(doc.Root()) != "" {
		t.Fatalf("no release: %v", err)
	}
}
