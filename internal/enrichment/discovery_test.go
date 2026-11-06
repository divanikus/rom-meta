package enrichment

import (
	"errors"
	"rommeta/internal/platformnames"
	"testing"
)

func TestMissingMediaPlatformKeepsNetworkErrorsSeparate(t *testing.T) {
	skipped := map[string]string{"New System": "no directory"}
	var unresolved *platformnames.ResolutionError
	if !errors.As(missingMediaPlatform("New System", skipped, nil), &unresolved) {
		t.Fatal("missing directory not skipped")
	}
	network := errors.New("listing failed")
	if missingMediaPlatform("New System", skipped, network) != network {
		t.Fatal("lost network error")
	}
	if err := missingMediaPlatform("Other", skipped, nil); err != nil {
		t.Fatal(err)
	}
}
