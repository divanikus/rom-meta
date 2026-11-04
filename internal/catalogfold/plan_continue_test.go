package catalogfold

import (
	"context"
	"path/filepath"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogfstest"
	"testing"
)

func TestContinuePreservesUnchangedDraftAndConsumesOnlyApplied(t *testing.T) {
	for _, mode := range []string{"unchanged", "changed", "packed"} {
		stale := mode == "changed"
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			webPlanFixture(t, root)
			for _, folder := range []string{"GardenA", "GardenB"} {
				writeFoldGame(t, root, "Console", folder, "Garden ("+folder+")", "Garden", "22")
				path := filepath.Join(root, "platforms", "Console", folder, "game.yaml")
				writeFoldFile(t, path, readFoldFile(t, path)+"\ndescription: "+folder+"\n")
			}
			if mode == "packed" {
				catalogfstest.Pack(t, filepath.Join(root, "platforms", "Console"))
			}
			p, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			var orbit, garden PlannedGroup
			for _, g := range p.Groups {
				if g.Group.Keep.Title == "Orbit" {
					orbit = g
				} else {
					garden = g
				}
			}
			draft, err := p.Preview(garden.ID, Draft{Primary: "GardenB", Fields: catalogedit.Patch{"description": "Chosen"}, Confirm: true, Reason: "Reviewed"})
			if err != nil || draft.Status != "ready" {
				t.Fatal(draft, err)
			}
			decisions := []Decision{{ID: orbit.ID, Revision: orbit.Revision, Action: "merge"}}
			if _, err = p.Apply(context.Background(), decisions); err != nil {
				t.Fatal(err)
			}
			if stale {
				path := filepath.Join(root, "platforms", "Console", "GardenA", "game.yaml")
				writeFoldFile(t, path, readFoldFile(t, path)+"\nplayers: 2\n")
			}
			p.Close()
			next, err := p.Continue(context.Background(), decisions)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if len(next.Groups) != 1 {
				t.Fatalf("remaining: %+v", next.Groups)
			}
			g := next.Groups[0]
			if stale {
				if len(g.Group.Edits) > 0 || g.Group.ReviewAccepted {
					t.Fatal("changed cards inherited a draft")
				}
			} else {
				if g.Status != "ready" || g.Group.Keep.Folder != "GardenB" || g.Group.Edits["description"] != "Chosen" {
					t.Fatalf("draft lost: %+v", g)
				}
				if _, err = next.Apply(context.Background(), []Decision{{ID: g.ID, Revision: g.Revision, Action: "merge"}}); err != nil {
					t.Fatal(err)
				}
				done, err := next.Continue(context.Background(), []Decision{{ID: g.ID, Revision: g.Revision, Action: "merge"}})
				if err != nil {
					t.Fatal(err)
				}
				defer done.Close()
				if len(done.Groups) != 0 {
					t.Fatal("finished groups returned")
				}
			}
		})
	}
}
func TestContinueManualReviewDoesNotExpandToPlatform(t *testing.T) {
	root := t.TempDir()
	webPlanFixture(t, root)
	writeFoldGame(t, root, "Console", "OtherA", "Other (USA)", "Other", "22")
	writeFoldGame(t, root, "Console", "OtherB", "Other (Japan)", "Other", "22")
	p, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, []string{"One", "Two"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	g, err := p.Preview(0, Draft{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	d := []Decision{{ID: 0, Revision: g.Revision, Action: "merge"}}
	if _, err = p.Apply(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	next, err := p.Continue(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if len(next.Groups) != 0 {
		t.Fatal("manual selection expanded into unrelated groups")
	}
}

// ReviewRequired also covers browser identity decisions, not only Xbox serial hints.
func TestContinueKeepSeparateDoesNotRepeatIdentityReview(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			root := t.TempDir()
			writeFoldGame(t, root, "Console", "One", "Orbit (Pressing 1)", "Orbit", "10")
			writeFoldGame(t, root, "Console", "Two", "Orbit (Pressing 2)", "Orbit", "10")
			if packed {
				catalogfstest.Pack(t, filepath.Join(root, "platforms", "Console"))
			}
			p, err := BuildPlan(Options{DataRoot: root, Platform: "Console"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if len(p.Groups) != 1 || p.Groups[0].Status != "review" {
				t.Fatal(p.Groups)
			}
			d := []Decision{{ID: 0, Revision: p.Groups[0].Revision, Action: "separate"}}
			result, err := p.Apply(context.Background(), d)
			if err != nil || result.KeptSeparate != 1 {
				t.Fatal(result, err)
			}
			next, err := p.Continue(context.Background(), d)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if len(next.Groups) != 0 {
				t.Fatal("kept-separate review returned", next.Groups)
			}
		})
	}
}
