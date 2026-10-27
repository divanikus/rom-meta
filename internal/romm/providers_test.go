package romm

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"rommeta/internal/exporter"
	"rommeta/internal/providerrefs"
)

func providerFixture(t *testing.T, count int) (*fakeAPI, *Client, *Plan) {
	t.Helper()
	api, client, p := buildFixture(t, count)
	dir := filepath.Join(p.DataRoot, "platforms", "Example", "Game")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	m := md5.Sum([]byte("synthetic ROM 0"))
	md5text := hex.EncodeToString(m[:])
	doc := map[string]any{"releases": map[string]any{"Example (USA)": map[string]any{}}, "sources": map[string]any{
		"igdb":          map[string]any{"matches": []any{map[string]any{"release": "Example (USA)", "igdb_id": 123, "slug": "synthetic-game", "descriptions": map[string]any{"en": "First source description", "de": "Andere Beschreibung"}, "release_date": "1993-07-02", "developer": "Studio", "genres": []string{"Action"}, "alternate_titles": []string{"Alternate"}, "rating": "71.83"}}},
		"screenscraper": map[string]any{"matches": []any{map[string]any{"release": "Example (USA)", "screenscraper_id": "321", "release_date": "1994", "rating": "14", "players": "1-2"}}},
		"launchbox":     map[string]any{"matches": []any{map[string]any{"release": "Example (USA)", "database_id": "456", "community_rating": "2.9", "max_players": "2"}}},
		"hasheous":      map[string]any{"matches": []any{map[string]any{"release": "Example (USA)", "hasheous_id": "789", "igdb_id": 123, "retroachievements_id": 222, "thegamesdb_id": 333, "matched_query": "md5:" + md5text[:8], "signatures": []string{"TOSEC", "FBNeo"}}}},
	}}
	b, _ := yaml.Marshal(doc)
	if e := os.WriteFile(filepath.Join(dir, "game.yaml"), b, 0600); e != nil {
		t.Fatal(e)
	}
	var items []any
	for _, entry := range p.Entries {
		items = append(items, map[string]any{"id": "no-intro.0001", "release": entry.Source.Release, "source": map[string]any{"kind": "dat", "name": "No-Intro"}, "file": map[string]any{"size": entry.Source.Size, "sha1": entry.Source.SHA1}})
	}
	b, _ = yaml.Marshal(map[string]any{"items": items})
	if e := os.WriteFile(filepath.Join(dir, "fingerprints.yaml"), b, 0600); e != nil {
		t.Fatal(e)
	}
	for _, row := range api.rows {
		row["summary"] = raw("Owner text")
		row["path_cover_large"] = raw("existing/big.png")
		row["path_cover_small"] = raw("existing/small.png")
		row["manual_metadata"] = raw(map[string]any{"first_release_date": 741571200000, "genres": []string{"Owner"}})
	}
	// Build captured the earlier baseline: refresh only because the synthetic owner
	// setup precedes provider planning, just as an actual fresh plan does.
	for i := range p.Entries {
		p.Entries[i].Before, _ = client.Detail(context.Background(), p.Entries[i].ROM.ID)
		p.Entries[i].BeforeHash = snapshotHash(p.Entries[i].Before)
	}
	if e := AddProviders(context.Background(), client, p); e != nil {
		t.Fatal(e)
	}
	return api, client, p
}
func TestProviderNormalizationCalendarRatingsAndUnknowns(t *testing.T) {
	ig := normalizeProvider("igdb", map[string]any{"release_date": "1993-07-02", "rating": "71.83", "alternate_titles": []any{"Alias", 123}, "platforms": []any{"Game Gear"}})
	want := time.Date(1993, 7, 2, 0, 0, 0, 0, time.UTC).Unix()
	if ig["first_release_date"] != want || ig["total_rating"] != "71.83" {
		t.Fatal(ig)
	}
	if _, ok := ig["player_count"]; ok {
		t.Fatal("invented player count")
	}
	if _, ok := ig["platforms"]; ok {
		t.Fatal("invented structured platform IDs")
	}
	ss := normalizeProvider("screenscraper", map[string]any{"release_date": "1994", "rating": "14", "players": "1-2"})
	if ss["ss_score"] != "7" || ss["player_count"] != "1-2" {
		t.Fatal(ss)
	}
	if _, ok := ss["first_release_date"]; ok {
		t.Fatal("partial year invented a day")
	}
	lb := normalizeProvider("launchbox", map[string]any{"community_rating": "4.25", "max_players": "2"})
	if lb["community_rating"] != 4.25 || lb["max_players"] != int64(2) {
		t.Fatal(lb)
	}
	for _, bad := range []string{"-1", "101", "NaN", "Inf"} {
		if _, ok := normalizeProvider("igdb", map[string]any{"rating": bad})["total_rating"]; ok {
			t.Fatal(bad)
		}
	}
	if _, ok := normalizeProvider("screenscraper", map[string]any{"players": "2-1", "rating": "21"})["player_count"]; ok {
		t.Fatal("invalid range")
	}
}

