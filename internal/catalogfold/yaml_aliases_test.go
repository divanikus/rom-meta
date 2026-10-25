package catalogfold

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/catalogyaml"
)

func TestFoldPreservesDocumentLocalYAMLAliases(t *testing.T) {
	for _, mode := range []string{"automatic", "interactive", "reviewed"} {
		for _, packed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/packed=%v", mode, packed), func(t *testing.T) {
				root := t.TempDir()
				read := func(path string) string {
					t.Helper()
					data, err := catalogfs.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					return string(data)
				}
				platform := "Console"
				expectedMatches := map[string]map[string]any{}
				expectedReleases := map[string]any{}
				expectedFingerprints := map[string]any{}
				for i, region := range []string{"USA", "Japan"} {
					release := "Game (" + region + ")"
					language := []string{"en", "ja"}[i]
					writeFoldGame(t, root, platform, region, release, "Game", "100")
					body := fmt.Sprintf(`title: Game
default_release: %q
releases:
  %q:
    source: redump
    title: Game
    regions: &region [%q]
sources:
  launchbox:
    matches:
      - release: %q
        database_id: "100"
        title: Game
        releases: &scope [%q]
        regions: *region
        languages: &language [%q]
  screenscraper:
    matches:
      - release: %q
        game_id: "200"
        title: Game
        releases: *scope
        regions: *region
        languages: *language
        literal: "*scope"
        details: {observed_releases: *scope}
`, release, release, region, release, release, language, release)
					writeFoldFile(t, filepath.Join(root, "platforms", platform, region, "game.yaml"), body)
					var document map[string]any
					if err := yaml.Unmarshal([]byte(body), &document); err != nil {
						t.Fatal(err)
					}
					expectedReleases[release] = document["releases"].(map[string]any)[release]
					for source, raw := range document["sources"].(map[string]any) {
						if expectedMatches[source] == nil {
							expectedMatches[source] = map[string]any{}
						}
						expectedMatches[source][release] = raw.(map[string]any)["matches"].([]any)[0]
					}
					fingerprints := fmt.Sprintf("items:\n  - id: %q\n    release: %q\n    md5: &digest %q\n    original: {md5: *digest}\n", "redump."+release, release, strings.Repeat(fmt.Sprint(i), 32))
					writeFoldFile(t, filepath.Join(root, "platforms", platform, region, "fingerprints.yaml"), fingerprints)
					var original struct {
						Items []map[string]any `yaml:"items"`
					}
					if err := yaml.Unmarshal([]byte(fingerprints), &original); err != nil {
						t.Fatal(err)
					}
					expectedFingerprints[release] = original.Items[0]
				}
				if packed {
					catalogfstest.Pack(t, filepath.Join(root, "platforms", platform))
				}
				options := Options{DataRoot: root, Platform: platform}
				if mode == "reviewed" {
					options.Platform = ""
					group := ReviewedGroup{Platform: platform, Reason: "Alias preservation regression"}
					for _, folder := range []string{"USA", "Japan"} {
						path := filepath.Join(root, "platforms", platform, folder)
						group.Members = append(group.Members, ReviewedMember{Folder: folder, GameSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(read(filepath.Join(path, "game.yaml"))))), FingerprintsSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(read(filepath.Join(path, "fingerprints.yaml")))))})
					}
					options.ReviewPlan = writeReviewFixture(t, root, group)
				}
				beforeUSA := read(filepath.Join(root, "platforms", platform, "USA", "game.yaml"))
				beforeJapan := read(filepath.Join(root, "platforms", platform, "Japan", "game.yaml"))
				if _, err := Fold(options); err != nil {
					t.Fatal("preview:", err)
				}
				if read(filepath.Join(root, "platforms", platform, "USA", "game.yaml")) != beforeUSA || read(filepath.Join(root, "platforms", platform, "Japan", "game.yaml")) != beforeJapan {
					t.Fatal("preview changed inputs")
				}
				if mode == "interactive" {
					options.Review = func(ReviewPrompt) (ReviewDecision, error) { return ReviewApply, nil }
				} else {
					options.Apply = true
				}
				result, err := Fold(options)
				if err != nil {
					t.Fatal(err)
				}
				if result.AppliedGroupCount != 1 || result.MergedGameCount != 1 {
					t.Fatalf("fold did not merge: %+v", result)
				}
				kept := filepath.Join(root, "platforms", platform, result.Groups[0].Keep.Folder)
				gameBytes := []byte(read(filepath.Join(kept, "game.yaml")))
				var merged map[string]any
				if err := yaml.Unmarshal(gameBytes, &merged); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(merged["releases"], expectedReleases) {
					t.Fatalf("release data changed: %#v", merged["releases"])
				}
				for source, wanted := range expectedMatches {
					matches := merged["sources"].(map[string]any)[source].(map[string]any)["matches"].([]any)
					if len(matches) != len(wanted) {
						t.Fatalf("lost %s matches: %d", source, len(matches))
					}
					for _, raw := range matches {
						match := raw.(map[string]any)
						release := match["release"].(string)
						if !reflect.DeepEqual(match, wanted[release]) {
							t.Errorf("%s/%s changed: got %#v want %#v", source, release, match, wanted[release])
						}
					}
				}
				fingerprintBytes := []byte(read(filepath.Join(kept, "fingerprints.yaml")))
				var mergedFP struct {
					Items []map[string]any `yaml:"items"`
				}
				if err := yaml.Unmarshal(fingerprintBytes, &mergedFP); err != nil {
					t.Fatal(err)
				}
				if len(mergedFP.Items) != 2 {
					t.Fatal("fingerprints lost")
				}
				for _, item := range mergedFP.Items {
					if !reflect.DeepEqual(item, expectedFingerprints[item["release"].(string)]) {
						t.Fatalf("fingerprint changed: %#v", item)
					}
				}
				for _, contents := range [][]byte{gameBytes, fingerprintBytes} {
					var node yaml.Node
					if err := yaml.Unmarshal(contents, &node); err != nil {
						t.Fatal(err)
					}
					assertLiteralFoldYAML(t, &node)
				}
				if len(result.Backups) == 0 {
					t.Fatal("missing recovery backup")
				}
				if packed {
					if _, err := os.Stat(filepath.Join(kept, "game.yaml")); !os.IsNotExist(err) {
						t.Fatal("packed fold created loose YAML")
					}
				}
			})
		}
	}
}

