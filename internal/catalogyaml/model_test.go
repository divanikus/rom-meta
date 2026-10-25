package catalogyaml

import "testing"

func TestParseNavigatesQuotedMappingsAndBlockSequences(t *testing.T) {
	document, err := Parse([]byte(`title: "Game: One"
aliases:
  - First
  - "Second: Name"
releases:
  "Game (USA)":
    regions: [USA]
unknown:
  keep: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := Scalar(Value(document.Root(), "title")); got != "Game: One" {
		t.Fatalf("title = %q", got)
	}
	aliases := Strings(Value(document.Root(), "aliases"))
	if len(aliases) != 2 || aliases[1] != "Second: Name" {
		t.Fatalf("aliases = %#v", aliases)
	}
	if Value(document.Root(), "unknown") == nil {
		t.Fatal("unknown node was not preserved")
	}
}

func TestParseRejectsMalformedYAML(t *testing.T) {
	if _, err := Parse([]byte("title: [broken\n")); err == nil {
		t.Fatal("expected malformed YAML error")
	}
}

func TestParseDropsForbiddenLegacyControlCharacters(t *testing.T) {
	document, err := Parse([]byte("title: Game\ndescription: |-\n  Link Dragon (é¾\u008d)\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := Scalar(Value(document.Root(), "description")); got != "Link Dragon (é¾)" {
		t.Fatalf("description = %q", got)
	}
}
