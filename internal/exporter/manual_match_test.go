package exporter

import "testing"

func TestManualMatchCannotOverrideReaderOrVerifiedEvidence(t *testing.T) {
	for _, r := range []Record{
		{Status: "unsupported", InputGuard: &InputGuard{}},
		{Status: "unmatched"},
		{Status: "matched", MatchedBy: "hash", InputGuard: &InputGuard{}},
		{Status: "unmatched", Hidden: true, InputGuard: &InputGuard{}},
		{Status: "unmatched", SourceOnly: true, InputGuard: &InputGuard{}},
	} {
		if CanManuallyMatch(r) {
			t.Fatalf("unsafe input accepted: %+v", r)
		}
	}
	for _, r := range []Record{
		{Status: "unmatched", InputGuard: &InputGuard{}},
		{Status: "ambiguous", InputGuard: &InputGuard{}},
		{Status: "matched", MatchedBy: "game-name", InputGuard: &InputGuard{}},
	} {
		if !CanManuallyMatch(r) {
			t.Fatalf("reviewable input rejected: %+v", r)
		}
	}
}
