package catalogfold

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func splitFixture(t *testing.T, root string) (SplitPlan, string) {
	t.Helper()
	releaseA, releaseB := "Orbit (USA)", "Orbit (Japan) (Demo)"
	writeFoldGame(t, root, "Console", "Orbit", releaseA, "Orbit", "10")
	path := filepath.Join(root, "platforms", "Console", "Orbit")
	game := readFoldFile(t, filepath.Join(path, "game.yaml"))
	game = strings.Replace(game, "releases:\n", "manual_note: preserve\nreleases:\n  \""+releaseB+"\": {title: Orbit, custom: keep}\n", 1)
	game += "  igdb:\n    matches:\n      - igdb_id: '20'\n        title: Demo\n        platforms: [Console]\n        release: \"" + releaseB + "\"\n"
	writeFoldFile(t, filepath.Join(path, "game.yaml"), game)
	fp := readFoldFile(t, filepath.Join(path, "fingerprints.yaml")) + "  - id: demo\n    release: \"" + releaseB + "\"\n    sha1: deadbeef\n    custom: preserve\n"
	writeFoldFile(t, filepath.Join(path, "fingerprints.yaml"), fp)
	for name, data := range map[string]string{"media/cover.jpg": "original artwork", "media/" + releaseB + "/cover.jpg": "demo artwork", "notes.txt": "original extra"} {
		writeFoldFile(t, filepath.Join(path, filepath.FromSlash(name)), data)
	}
	hash := func(name string) string {
		return fmt.Sprintf("%x", sha256.Sum256([]byte(readFoldFile(t, filepath.Join(path, name)))))
	}
	group := SplitGroup{Folder: "Orbit", GameSHA256: hash("game.yaml"), FingerprintsSHA256: hash("fingerprints.yaml"), Reason: "Explicit demo is separate from retail", Parts: []SplitPart{{Folder: "Orbit", Title: "Orbit", ContentKey: "retail-key", Releases: []string{releaseA}}, {Folder: "Orbit Demo", Title: "Orbit (Demo)", ContentKey: "demo-key", Releases: []string{releaseB}}}}
	for _, name := range []string{"media/cover.jpg", "media/" + releaseB + "/cover.jpg", "notes.txt"} {
		info, err := os.Stat(filepath.Join(path, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		target := "Orbit"
		if strings.Contains(name, releaseB) {
			target = "Orbit Demo"
		}
		group.Files = append(group.Files, SplitFile{Path: name, Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), SHA256: hash(filepath.FromSlash(name)), Reason: "Reviewed release ownership", Targets: []SplitTarget{{Folder: target, Path: name}}})
	}
	plan := SplitPlan{Version: 1, Platform: "Console", Groups: []SplitGroup{group}}
	return plan, writeSplitFixture(t, root, plan)
}
func writeSplitFixture(t *testing.T, root string, plan SplitPlan) string {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "split.json")
	writeFoldFile(t, path, string(data))
	return path
}
func TestReviewedSplitPreservesAllPayloadsAndFiles(t *testing.T) {
	root := t.TempDir()
	_, plan := splitFixture(t, root)
	before := catalogBytes(t, root)
	preview, err := Split(SplitOptions{DataRoot: root, ReviewPlan: plan})
	if err != nil || preview.NewCards != 1 || preview.Releases != 2 || preview.Fingerprints != 2 {
		t.Fatal(err, preview)
	}
	if !reflect.DeepEqual(before, catalogBytes(t, root)) {
		t.Fatal("preview wrote files")
	}
	result, err := Split(SplitOptions{DataRoot: root, ReviewPlan: plan, Apply: true})
	if err != nil || result.Backup == "" {
		t.Fatal(err, result)
	}
	original := readContentMap(t, filepath.Join(result.Backup, "Orbit", "game.yaml"))
	releases := map[string]any{}
	fps := []any{}
	sourceCount := 0
	for _, folder := range []string{"Orbit", "Orbit Demo"} {
		game := readContentMap(t, filepath.Join(root, "platforms", "Console", folder, "game.yaml"))
		for name, value := range game["releases"].(map[string]any) {
			releases[name] = value
		}
		fp := readContentMap(t, filepath.Join(root, "platforms", "Console", folder, "fingerprints.yaml"))
		fps = append(fps, fp["items"].([]any)...)
		for _, raw := range game["sources"].(map[string]any) {
			stanza := raw.(map[string]any)
			if ms, ok := stanza["matches"].([]any); ok {
				sourceCount += len(ms)
			}
		}
	}
	if !reflect.DeepEqual(releases, original["releases"]) || len(fps) != 2 || sourceCount != 2 {
		t.Fatal("metadata payload loss", releases, fps, sourceCount)
	}
	for path, want := range map[string]string{filepath.Join(root, "platforms", "Console", "Orbit", "media", "cover.jpg"): "original artwork", filepath.Join(root, "platforms", "Console", "Orbit Demo", "media", "Orbit (Japan) (Demo)", "cover.jpg"): "demo artwork", filepath.Join(result.Backup, "Orbit", "media", "Orbit (Japan) (Demo)", "cover.jpg"): "demo artwork", filepath.Join(root, "platforms", "Console", "Orbit", "notes.txt"): "original extra"} {
		if readFoldFile(t, path) != want {
			t.Fatal("file changed", path)
		}
	}
	if _, err = Split(SplitOptions{DataRoot: root, ReviewPlan: plan, Apply: true}); err == nil {
		t.Fatal("consumed plan replayed")
	}
}
func TestReviewedSplitRejectsInvalidWholePlanBeforeWrites(t *testing.T) {
	for _, mode := range []string{"omitted release", "duplicate release", "orphan fingerprint", "omitted file", "unplanned file", "changed bytes", "stale hash", "case collision", "path traversal", "contradictory source route", "cross-partition anchor"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			plan, _ := splitFixture(t, root)
			group := &plan.Groups[0]
			path := filepath.Join(root, "platforms", "Console", "Orbit")
			switch mode {
			case "omitted release":
				group.Parts[1].Releases = []string{"Unknown"}
			case "duplicate release":
				group.Parts[1].Releases = group.Parts[0].Releases
			case "orphan fingerprint":
				file := filepath.Join(path, "fingerprints.yaml")
				writeFoldFile(t, file, readFoldFile(t, file)+"  - release: Missing\n    sha1: lost\n")
				group.FingerprintsSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(readFoldFile(t, file))))
			case "omitted file":
				group.Files = group.Files[:2]
			case "unplanned file":
				writeFoldFile(t, filepath.Join(path, "new.txt"), "new")
			case "changed bytes":
				writeFoldFile(t, filepath.Join(path, "notes.txt"), "modified")
			case "stale hash":
				group.GameSHA256 = strings.Repeat("0", 64)
			case "case collision":
				group.Parts[1].Folder = "ORBIT"
			case "path traversal":
				group.Files[0].Targets[0].Path = "../outside"
			case "cross-partition anchor":
				file := filepath.Join(path, "game.yaml")
				game := strings.ReplaceAll(readFoldFile(t, file), "manual_note: preserve", "manual_note: &common {key: keep}")
				game = strings.ReplaceAll(game, "custom: keep}", "custom: *common}")
				writeFoldFile(t, file, game)
				group.GameSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(game)))
			case "contradictory source route":
				group.SourceRoutes = []SplitSourceRoute{{Source: "igdb", MatchIndex: 0, Folder: "Orbit"}}
			}
			planPath := writeSplitFixture(t, root, plan)
			before := catalogBytes(t, root)
			if _, err := Split(SplitOptions{DataRoot: root, ReviewPlan: planPath, Apply: true}); err == nil {
				t.Fatal("invalid plan applied")
			}
			if !reflect.DeepEqual(before, catalogBytes(t, root)) {
				t.Fatal("invalid plan wrote files")
			}
			if _, err := os.Stat(filepath.Join(root, "backups")); !os.IsNotExist(err) {
				t.Fatal("rejected preflight created transaction")
			}
		})
	}
}
func TestReviewedContentKeysPreventRefoldingAndSurviveCompatibleFold(t *testing.T) {
	root := t.TempDir()
	for _, item := range []struct{ folder, key, region string }{{"Main", "main-key", "USA"}, {"Expanded", "expanded-key", "Japan"}} {
		writeFoldGame(t, root, "Console", item.folder, "Orbit ("+item.region+")", "Orbit", "10")
		path := filepath.Join(root, "platforms", "Console", item.folder, "game.yaml")
		writeFoldFile(t, path, readFoldFile(t, path)+"\nfold: {content_key: "+item.key+"}\n")
	}
	result, err := Fold(Options{DataRoot: root, Apply: true})
	if err != nil || result.AppliedGroupCount != 0 || result.HeldGroupCount != 1 || !strings.Contains(result.Groups[0].HoldReason, "reviewed content") {
		t.Fatal(err, result)
	}
	group := reviewedFixture(t, root, "Console", "Main", "Expanded")
	group.PrimaryCandidates = []string{"Main"}
	before := catalogBytes(t, root)
	if _, err := Fold(Options{DataRoot: root, ReviewPlan: writeReviewFixture(t, root, group), Apply: true}); err == nil {
		t.Fatal("review plan crossed content keys")
	}
	if !reflect.DeepEqual(before, catalogBytes(t, root)) {
		t.Fatal("rejected fold changed content")
	}
	path := filepath.Join(root, "platforms", "Console", "Expanded", "game.yaml")
	writeFoldFile(t, path, strings.ReplaceAll(readFoldFile(t, path), "expanded-key", "main-key"))
	result, err = Fold(Options{DataRoot: root, Apply: true})
	if err != nil || result.AppliedGroupCount != 1 {
		t.Fatal(err, result)
	}
	var doc map[string]any
	if err = yaml.Unmarshal([]byte(readFoldFile(t, filepath.Join(root, "platforms", "Console", result.Groups[0].Keep.Folder, "game.yaml"))), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["fold"].(map[string]any)["content_key"] != "main-key" {
		t.Fatal("curated identity lost")
	}
}

func TestReviewedSplitSharesMediaAndRoutesUnscopedSource(t *testing.T) {
	root := t.TempDir()
	plan, _ := splitFixture(t, root)
	group := &plan.Groups[0]
	path := filepath.Join(root, "platforms", "Console", "Orbit", "game.yaml")
	game := readFoldFile(t, path) + "  archive:\n    manual: retain\n    matches:\n      - title: Demo\n        custom: {nested: keep}\n"
	writeFoldFile(t, path, game)
	group.GameSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(game)))
	group.SourceRoutes = []SplitSourceRoute{{Source: "archive", MatchIndex: 0, Folder: "Orbit Demo"}}
	group.Files[0].Targets = append(group.Files[0].Targets, SplitTarget{Folder: "Orbit Demo", Path: "media/shared.jpg"})
	result, err := Split(SplitOptions{DataRoot: root, ReviewPlan: writeSplitFixture(t, root, plan), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if readFoldFile(t, filepath.Join(root, "platforms", "Console", "Orbit Demo", "media", "shared.jpg")) != "original artwork" {
		t.Fatal("shared media changed")
	}
	demo := readContentMap(t, filepath.Join(root, "platforms", "Console", "Orbit Demo", "game.yaml"))
	archive := demo["sources"].(map[string]any)["archive"].(map[string]any)
	original := readContentMap(t, filepath.Join(result.Backup, "Orbit", "game.yaml"))
	if !reflect.DeepEqual(archive, original["sources"].(map[string]any)["archive"]) {
		t.Fatal("routed source payload changed")
	}
	wantFP := readContentMap(t, filepath.Join(result.Backup, "Orbit", "fingerprints.yaml"))["items"].([]any)
	gotFP := map[string]any{}
	for _, folder := range []string{"Orbit", "Orbit Demo"} {
		doc := readContentMap(t, filepath.Join(root, "platforms", "Console", folder, "fingerprints.yaml"))
		for _, item := range doc["items"].([]any) {
			gotFP[item.(map[string]any)["release"].(string)] = item
		}
	}
	for _, item := range wantFP {
		if !reflect.DeepEqual(gotFP[item.(map[string]any)["release"].(string)], item) {
			t.Fatal("complete fingerprint payload changed")
		}
	}
}

func TestReviewedSplitProjectsMultipleReleaseScopes(t *testing.T) {
	root := t.TempDir()
	plan, _ := splitFixture(t, root)
	group := &plan.Groups[0]
	path := filepath.Join(root, "platforms", "Console", "Orbit", "game.yaml")
	game := readFoldFile(t, path) + "  multi:\n    custom_stanza: keep\n    matches:\n      - releases: [Orbit (USA), Orbit (Japan) (Demo)]\n        custom: {nested: keep}\n"
	writeFoldFile(t, path, game)
	group.GameSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(game)))
	result, err := Split(SplitOptions{DataRoot: root, ReviewPlan: writeSplitFixture(t, root, plan), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	original := readContentMap(t, filepath.Join(result.Backup, "Orbit", "game.yaml"))["sources"].(map[string]any)["multi"].(map[string]any)
	for _, part := range group.Parts {
		stanza := readContentMap(t, filepath.Join(root, "platforms", "Console", part.Folder, "game.yaml"))["sources"].(map[string]any)["multi"].(map[string]any)
		if stanza["custom_stanza"] != original["custom_stanza"] {
			t.Fatal("source attributes changed")
		}
		match := stanza["matches"].([]any)[0].(map[string]any)
		old := original["matches"].([]any)[0].(map[string]any)
		if !reflect.DeepEqual(match["custom"], old["custom"]) || !reflect.DeepEqual(match["releases"], []any{part.Releases[0]}) {
			t.Fatal("multi-scope payload lost", match)
		}
	}
}

func TestReviewedSplitPreservesStructuredProviderReleasePayloads(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			root := t.TempDir()
			plan, _ := splitFixture(t, root)
			group := &plan.Groups[0]
			path := filepath.Join(root, "platforms", "Console", "Orbit", "game.yaml")
			game := readFoldFile(t, path) + "  openvgdb:\n    matches:\n      - title: Orbit\n        custom: {untouched: true}\n        releases:\n          - release: Orbit (USA)\n            openvgdb_rom: {sha1: original-hash, nested: [one, two]}\n          - release: Orbit (Japan) (Demo)\n            region: Japan\n            openvgdb_rom: {file_name: exact-original.bin, crc32: abcdef01}\n"
			if invalid {
				game = strings.ReplaceAll(game, "release: Orbit (USA)", "release: Missing")
			}
			writeFoldFile(t, path, game)
			group.GameSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(game)))
			before := catalogBytes(t, root)
			result, err := Split(SplitOptions{DataRoot: root, ReviewPlan: writeSplitFixture(t, root, plan), Apply: true})
			if invalid {
				if err == nil || !reflect.DeepEqual(before, catalogBytes(t, root)) {
					t.Fatal("invalid nested scope mutated catalog", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			original := readContentMap(t, filepath.Join(result.Backup, "Orbit", "game.yaml"))["sources"].(map[string]any)["openvgdb"].(map[string]any)["matches"].([]any)[0].(map[string]any)
			for i, part := range group.Parts {
				got := readContentMap(t, filepath.Join(root, "platforms", "Console", part.Folder, "game.yaml"))["sources"].(map[string]any)["openvgdb"].(map[string]any)["matches"].([]any)[0].(map[string]any)
				if !reflect.DeepEqual(got["custom"], original["custom"]) || !reflect.DeepEqual(got["releases"], []any{original["releases"].([]any)[i]}) {
					t.Fatal("nested provider payload changed", got, original)
				}
			}
		})
	}
}
