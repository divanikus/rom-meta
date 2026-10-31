package frontend

import (
	"bytes"
	"encoding/json"
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