// Installed RomM 5.4.0 PostgreSQL generated_first_release_date keeps manual
// milliseconds, but multiplies IGDB/SS/LaunchBox provider seconds by 1000.
func TestProviderDatesUseSecondsWithoutManualFallbackAndKeepManualMilliseconds(t *testing.T) {
	date := time.Date(1995, 12, 15, 0, 0, 0, 0, time.UTC)
	record := exporter.Record{Status: "matched", MatchedBy: "game-name", Size: 4, SHA1: strings.Repeat("a", 40), MetadataRelease: "Game (USA)"}
	proof := VerificationProof{Size: 4, SHA1: record.SHA1, SHA256: strings.Repeat("b", 64), MD5: strings.Repeat("c", 32), CRC32: "01234567"}
	doc := []byte("releases: {Game (USA): {}}\nsources:\n  igdb:\n    matches: [{igdb_id: 123, release_date: '1995-12-15'}]\n  screenscraper:\n    matches: [{screenscraper_id: 321, release_date: '1995-12-15'}]\n  launchbox:\n    matches: [{database_id: 456, release_date: '1995-12-15'}]\n")
	before := map[string]json.RawMessage{"manual_metadata": raw(map[string]any{})}
	evidence, form, err := ProjectSnapshot(record, doc, []byte("items: []\n"), proof, before)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := form["raw_manual_metadata"]; ok {
		t.Fatal("test requires actual native provider date fallback", form)
	}
	for _, provider := range []string{"igdb", "screenscraper", "launchbox"} {
		seconds, ok := evidence.Slots[provider].Metadata["first_release_date"].(int64)
		if !ok || seconds != date.Unix() || seconds*1000 != date.UnixMilli() {
			t.Fatal(provider, evidence.Slots[provider])
		}
		var metadata struct {
			Date int64 `json:"first_release_date"`
		}
		if err := json.Unmarshal([]byte(form["raw_"+nativeMetadata[provider]]), &metadata); err != nil || metadata.Date != date.Unix() {
			t.Fatal(provider, form, err)
		}
	}
	record.Fields = map[string]exporter.Value{"release_date": {Value: "1995-12-15", Source: "igdb"}}
	_, form, err = ProjectSnapshot(record, doc, []byte("items: []\n"), proof, before)
	if err != nil {
		t.Fatal(err)
	}
	var manual struct {
		Date int64 `json:"first_release_date"`
	}
	if err = json.Unmarshal([]byte(form["raw_manual_metadata"]), &manual); err != nil || manual.Date != date.UnixMilli() {
		t.Fatal("manual millisecond contract changed", form, err)
	}
}

