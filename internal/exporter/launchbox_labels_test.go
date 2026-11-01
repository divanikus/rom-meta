package exporter

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchBoxFilenameLabelsIgnoreVariantDescriptors(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"retail/Example (Japan) (2M).chd", "Example (Japan) (2M).chd"},
		{"translations/en/Example (Japan) [T-En by Example v1.1] [Ultimate] [n].chd", "Example (Japan) [T-En by Example v1.1] [Ultimate] [n].chd"},
		{"Example [opaque, With + Punctuation]  (Special).chd", "Example [opaque, With + Punctuation]  (Special).chd"},
		{"folder/Unlabelled.chd", "Unlabelled.chd"},
		{"Example [unclosed.chd", "Example [unclosed.chd"},
		{"Example [翻訳版 by 著者 v1.0].chd", "Example [翻訳版 by 著者 v1.0].chd"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			for _, status := range []string{"matched", "unmatched", "ambiguous"} {
				r := versionRecord(tc.file, "platforms/saturn/Example", "Different catalog title", "Japan", "ru", "translation")
				r.Status = status
				plan := &Plan{System: "saturn", ROMRoot: t.TempDir(), Records: []Record{r}}
				if err := ProjectTarget(plan, "launchbox"); err != nil {
					t.Fatal(err)
				}
				v := plan.LaunchBox.Groups[0].Versions[0]
				if v.Name != tc.want {
					t.Fatalf("%s: %q, want %q", status, v.Name, tc.want)
				}
			}
		})
	}
}

func TestLaunchBoxFilenamesKeepIdentityPrimaryAndStableCollisions(t *testing.T) {
	plan := &Plan{System: "saturn", ROMRoot: t.TempDir()}
	for _, file := range []string{"a/Example (Japan) [opaque].chd", "b/Example (Japan) [opaque].chd", "c/Different title (Japan) [opaque].chd", "Example (USA) (Rev 1).chd"} {
		plan.Records = append(plan.Records, versionRecord(file, "platforms/saturn/Example", "Example (Japan)", "Japan", "ja", "original"))
	}
	projection, err := buildLaunchBoxProjection(plan)
	if err != nil {
		t.Fatal(err)
	}
	group := projection.Groups[0]
	// The previous complete-descriptor/path order selected this launch too.
	if group.Primary != launchBoxVersionID(plan, plan.Records[3]) {
		t.Fatal("label changed primary", group)
	}
	before := map[string]string{}
	seen := map[string]bool{}
	for _, v := range group.Versions {
		r := plan.Records[v.Records[0]]
		if seen[v.Name] || strings.ContainsAny(v.Name, "/\\") || !strings.HasSuffix(v.Name, ".chd") && !strings.Contains(v.Name, ".chd (") {
			t.Fatal("duplicate label or missing filename", v)
		}
		if v.ID != launchBoxVersionID(plan, r) || v.Path != filepath.Join(plan.ROMRoot, filepath.FromSlash(r.ROM)) {
			t.Fatal("display changed identity", v)
		}
		seen[v.Name] = true
		before[v.ID] = v.Name
	}
	for i, j := 0, len(plan.Records)-1; i < j; i, j = i+1, j-1 {
		plan.Records[i], plan.Records[j] = plan.Records[j], plan.Records[i]
	}
	projection, err = buildLaunchBoxProjection(plan)
	if err != nil {
		t.Fatal(err)
	}
	after := map[string]string{}
	for _, v := range projection.Groups[0].Versions {
		after[v.ID] = v.Name
	}
	if !reflect.DeepEqual(before, after) || group.Primary != projection.Groups[0].Primary {
		t.Fatal("input order changed labels or primary", before, after)
	}
}
