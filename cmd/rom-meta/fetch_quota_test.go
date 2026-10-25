package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"rommeta/internal/screenscraper"
	"rommeta/internal/store"
)

func TestFetchQuotaSkipsSameProviderAndContinuesOthers(t *testing.T) {
	jobs := []fetchJob{
		{Source: "screenscraper", Type: "metadata"},
		{Source: "igdb", Type: "artwork"},
		{Source: "screenscraper", Type: "artwork"},
		{Source: "emumovies", Type: "video"},
	}
	var out bytes.Buffer
	var ran []string
	err := runFetchJobs(&out, jobs, fetchOptions{}, store.SourcesConfig{}, func(job fetchJob, _ fetchOptions, _ store.SourcesConfig) error {
		ran = append(ran, job.Source+"/"+job.Type)
		if job.Source == "screenscraper" {
			return fmt.Errorf("provider stopped: %w", screenscraper.ErrDailyQuota)
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "daily quota") {
		t.Fatalf("quota must return an unsuccessful run: %v", err)
	}
	if got := strings.Join(ran, ","); got != "screenscraper/metadata,igdb/artwork,emumovies/video" {
		t.Fatalf("wrong jobs after quota: %s", got)
	}
	if !strings.Contains(out.String(), "Result: skipped; ScreenScraper daily quota") {
		t.Fatalf("missing skipped job reason: %s", out.String())
	}
}
