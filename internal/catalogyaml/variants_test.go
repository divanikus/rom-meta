package catalogyaml

import (
	"strings"
	"testing"
)

func TestVariantGraph(t *testing.T) {
	valid := "releases:\n  Base (Japan): {}\nvariants:\n  English:\n    kind: translation\n    based_on: Base (Japan)\n    languages: [en]\n  Fixed:\n    kind: bugfix\n    based_on: English\n"
	doc, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	chain, base, err := VariantChain(doc.Root(), "Fixed")
	if err != nil || strings.Join(chain, ",") != "Fixed,English" || base != "Base (Japan)" {
		t.Fatalf("%v %q %v", chain, base, err)
	}
	for _, broken := range []string{
		strings.Replace(valid, "based_on: English", "based_on: Fixed", 1),
		strings.Replace(valid, "based_on: Base (Japan)", "based_on: Missing", 1),
		strings.Replace(valid, "based_on: Base (Japan)", "based_on: Fixed", 1),
		strings.Replace(valid, "kind: translation", "kind: ''", 1),
		strings.Replace(valid, "kind: translation", "kind: translation\n    based_on: Missing", 1),
		valid + "variants: {}\n",
		strings.Replace(valid, "  English:", "  Base (Japan):", 1),
	} {
		doc, err := Parse([]byte(broken))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Variants(doc.Root()); err == nil {
			t.Fatalf("accepted invalid graph:\n%s", broken)
		}
	}
	for _, input := range []string{"release: Base (Japan)\nvariant: English\n", "variant: Missing\n", "file: {}\n"} {
		fp, err := Parse([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := FingerprintTarget(doc.Root(), fp.Root()); err == nil {
			t.Fatal("accepted invalid fingerprint", input)
		}
	}
}

func TestGameOnlyVariantChainAndOptionalBaseValidation(t *testing.T) {
	const source = "releases:\n  Game (Japan): {}\nvariants:\n  English:\n    kind: translation\n    languages: [en]\n  Fixed:\n    kind: bugfix\n    based_on: English\n"
	doc, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"English", "Fixed"} {
		chain, base, err := VariantChain(doc.Root(), name)
		if err != nil || base != "" || chain[len(chain)-1] != "English" {
			t.Fatalf("%s: %v %q %v", name, chain, base, err)
		}
	}
	// Omission means game-only. Malformed supplied references remain errors.
	for _, value := range []string{"''", "null", "[]", "{}", "42", "Missing", "Fixed"} {
		raw := strings.Replace(source, "    languages:", "    based_on: "+value+"\n    languages:", 1)
		broken, err := Parse([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Variants(broken.Root()); err == nil {
			t.Fatalf("accepted based_on: %s", value)
		}
	}
}
