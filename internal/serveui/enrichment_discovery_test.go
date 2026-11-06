package serveui

import (
	"encoding/json"
	"path/filepath"
	"rommeta/internal/enrichment"
	"rommeta/internal/platformnames"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
	"testing"
)

func TestUnknownPlatformCanBeQueuedAndResolutionFailureDoesNotStopNextSource(t *testing.T) {
	s, _ := enrichmentTestServer(t)
	const platform = "Future Vendor - New System"
	writeEnrichmentFixture(t, filepath.Join(s.root, "platforms", platform, "Orbit", "game.yaml"), "title: Orbit\n")
	options, _, _, err := s.enrichmentChoices(platform)
	if err != nil {
		t.Fatal(err)
	}
	for _, choice := range options.Choices {
		if choice.Source == "launchbox" && choice.Type == "metadata" && (!choice.Available || !choice.Default) {
			t.Fatal(choice)
		}
	}
	w, _ := catalogCall(t, s, "/api/catalog/enrichment-plan", enrichmentRequest{
		Platform: platform, Revision: options.Revision, Jobs: []enrichmentSelection{{"launchbox", "metadata"}, {"openvgdb", "metadata"}},
	})
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var op libraryOperation
	json.Unmarshal(w.Body.Bytes(), &op)
	calls := 0
	s.enrichRun = func(job enrichment.Job, _ enrichment.Options, _ store.SourcesConfig, _ taskprogress.Func) (any, error) {
		calls++
		if job.Source == "launchbox" {
			return nil, &platformnames.ResolutionError{Source: job.Source, Platform: platform}
		}
		return nil, nil
	}
	if err = s.beginEnrichment(op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	done := waitImport(t, s, op.ID, "incomplete")
	waitQueueIdle(t, s)
	if calls != 2 || done.Enrichment.Jobs[0].State != "skipped" || done.Enrichment.Jobs[1].State != "succeeded" {
		t.Fatal(calls, done)
	}
}
