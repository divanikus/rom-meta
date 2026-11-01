package exporter

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchBoxLiteralFileTags(t *testing.T) {
	for _, tc := range []struct {
		file, want string
		discs      int
	}{
		{"retail/Example (Japan) (2M).chd", "(Japan) (2M)", 0},
		{"translations/en/Example (Japan) [T-En by Paul_met v1.1] [Ultimate] [Bugfix by Another] [n].chd", "(Japan) [T-En by Paul_met v1.1] [Ultimate] [Bugfix by Another] [n]", 0},
		{"Example [opaque, With + Punctuation]  (Special).chd", "[opaque, With + Punctuation]  (Special)", 0},
		{"Example (USA) (Disc I) (Beta).chd", "(USA) (Beta) [2 discs]", 2},
		{"Example (USA) (Disc I) (Beta).chd", "(USA) (Disc I) (Beta)", 0},
		{"folder/Unlabelled.chd", "Unlabelled", 0},
		{"Example [unclosed.chd", "Example [unclosed", 0},
		{"Example [翻訳版 by 著者 v1.0].chd", "[翻訳版 by 著者 v1.0]", 0},
	} {
		t.Run(tc.file, func(t *testing.T) {
			r := Record{ROM: tc.file, Variant: "ignored-identity", VariantInfo: &VariantInfo{Kind: "translation", Languages: []string{"ru"}, Group: "Ignored group", Version: "99"}}
			if got := launchBoxFileLabel(r, tc.discs); got != tc.want {
				t.Fatalf("%q, want literal %q", got, tc.want)
			}
		})
	}
}

func TestLaunchBoxLiteralTagsKeepIdentityPrimaryAndStableCollisions(t *testing.T) {
	plan := &Plan{System: "saturn", ROMRoot: t.TempDir()}
	for _, file := range []string{"a/Example (Japan) [opaque].chd", "b/Example (Japan) [opaque].chd", "c/Example (Japan) [opaque] (1).chd", "Example (USA) (Rev 1).chd"} {
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
		if seen[v.Name] || strings.Contains(v.Name, "Example") || strings.Contains(v.Name, ".chd") {
			t.Fatal("duplicate or filename label", v)
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