func TestProviderIdentityGroupingIsCardScopedButFlagsAreExact(t *testing.T) {
	_, _, p := providerFixture(t, 1)
	e := p.Entries[0]
	if !e.Providers.Verification.Flags["nointro_match"] || !e.Providers.Verification.Flags["fbneo_match"] {
		t.Fatal(e.Providers)
	}
	// Same explicit card gets shared identity, while a beta has its own DAT proof.
	var doc map[string]any
	b, _ := os.ReadFile(e.Providers.Files[0].Path)
	_ = yaml.Unmarshal(b, &doc)
	doc["releases"].(map[string]any)["Example (USA) (Beta)"] = map[string]any{}
	b, _ = yaml.Marshal(doc)
	_ = os.WriteFile(e.Providers.Files[0].Path, b, 0600)
	var fps map[string]any
	b, _ = os.ReadFile(e.Providers.Files[1].Path)
	_ = yaml.Unmarshal(b, &fps)
	fps["items"].([]any)[0].(map[string]any)["release"] = "Example (USA) (Beta)"
	b, _ = yaml.Marshal(fps)
	_ = os.WriteFile(e.Providers.Files[1].Path, b, 0600)
	e.Source.Release = "Example (USA) (Beta)"
	beta, err := buildProviderEvidence(p, e)
	if err != nil {
		t.Fatal(err)
	}
	if beta.Slots["igdb"].ID != 123 || beta.Slots["hasheous"].ID != 789 || beta.Slots["screenscraper"].Scope != "card-donor" {
		t.Fatal(beta)
	}
	if !beta.Verification.Flags["nointro_match"] || beta.Verification.Flags["fbneo_match"] || beta.Verification.Flags["tosec_match"] || beta.Verification.HasheousMatchIndex != nil {
		t.Fatal("retail signature inherited by beta", beta.Verification)
	}
	if beta.References["retroachievements"] != "222" {
		t.Fatal(beta.References)
	}
}
func TestProviderConsensusAndFillOnlyPreserveUnknownFields(t *testing.T) {
	if _, _, id, scope, err := chooseMatch([]map[string]any{{"igdb_id": 1, "release": "Other"}, {"igdb_id": 2, "release": "R"}}, "igdb_id", "R"); err != nil || id != 2 || scope != "exact-release" {
		t.Fatal("exact release did not precede donor identity", id, scope, err)
	}
	if _, _, _, _, err := chooseMatch([]map[string]any{{"igdb_id": 1}, {"igdb_id": 2}}, "igdb_id", "R"); err == nil {
		t.Fatal("conflicting card IDs accepted")
	} else {
		var conflict *IdentityConflict
		if !errors.As(err, &conflict) {
			t.Fatal("identity conflict lost its type", err)
		}
	}
	entry := Entry{Before: map[string]json.RawMessage{"igdb_id": raw(123), "igdb_metadata": raw(map[string]any{"total_rating": "99", "custom_extra": "owner", "genres": []string{}}), "name": raw("Owner"), "summary": raw("Owner prose")}}
	evidence := &ProviderEvidence{Slots: map[string]ProviderSlot{"igdb": {ID: 123, Metadata: map[string]any{"total_rating": "71.83", "genres": []string{"Action"}}}}}
	form := providerForm(entry, evidence)
	var payload map[string]any
	_ = json.Unmarshal([]byte(form["raw_igdb_metadata"]), &payload)
	if payload["total_rating"] != "99" || payload["custom_extra"] != "owner" || len(payload["genres"].([]any)) != 1 {
		t.Fatal(form)
	}
	entry.Before["igdb_id"] = raw(999)
	if len(providerForm(entry, evidence)) != 0 {
		t.Fatal("overwrote different existing identity")
	}
	entry.Before["igdb_id"] = raw(123)
	entry.Before["igdb_metadata"] = raw(map[string]any{"box2d_url": "https://example.invalid/cover"})
	if len(providerForm(entry, evidence)) != 0 {
		t.Fatal("resubmitted remote media")
	}
}