func assertLiteralFoldYAML(t *testing.T, node *yaml.Node) {
	t.Helper()
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		t.Fatalf("fold output retains YAML reference %q", node.Value)
	}
	for _, child := range node.Content {
		assertLiteralFoldYAML(t, child)
	}
}

func TestLiteralFoldYAMLPreservesMergeKeysAndDoesNotMutateInput(t *testing.T) {
	input := []byte(`base: &base {release: 'Game (USA)', regions: [USA], platforms: [Console]}
second: &second {regions: [Japan], languages: [ja]}
combined:
  <<: [*base, *second]
  regions: [World]
  title: Game
  literal: '*base'
copy: *base
`)
	document, err := catalogyaml.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	before, err := yaml.Marshal(document.Root())
	if err != nil {
		t.Fatal(err)
	}
	literal, err := literalFoldYAML(document.Root())
	if err != nil {
		t.Fatal(err)
	}
	assertLiteralFoldYAML(t, literal)
	var expected, actual map[string]any
	if err = yaml.Unmarshal(input, &expected); err != nil {
		t.Fatal(err)
	}
	if err = literal.Decode(&actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("YAML merge values changed: got %#v want %#v", actual, expected)
	}
	after, err := yaml.Marshal(document.Root())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("literalization changed original tree")
	}
	// Literal copies must not share mutable values with their original anchor.
	copied := catalogyaml.Value(literal, "copy")
	catalogyaml.Value(copied, "release").Value = "Edited"
	if catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(literal, "base"), "release")) != "Game (USA)" {
		t.Fatal("alias expansion shares mutable target")
	}
}

func TestFoldRejectsRecursiveOrExcessiveAliasesBeforeMutation(t *testing.T) {
	var bomb strings.Builder
	bomb.WriteString("        a: &a [value, value, value, value, value, value, value, value, value, value]\n")
	for i := 1; i < 7; i++ {
		previous := string(rune('a' + i - 1))
		next := string(rune('a' + i))
		fmt.Fprintf(&bomb, "        %s: &%s [%s]\n", next, next, strings.TrimSuffix(strings.Repeat("*"+previous+", ", 10), ", "))
	}
	for _, mode := range []string{"automatic", "reviewed"} {
		for _, document := range []string{"game.yaml", "fingerprints.yaml"} {
			for name, extra := range map[string]string{"cycle": "        loop: &loop [*loop]\n", "expansion": bomb.String()} {
				t.Run(mode+"/"+document+"/"+name, func(t *testing.T) {
					root := t.TempDir()
					platform := "Console"
					for _, folder := range []string{"USA", "Japan"} {
						writeFoldGame(t, root, platform, folder, "Game ("+folder+")", "Game", "100")
					}
					path := filepath.Join(root, "platforms", platform, "USA", document)
					if document == "fingerprints.yaml" {
						extra = strings.ReplaceAll(extra, "        ", "    ")
					}
					contents := readFoldFile(t, path) + extra
					writeFoldFile(t, path, contents)
					options := Options{DataRoot: root, Platform: platform, Apply: true}
					if mode == "reviewed" {
						options.Platform = ""
						options.ReviewPlan = writeReviewFixture(t, root, reviewedFixture(t, root, platform, "USA", "Japan"))
					}
					result, err := Fold(options)
					if err == nil {
						t.Fatalf("unsafe references accepted: %+v", result)
					}
					if text := strings.ToLower(err.Error()); !strings.Contains(text, "alias") && !strings.Contains(text, "anchor") && !strings.Contains(text, "reference") {
						t.Fatalf("unexpected error: %v", err)
					}
					if readFoldFile(t, path) != contents {
						t.Fatal("failure changed source")
					}
					if _, err = os.Stat(filepath.Join(root, "platforms", platform, "Japan")); err != nil {
						t.Fatal("failure removed member")
					}
					if entries, _ := os.ReadDir(filepath.Join(root, "backups")); len(entries) != 0 {
						t.Fatal("invalid aliases reached publication")
					}
				})
			}
		}
	}
}
