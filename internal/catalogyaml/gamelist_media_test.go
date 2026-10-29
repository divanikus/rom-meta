package catalogyaml

import "testing"

func TestGamelistReferencesUnionAndBoundaries(t *testing.T) {
	doc, err := Parse([]byte("sources:\n  gamelist.first:\n    matches:\n      - media: {box_front: [{url: media/A/old.png}]}\n      - media: {box_front: [{url: media/A/new.png}]}\n  gamelist.other:\n    matches: []\n  launchbox:\n    matches:\n      - media: {box_front: [{url: 'https://example.test/image.png'}]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	refs, err := GamelistMediaReferences(doc.Root())
	if err != nil || len(refs["gamelist.first"]) != 2 || len(refs["gamelist.other"]) != 0 || len(refs) != 2 {
		t.Fatal(refs, err)
	}
	for _, p := range []string{"../outside.png", "media/A/../../outside.png", "C:/outside.png", "https://example.test/image.png"} {
		bad, _ := Parse([]byte("sources:\n  gamelist.first:\n    matches:\n      - media: {box_front: [{url: '" + p + "'}]}\n"))
		if _, err := GamelistMediaReferences(bad.Root()); err == nil {
			t.Fatal("unsafe reference accepted", p)
		}
	}
}