func TestAmbiguousProviderDoesNotDiscardIndependentMetadata(t *testing.T) {
	_, _, plan := providerFixture(t, 1)
	entry := plan.Entries[0]
	path := entry.Providers.Files[0].Path
	b, _ := os.ReadFile(path)
	var doc providerDocument
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	lb := doc.Sources["launchbox"]
	lb.Matches[0]["release"] = entry.Source.Release
	lb.Matches = append(lb.Matches, map[string]any{"database_id": 999, "release": entry.Source.Release})
	doc.Sources["launchbox"] = lb
	b, _ = yaml.Marshal(doc)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	e, err := buildProviderEvidence(plan, entry)
	if err != nil || e.Slots["igdb"].ID != 123 || e.Slots["hasheous"].ID != 789 || e.Slots["launchbox"].ID != 0 || len(e.Conflicts) == 0 || !e.Verification.Flags["nointro_match"] {
		t.Fatal("one ambiguous provider discarded independent data", e, err)
	}
}
func TestProviderPlanApplyAndNoopPreserveOwnerAndMedia(t *testing.T) {
	api, client, p := providerFixture(t, 1)
	if p.CoverBytes != 0 || p.Entries[0].Cover != nil {
		t.Fatal("provider plan copies media")
	}
	if strings.Contains(fmt.Sprint(p.Entries[0].Form), "ra_id") {
		t.Fatal("RA lookup requested")
	}
	receipt := filepath.Join(t.TempDir(), "receipt.json")
	if e := Apply(context.Background(), client, p, receipt, nil, false); e != nil {
		t.Fatal(e)
	}
	if api.put != 1 || api.badField {
		t.Fatal(api.put)
	}
	e := p.Entries[0]
	e.Before, _ = client.Detail(context.Background(), e.ROM.ID)
	e.BeforeHash = snapshotHash(e.Before)
	e.Providers = nil
	p.Entries[0] = e
	p.Operation = ""
	if err := AddProviders(context.Background(), client, p); err != nil {
		t.Fatal(err)
	}
	if len(p.Entries[0].Form) != 0 {
		t.Fatal("repeat is not no-op", p.Entries[0].Form)
	}
	if err := Preflight(context.Background(), client, p); err != nil {
		t.Fatal(err)
	}
}
func TestProviderWholeBatchPreflightBlocksSourceAndRuntimeChanges(t *testing.T) {
	for _, mode := range []string{"source", "runtime", "native"} {
		t.Run(mode, func(t *testing.T) {
			api, client, p := providerFixture(t, 2)
			switch mode {
			case "source":
				f := p.Entries[1].Providers.Files[1].Path
				fhandle, _ := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0600)
				_, _ = fhandle.WriteString("\n# modified\n")
				_ = fhandle.Close()
			case "runtime":
				api.enabledProvider = "IGDB_API_ENABLED"
			case "native":
				api.rows[2]["igdb_metadata"] = raw(map[string]string{"genres": "changed"})
			}
			if err := Apply(context.Background(), client, p, filepath.Join(t.TempDir(), "receipt.json"), nil, false); err == nil {
				t.Fatal("unsafe apply accepted")
			}
			if api.put != 0 {
				t.Fatal("wrote before whole batch preflight")
			}
		})
	}
}

