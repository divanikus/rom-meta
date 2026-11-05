package serveui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfold"
	"testing"
)

func batchFoldFixture(t *testing.T) (string, http.Handler) {
	t.Helper()
	root := editFixture(t)
	for _, folder := range []string{"One", "Two"} {
		for _, name := range []string{"game.yaml", "fingerprints.yaml"} {
			data, err := os.ReadFile(filepath.Join(root, "platforms", "Console", folder, name))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "platforms", "Other", folder, name)
			if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	h, err := HandlerWithEditing(root, true)
	if err != nil {
		t.Fatal(err)
	}
	return root, h
}
func buildFoldBatch(t *testing.T, h http.Handler) foldPlanResponse {
	t.Helper()
	w, _ := catalogCall(t, h, "/api/catalog/fold-plan", foldPlanRequest{Platforms: []string{"Console", "Other"}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var plan foldPlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Platforms) != 2 || len(plan.Groups) != 2 || plan.Groups[0].ID == plan.Groups[1].ID {
		t.Fatal(plan)
	}
	for i, g := range plan.Groups {
		if g.Group.Platform != plan.Platforms[i] || g.Status != "ready" {
			t.Fatal(g)
		}
	}
	return plan
}
func TestFoldBatchPreviewApplyAndContinuation(t *testing.T) {
	root, h := batchFoldFixture(t)
	plan := buildFoldBatch(t, h)
	// Inspect a second-platform group whose local group ID and folder names collide.
	w, r := catalogCall(t, h, "/api/catalog/fold-preview", foldPreviewRequest{Plan: plan.ID, Group: 1, Draft: &catalogfold.Draft{Primary: "Two", Confirm: true}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	g := r["group"].(map[string]any)
	if g["id"] != float64(1) || g["group"].(map[string]any)["Platform"] != "Other" {
		t.Fatal(r)
	}
	w, r = catalogCall(t, h, "/api/catalog/fold-apply", foldApplyRequest{Plan: plan.ID, Continue: true, Decisions: []catalogfold.Decision{{ID: 0, Revision: 1, Action: "merge"}}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	data, _ := json.Marshal(r["next_plan"])
	var next foldPlanResponse
	json.Unmarshal(data, &next)
	if next.ID == plan.ID || len(next.Platforms) != 2 || len(next.Groups) != 1 || next.Groups[0].Group.Platform != "Other" || next.Groups[0].Group.Keep.Folder != "Two" {
		t.Fatal(next)
	}
	w, _ = catalogCall(t, h, "/api/catalog/fold-apply", foldApplyRequest{Plan: next.ID, Continue: true, Decisions: []catalogfold.Decision{{ID: next.Groups[0].ID, Revision: next.Groups[0].Revision, Action: "merge"}}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, platform := range []string{"Console", "Other"} {
		entries, err := os.ReadDir(filepath.Join(root, "platforms", platform))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range entries {
			if entry.IsDir() {
				count++
			}
		}
		if count != 1 {
			t.Fatal(platform, count)
		}
	}
}
func TestFoldBatchPrevalidatesLaterPlatformBeforeWrites(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "revision", true: "files"}[stale], func(t *testing.T) {
			root, h := batchFoldFixture(t)
			plan := buildFoldBatch(t, h)
			decisions := []catalogfold.Decision{{ID: 0, Revision: 1, Action: "merge"}, {ID: 1, Revision: 1, Action: "merge"}}
			if stale {
				path := filepath.Join(root, "platforms", "Other", "One", "game.yaml")
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
				if err != nil {
					t.Fatal(err)
				}
				f.WriteString("changed: true\n")
				f.Close()
			} else {
				decisions[1].Revision = 99
			}
			w, r := catalogCall(t, h, "/api/catalog/fold-apply", foldApplyRequest{Plan: plan.ID, Decisions: decisions})
			if w.Code != 409 || r["result"].(map[string]any)["applied"] != float64(0) {
				t.Fatal(w.Body.String())
			}
			for _, folder := range []string{"One", "Two"} {
				if _, err := os.Stat(filepath.Join(root, "platforms", "Console", folder, "game.yaml")); err != nil {
					t.Fatal("earlier platform changed", err)
				}
			}
		})
	}
}
func TestFoldBatchAppliesBothPlatformsInOneRequest(t *testing.T) {
	_, h := batchFoldFixture(t)
	plan := buildFoldBatch(t, h)
	w, r := catalogCall(t, h, "/api/catalog/fold-apply", foldApplyRequest{Plan: plan.ID, Continue: true, Decisions: []catalogfold.Decision{{ID: 0, Revision: 1, Action: "merge"}, {ID: 1, Revision: 1, Action: "merge"}}})
	if w.Code != 200 || r["result"].(map[string]any)["applied"] != float64(2) || len(r["next_plan"].(map[string]any)["groups"].([]any)) != 0 {
		t.Fatal(w.Body.String())
	}
}
func TestFoldBatchRejectsMixedAndDuplicateScope(t *testing.T) {
	_, h := batchFoldFixture(t)
	for _, req := range []foldPlanRequest{
		{Platforms: []string{"Console", "Console"}},
		{Platforms: []string{"Console", "../Other"}},
		{Platforms: []string{"Console", "Other"}, Games: []string{"One", "Two"}},
		{Platform: "Console", Platforms: []string{"Other"}},
	} {
		w, _ := catalogCall(t, h, "/api/catalog/fold-plan", req)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
