package catalogyaml

import "testing"

func TestParseRejectsTrailingDocuments(t *testing.T) {
	for _, input := range []string{
		"title: First\n---\ntitle: Second\n",
		"title: First\n---\n",
		"title: First\n---\nbroken: [\n",
	} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("accepted trailing YAML document: %q", input)
		}
	}
}

func TestParseAcceptsSingleDocumentWithEndMarker(t *testing.T) {
	if _, err := Parse([]byte("---\ntitle: First\n...\n# comment\n")); err != nil {
		t.Fatal(err)
	}
}