func TestProviderResponseRetainsResourceIdentityAcrossNativeCacheBusters(t *testing.T) {
	before := beforeRow(1, "Game.zip", 123)
	before["path_cover_small"] = raw("/assets/resources/cover/small.png?ts=old&format=png")
	before["path_cover_large"] = raw("/assets/resources/cover/big.png?ts=old")
	e := Entry{Before: before, Providers: &ProviderEvidence{}, Form: map[string]string{}}
	after := map[string]json.RawMessage{}
	for k, v := range before {
		after[k] = v
	}
	after["path_cover_small"] = raw("/assets/resources/cover/small.png?format=png&ts=new")
	after["path_cover_large"] = raw("/assets/resources/cover/big.png?ts=new")
	if err := verifyProviderResponse(e, after); err != nil {
		t.Fatal("native revision mistaken for media replacement", err)
	}
	for _, changed := range []string{"/assets/resources/cover/other.png?ts=new&format=png", "/assets/resources/cover/small.png?ts=new&format=webp", "https://other.invalid/assets/resources/cover/small.png?ts=new&format=png"} {
		after["path_cover_small"] = raw(changed)
		if err := verifyProviderResponse(e, after); err == nil {
			t.Fatal("actual resource change accepted", changed)
		}
	}
}

func TestIGDBRatingCountAndSourceTextAreNotNativeInventedFields(t *testing.T) {
	for _, value := range []any{int64(0), "17"} {
		metadata := normalizeProvider("igdb", map[string]any{"rating_count": value, "slug": "correct-game", "descriptions": map[string]any{"en": "Provider description"}})
		if _, ok := metadata["total_rating_count"]; !ok {
			t.Fatal(metadata)
		}
		for _, key := range []string{"slug", "descriptions", "summary"} {
			if _, ok := metadata[key]; ok {
				t.Fatal("unsupported source field sent natively", key)
			}
		}
	}
	for _, bad := range []any{-1, "17.5", "NaN", "2147483648", true} {
		if _, ok := normalizeProvider("igdb", map[string]any{"rating_count": bad})["total_rating_count"]; ok {
			t.Fatal("invalid native count", bad)
		}
	}
}

