package launchbox

import (
	"reflect"
	"rommeta/internal/sourceyaml"
	"testing"
)

func TestMediaCandidatesPreserveFirstEvidenceAndProviderOrder(t *testing.T) {
	images := []launchboxImage{
		{Type: "Box - Front", FileName: "z.png", Region: "USA", CRC32: "1234"},
		{Type: "Clear Logo", FileName: "logo.png"},
		{Type: "Box - Front - Reconstructed", FileName: "a.png", Region: "Europe"},
		{Type: "Box - Front", FileName: "z.png", Region: "Japan"},
		{Type: "Box - Back", FileName: "z.png"},
		{Type: "unknown", FileName: "skip.png"},
		{Type: "Box - Front"},
	}
	got := mediaCandidates(images)
	want := map[string][]sourceyaml.MediaCandidate{
		"box_front": {
			{URL: "https://images.launchbox-app.com/z.png", Region: "USA", SourceFile: "z.png", SourceType: "Box - Front", CRC32: "1234"},
			{URL: "https://images.launchbox-app.com/a.png", Region: "Europe", SourceFile: "a.png", SourceType: "Box - Front - Reconstructed"},
		},
		"unknown":  {{URL: "https://images.launchbox-app.com/skip.png", SourceFile: "skip.png", SourceType: "unknown"}},
		"logo":     {{URL: "https://images.launchbox-app.com/logo.png", SourceFile: "logo.png", SourceType: "Clear Logo"}},
		"box_back": {{URL: "https://images.launchbox-app.com/z.png", SourceFile: "z.png", SourceType: "Box - Back"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}
