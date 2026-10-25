package main

import (
	"strings"
	"testing"
)

func TestCatalogFoldReviewPlanArguments(t *testing.T) {
	for _, args := range [][]string{{"--review-plan"}, {"--review-plan="}, {"--review-plan", "p.json", "--interactive"}, {"--review-plan=p.json", "--platform", "nes"}, {"--review-plan=p.json", "--match", "game"}, {"--review-plan=p.json", "--editions-only"}} {
		if err := runCatalogFold(args); err == nil || !strings.Contains(err.Error(), "--review-plan") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}
