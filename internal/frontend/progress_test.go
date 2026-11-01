package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestNativePlanningProgressPreservesPlanAndReachesCompletion(t *testing.T) {
	for _, target := range []string{"es-de", "launchbox"} {
		t.Run(target, func(t *testing.T) {
			options := esdeSetup(t, "")
			if target == "launchbox" {
				options = setup(t, "", game(a, "Example", "/roms/example.chd", ""))
			}
			original := mustBuild(t, options)
			var stages []string
			completed := false
			options.Progress = func(stage, name string, current, total int) {
				stages = append(stages, stage)
				if total > 0 && (current < 0 || current > total) {
					t.Fatalf("invalid stage count: %s %d/%d", stage, current, total)
				}
				if stage == "plan library/media" && total > 0 && current == total {
					completed = true
				}
			}
			observed := mustBuild(t, options)
			a, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(observed)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) {
				t.Fatal("progress changes native plan")
			}
			if len(stages) == 0 || stages[0] != "read destination" || !completed {
				t.Fatal("native work is silent/incomplete", stages)
			}
			if err = observed.Check(); err != nil {
				t.Fatal(err)
			}
			if stages[len(stages)-1] != "check files" {
				t.Fatal("freshness checks are silent", stages)
			}
		})
	}
}

func TestLaunchBoxPlansEachCardOnceAndReusesVersionMatches(t *testing.T) {
	incoming := ""
	const count = 8
	for i := 0; i < count; i++ {
		gameID := fmt.Sprintf("40000000-0000-0000-0000-%012d", i+1)
		versionID := fmt.Sprintf("50000000-0000-0000-0000-%012d", i+1)
		launch := fmt.Sprintf("ROMs/game%d.zip", i)
		incoming += game(gameID, fmt.Sprint("Game ", i), launch, "") + version(versionID, gameID, launch, "")
	}
	options := setup(t, "", incoming)
	seen := map[string]int{}
	completions := 0
	options.Progress = func(stage, name string, current, total int) {
		if stage == "match library" || stage == "check versions" {
			t.Fatal("separate card pass remains", stage)
		}
		if stage == "plan library/media" {
			if total != count {
				t.Fatal("wrong total", total)
			}
			if current == total {
				completions++
			} else {
				seen[name]++
			}
		}
	}
	p := mustBuild(t, options)
	if completions != 1 || len(seen) != count {
		t.Fatal("card pass reset or incomplete", seen, completions)
	}
	for _, visits := range seen {
		if visits != 1 {
			t.Fatal("card revisited", seen)
		}
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	options.Progress = nil
	if repeat := mustBuild(t, options); len(repeat.Files) != 0 {
		t.Fatal("single-pass repeat changed data", repeat.Files)
	}
}

func TestLateSharedTargetConflictInvalidatesEarlierDraft(t *testing.T) {
	const third = "10000000-0000-0000-0000-000000000004"
	existing := game(a, "Owner", "ROMs/us.zip", "<Favorite>true</Favorite>") + version(v1, a, "ROMs/eu.zip", "") + version(v2, a, "ROMs/jp.zip", "")
	incoming := game(b, "First", "ROMs/us.zip", "") + game(c, "Second", "ROMs/eu.zip", "") + game(third, "Third", "ROMs/jp.zip", "")
	options := setup(t, existing, incoming)
	put(t, options.PackageRoot, "Images/"+platform+"/Box - Front/"+b+"-01.png", "first planned asset")
	before := get(t, options.Destination, platformFile)
	p := mustBuild(t, options)
	if len(p.Conflicts) != 3 || len(p.Files) != 0 {
		t.Fatal("late conflict retained draft writes", p.Conflicts, p.Files)
	}
	for _, change := range p.Changes {
		if change.Action != "conflict" || change.Media != 0 || len(change.Fields) != 0 {
			t.Fatal("earlier card not invalidated", change)
		}
	}
	if err := p.Apply(); err == nil || !strings.Contains(err.Error(), "3 conflicts") {
		t.Fatal(err)
	}
	if !bytes.Equal(before, get(t, options.Destination, platformFile)) {
		t.Fatal("late conflict changed native data")
	}
}