func TestSnapshotProjectionReusesFileBackedNormalizationAndLosslessCRC(t *testing.T) {
	_, _, plan := providerFixture(t, 1)
	entry := plan.Entries[0]
	game, err := os.ReadFile(entry.Providers.Files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	fingerprints, err := os.ReadFile(entry.Providers.Files[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	projected, form, err := ProjectSnapshot(entry.Source, game, fingerprints, entry.Providers.Verification, entry.Before)
	if err != nil {
		t.Fatal(err)
	}
	expected := *entry.Providers
	expected.Files = nil
	// Transport limits differ between public API and native-runtime consumers;
	// native provider values/proof and fill-only form must remain identical.
	projected.Unsupported = nil
	expected.Unsupported = nil
	left, _ := json.Marshal(projected)
	right, _ := json.Marshal(expected)
	if !sameJSON(left, right) || !reflect.DeepEqual(form, entry.Form) {
		t.Fatal("projection drifted from tested mapper")
	}
	record := exporter.Record{Status: "matched", MatchedBy: "hash", Size: 4, SHA1: strings.Repeat("a", 40), Release: "Game (USA)"}
	proof := VerificationProof{Size: 4, SHA1: record.SHA1, SHA256: strings.Repeat("b", 64), MD5: strings.Repeat("c", 32), CRC32: "01234567", Flags: map[string]bool{"tosec_match": true}}
	yamlGame := []byte("releases:\n  Game (USA): {}\n")
	yamlFP := []byte("items:\n  - id: no-intro.1\n    release: Game (USA)\n    source: {kind: dat, name: No-Intro}\n    file:\n      size: 4\n      sha1: " + record.SHA1 + "\n      crc32: 01234567\n")
	evidence, _, err := ProjectSnapshot(record, yamlGame, yamlFP, proof, map[string]json.RawMessage{})
	if err != nil || !evidence.Verification.Flags["nointro_match"] || evidence.Verification.Flags["tosec_match"] {
		t.Fatal("lexical CRC/independent proof failed", err, evidence)
	}
}

func TestGameOnlyBindingNeverInheritsDonorVerification(t *testing.T) {
	record := exporter.Record{Status: "matched", MatchedBy: "game-name", Size: 4, SHA1: strings.Repeat("a", 40), MetadataRelease: "Game (USA)"}
	proof := VerificationProof{Size: 4, SHA1: record.SHA1, SHA256: strings.Repeat("b", 64), MD5: strings.Repeat("c", 32), CRC32: "01234567"}
	doc := []byte("releases: {Game (USA): {}}\nsources:\n  hasheous:\n    matches:\n      - hasheous_id: 456\n        igdb_id: 123\n        matched_query: 'md5:cccccccc'\n        signatures: [NoIntros, RetroAchievements]\n")
	fps := []byte("items: []\n")
	evidence, form, err := ProjectSnapshot(record, doc, fps, proof, map[string]json.RawMessage{})
	if err != nil || form["igdb_id"] != "123" {
		t.Fatal(evidence, form, err)
	}
	if len(evidence.Verification.Flags) != 0 || evidence.Verification.Authority != "" || evidence.Verification.HasheousMatchIndex != nil {
		t.Fatal("donor verification inherited", evidence.Verification)
	}
}

func TestDirectProviderIdentityWinsForeignReferenceWithoutLosingExactProof(t *testing.T) {
	_, _, plan := providerFixture(t, 1)
	entry := plan.Entries[0]
	path := entry.Providers.Files[0].Path
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc providerDocument
	if err = yaml.Unmarshal(content, &doc); err != nil {
		t.Fatal(err)
	}
	doc.Sources["hasheous"].Matches[0]["igdb_id"] = 999
	write := func() {
		t.Helper()
		body, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	evidence, err := buildProviderEvidence(plan, entry)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Slots["igdb"].ID != 123 || evidence.Slots["hasheous"].ID != 789 || evidence.References["hasheous.igdb_id"] != "999" {
		t.Fatal(evidence)
	}
	if !evidence.Verification.Flags["nointro_match"] || !evidence.Verification.Flags["fbneo_match"] || evidence.Verification.HasheousMatchIndex == nil {
		t.Fatal("foreign ID disagreement changed independent exact proof", evidence.Verification)
	}
	if !strings.Contains(strings.Join(evidence.Unsupported, " "), "reference 999 conflicts with direct igdb ID 123") {
		t.Fatal("conflict not reported", evidence.Unsupported)
	}
	form := providerForm(entry, evidence)
	if form["igdb_id"] != "123" {
		t.Fatal("indirect ID replaced direct provider", form)
	}
	// Ambiguous foreign references do not discard direct identity or dump proof.
	more := doc.Sources["hasheous"]
	more.Matches = append(more.Matches, map[string]any{"hasheous_id": 789, "igdb_id": 998, "release": entry.Source.Release})
	doc.Sources["hasheous"] = more
	write()
	evidence, err = buildProviderEvidence(plan, entry)
	if err != nil || evidence.Slots["igdb"].ID != 123 || len(evidence.Conflicts) == 0 || !evidence.Verification.Flags["nointro_match"] {
		t.Fatal("ambiguity discarded independent metadata/proof", evidence, err)
	}
}

func TestReviewedIGDBReferenceDecisionsPreservePayloadProof(t *testing.T) {
	for _, action := range []string{"suppress", "replace"} {
		t.Run(action, func(t *testing.T) {
			_, _, plan := providerFixture(t, 1)
			entry := plan.Entries[0]
			path := entry.Providers.Files[0].Path
			b, _ := os.ReadFile(path)
			var doc providerDocument
			_ = yaml.Unmarshal(b, &doc)
			decision := providerrefs.IGDBDecision{Source: "hasheous", FromID: 123, Action: action, Reason: "Verified different product/port", Evidence: []string{"https://www.igdb.com/games/actual-port"}}
			if action == "replace" {
				decision.ToID = 124
				direct := doc.Sources["igdb"]
				direct.Matches = []map[string]any{{"igdb_id": 124, "slug": "actual-port", "capture": "identity-links"}}
				doc.Sources["igdb"] = direct
			} else {
				delete(doc.Sources, "igdb")
			}
			doc.Manual.ProviderReferences.IGDB = []providerrefs.IGDBDecision{decision}
			b, _ = yaml.Marshal(doc)
			_ = os.WriteFile(path, b, 0600)
			evidence, err := buildProviderEvidence(plan, entry)
			if err != nil {
				t.Fatal(err)
			}
			if len(evidence.ReferenceReviews) != 1 || evidence.References["hasheous.igdb_id"] != "123" || evidence.Slots["hasheous"].ID != 789 || !evidence.Verification.Flags["fbneo_match"] || !evidence.Verification.Flags["nointro_match"] {
				t.Fatal(evidence)
			}
			if action == "suppress" {
				if _, ok := evidence.Slots["igdb"]; ok {
					t.Fatal("suppressed foreign ID re-injected")
				}
			} else if evidence.Slots["igdb"].ID != 124 || len(evidence.Slots["igdb"].Metadata) != 0 {
				t.Fatal("replacement invented provider metadata", evidence)
			}
			// Ambiguous references are omitted even when one candidate has a review.
			hs := doc.Sources["hasheous"]
			hs.Matches = append(hs.Matches, map[string]any{"hasheous_id": 789, "igdb_id": 999, "release": entry.Source.Release})
			doc.Sources["hasheous"] = hs
			b, _ = yaml.Marshal(doc)
			_ = os.WriteFile(path, b, 0600)
			partial, err := buildProviderEvidence(plan, entry)
			if err != nil || len(partial.Conflicts) == 0 || !partial.Verification.Flags["nointro_match"] {
				t.Fatal("ambiguity not reported independently", partial, err)
			}
		})
	}
}

func TestFingerprintSizesAcceptIntegersAndDecimalStrings(t *testing.T) {
	for _, size := range []string{"2048", `"2048"`, `"002048"`, "0x800"} {
		t.Run(size, func(t *testing.T) {
			var doc fingerprintDocument
			if err := yaml.Unmarshal([]byte("items:\n  - file: {size: "+size+"}\n"), &doc); err != nil {
				t.Fatal(err)
			}
			if len(doc.Items) != 1 || doc.Items[0].File.Size != 2048 {
				t.Fatal(doc)
			}
		})
	}
	for _, size := range []string{"-1", `"-1"`, "1.5", `"1.5"`, "true", `"bad"`, `" 2048 "`, "9223372036854775808", `"9223372036854775808"`, "[]", "{}"} {
		t.Run("invalid/"+size, func(t *testing.T) {
			var doc fingerprintDocument
			if err := yaml.Unmarshal([]byte("items:\n  - file: {size: "+size+"}\n"), &doc); err == nil {
				t.Fatalf("accepted invalid fingerprint size %s", size)
			}
		})
	}
}

func TestQuotedFingerprintSizeKeepsExactVerification(t *testing.T) {
	record := exporter.Record{Status: "matched", MatchedBy: "hash", Size: 2048, SHA1: strings.Repeat("a", 40), Release: "Game (USA)"}
	proof := VerificationProof{Size: record.Size, SHA1: record.SHA1, SHA256: strings.Repeat("b", 64), MD5: strings.Repeat("c", 32), CRC32: "01234567"}
	game := []byte("releases: {Game (USA): {}}\n")
	for _, size := range []int{2048, 2049} {
		fps := []byte(fmt.Sprintf("items:\n  - id: no-intro.1\n    release: Game (USA)\n    source: {kind: dat, name: No-Intro}\n    file: {size: '%d', sha1: '%s', crc32: '01234567'}\n", size, record.SHA1))
		evidence, _, err := ProjectSnapshot(record, game, fps, proof, nil)
		if err != nil {
			t.Fatal(err)
		}
		if evidence.Verification.Flags["nointro_match"] != (size == 2048) {
			t.Fatal("quoted size changed exact-match semantics", size, evidence.Verification)
		}
	}
}
