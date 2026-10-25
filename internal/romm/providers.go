package romm

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/exporter"
	"rommeta/internal/providerrefs"
)

const providerImport = "offline-providers"

// ProviderEvidence binds native game identity to an explicit catalog card. Metadata
// may use a card donor; verification never inherits another release's signatures.
type ProviderEvidence struct {
	Conflicts        []ProviderConflict          `json:"conflicts,omitempty"`
	ReferenceReviews []providerrefs.IGDBDecision `json:"reference_reviews,omitempty"`
	Files            []FileState                 `json:"files"`
	Slots            map[string]ProviderSlot     `json:"slots"`
	Verification     VerificationProof           `json:"verification"`
	References       map[string]string           `json:"reference_only,omitempty"`
	Unsupported      []string                    `json:"unsupported"`
	SourceTexts      []SourceTextSnapshot        `json:"source_texts,omitempty"`
}
type ProviderConflict struct {
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
}
type ProviderSlot struct {
	ID            int64          `json:"id"`
	Source        string         `json:"source"`
	MatchIndex    int            `json:"match_index"`
	Scope         string         `json:"scope"`
	SourceRelease string         `json:"source_release,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	SourceDate    string         `json:"source_date,omitempty"`
	SourceSlug    string         `json:"source_slug,omitempty"`
	Preserved     string         `json:"preserved,omitempty"`
}

// SourceTextSnapshot preserves every collected provider/language alternative for
// review. RomM's provider schemas do not expose separate descriptions; these are
// catalog provenance, never disguised as native provider fields or user notes.
type SourceTextSnapshot struct {
	Source        string            `json:"source"`
	MatchIndex    int               `json:"match_index"`
	Scope         string            `json:"scope"`
	SourceRelease string            `json:"source_release,omitempty"`
	Descriptions  map[string]string `json:"descriptions"`
}

type VerificationProof struct {
	Components         []exporter.PayloadProof `json:"components,omitempty"`
	Authority          string                  `json:"authority,omitempty"`
	Fingerprint        string                  `json:"fingerprint,omitempty"`
	Size               int64                   `json:"payload_size"`
	SHA1               string                  `json:"payload_sha1"`
	MD5                string                  `json:"payload_md5"`
	SHA256             string                  `json:"payload_sha256"`
	CRC32              string                  `json:"payload_crc32"`
	HasheousMatchIndex *int                    `json:"exact_hasheous_match_index,omitempty"`
	HasheousQuery      string                  `json:"exact_hasheous_query,omitempty"`
	Flags              map[string]bool         `json:"flags"`
}
type providerDocument struct {
	Manual struct {
		ProviderReferences struct {
			IGDB []providerrefs.IGDBDecision `yaml:"igdb"`
		} `yaml:"provider_references"`
	} `yaml:"manual"`
	Releases map[string]any `yaml:"releases"`
	Sources  map[string]struct {
		Matches []map[string]any `yaml:"matches"`
	} `yaml:"sources"`
}
type fingerprintDocument struct {
	Items []struct {
		ID      string                                                           `yaml:"id"`
		Release string                                                           `yaml:"release"`
		Source  struct{ Kind, Name, System, DatFile, EntryName, EntryID string } `yaml:"source"`
		File    struct {
			Size                     int64 `yaml:"size"`
			SHA1, MD5, SHA256, CRC32 string
		} `yaml:"file"`
	} `yaml:"items"`
}

var providerNames = []string{"igdb", "screenscraper", "launchbox", "hasheous", "tgdb"}
var nativeID = map[string]string{"igdb": "igdb_id", "screenscraper": "ss_id", "launchbox": "launchbox_id", "hasheous": "hasheous_id", "tgdb": "tgdb_id"}
var nativeMetadata = map[string]string{"igdb": "igdb_metadata", "screenscraper": "ss_metadata", "launchbox": "launchbox_metadata", "hasheous": "hasheous_metadata"}
var sourceID = map[string]string{"igdb": "igdb_id", "screenscraper": "screenscraper_id", "launchbox": "database_id", "hasheous": "hasheous_id"}
var signatureFields = map[string]string{"TOSEC": "tosec_match", "MAMEArcade": "mame_arcade_match", "MAMEMess": "mame_mess_match", "NoIntros": "nointro_match", "Redump": "redump_match", "MAMERedump": "mame_redump_match", "WHDLoad": "whdload_match", "RetroAchievements": "ra_match", "FBNeo": "fbneo_match", "PureDOSDAT": "puredos_match"}

func textValue(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
func stringValues(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, a := range x {
			if s, ok := a.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		out = append(out, x...)
	case string:
		if strings.TrimSpace(x) != "" {
			out = append(out, strings.TrimSpace(x))
		}
	}
	return out
}
func positiveID(v any) int64 {
	n, e := strconv.ParseInt(textValue(v), 10, 64)
	if e != nil || n < 1 {
		return 0
	}
	return n
}
func sourceScope(m map[string]any, release string) string {
	if textValue(m["release"]) == release {
		return "exact-release"
	}
	for _, r := range stringValues(m["releases"]) {
		if r == release {
			return "listed-release"
		}
	}
	if textValue(m["release"]) == "" && len(stringValues(m["releases"])) == 0 {
		return "shared-card"
	}
	return "card-donor"
}

// IdentityConflict marks ambiguous catalog identities, never bad payload evidence.
// Batch consumers may explicitly report/skip these while strict callers still fail.
type IdentityConflict struct{ Reason string }

func (e *IdentityConflict) Error() string { return e.Reason }

func chooseMatch(matches []map[string]any, idKey, release string) (map[string]any, int, int64, string, error) {
	var id int64
	chosen := -1
	rank := 100
	for i, m := range matches {
		n := positiveID(m[idKey])
		if n == 0 {
			continue
		}
		scope := sourceScope(m, release)
		r := map[string]int{"exact-release": 0, "listed-release": 1, "shared-card": 2, "card-donor": 3}[scope]
		if r < rank {
			chosen = i
			rank = r
			id = n
		}
	}
	if chosen < 0 {
		return nil, -1, 0, "", nil
	}
	for _, m := range matches {
		r := map[string]int{"exact-release": 0, "listed-release": 1, "shared-card": 2, "card-donor": 3}[sourceScope(m, release)]
		if n := positiveID(m[idKey]); n != 0 && r == rank && n != id {
			return nil, 0, 0, "", &IdentityConflict{Reason: fmt.Sprintf("conflicting %s values at the same release scope", idKey)}
		}
	}
	return matches[chosen], chosen, id, sourceScope(matches[chosen], release), nil
}
func fullDate(s string) (int64, bool) {
	d, e := time.Parse("2006-01-02", s)
	// RomM provider JSON stores Unix seconds; its generated date multiplies by
	// 1000. Manual metadata has a separate millisecond contract.
	return d.Unix(), e == nil && len(s) == 10
}
func boundedNumber(v any, maximum float64) (float64, bool) {
	n, e := strconv.ParseFloat(textValue(v), 64)
	return n, e == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= maximum
}
func normalizeProvider(name string, m map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"genres", "franchises", "collections", "themes", "keywords", "game_modes", "player_perspectives"} {
		if (name == "launchbox" && k != "genres") || (name == "screenscraper" && (k == "collections" || k == "themes" || k == "keywords" || k == "player_perspectives")) {
			continue
		}
		if values := stringValues(m[k]); len(values) > 0 {
			out[k] = values
		}
	}
	for src, dst := range map[string]string{"developer": "developers", "publisher": "publishers"} {
		if values := stringValues(m[src]); len(values) > 0 {
			out[dst] = values
		}
	}
	var companies []string
	for _, key := range []string{"developers", "publishers"} {
		if v, ok := out[key].([]string); ok {
			for _, a := range v {
				found := false
				for _, b := range companies {
					found = found || a == b
				}
				if !found {
					companies = append(companies, a)
				}
			}
		}
	}
	if len(companies) > 0 {
		out["companies"] = companies
	}
	if date, ok := fullDate(textValue(m["release_date"])); ok {
		out["first_release_date"] = date
	}
	if name != "launchbox" {
		if values := stringValues(m["alternate_titles"]); len(values) > 0 {
			out["alternative_names"] = values
		}
	}
	switch name {
	case "igdb":
		if n, e := strconv.ParseInt(textValue(m["rating_count"]), 10, 32); e == nil && n >= 0 {
			out["total_rating_count"] = n
		}
		if n, ok := boundedNumber(m["rating"], 100); ok {
			out["total_rating"] = strconv.FormatFloat(n, 'f', -1, 64)
		}
	case "screenscraper":
		if n, ok := boundedNumber(m["rating"], 20); ok {
			out["ss_score"] = strconv.FormatFloat(n/2, 'f', -1, 64)
		}
		if p := textValue(m["players"]); validPlayers(p) {
			out["player_count"] = p
		}
	case "launchbox":
		if n := positiveID(m["max_players"]); n > 0 && n <= 1024 {
			out["max_players"] = n
		}
		if n, ok := boundedNumber(m["community_rating"], 5); ok {
			out["community_rating"] = n
		}
	}
	return out
}
func validPlayers(s string) bool {
	parts := strings.Split(s, "-")
	if len(parts) > 2 || len(parts) == 0 {
		return false
	}
	last := int64(0)
	for _, p := range parts {
		n := positiveID(p)
		if n < 1 || n > 1024 || n < last {
			return false
		}
		last = n
	}
	return true
}
func readProviderYAML(root, name string, out any) (FileState, error) {
	state, e := fileState(root, name, 16<<20)
	if e != nil {
		return state, e
	}
	b, e := catalogfs.ReadFile(state.Path)
	if e != nil {
		return state, e
	}
	if e = yaml.Unmarshal(b, out); e != nil {
		return state, fmt.Errorf("invalid catalog source YAML")
	}
	again, e := fileState(root, name, 16<<20)
	if e != nil || again != state {
		return state, fmt.Errorf("catalog changed while reading")
	}
	return state, nil
}
func payloadProof(entry Entry) (VerificationProof, error) {
	p := VerificationProof{Size: entry.Source.Size, SHA1: entry.Source.SHA1, Flags: map[string]bool{}}
	f, e := catalogfs.Open(entry.Input.Path)
	if e != nil {
		return p, e
	}
	defer f.Close()
	var r io.Reader = f
	if entry.Source.Member != "" {
		info, e := f.Stat()
		if e != nil {
			return p, e
		}
		z, e := zip.NewReader(f, info.Size())
		if e != nil {
			return p, e
		}
		count := 0
		for _, m := range z.File {
			if m.Name == entry.Source.Member {
				count++
				rc, e := m.Open()
				if e != nil {
					return p, e
				}
				defer rc.Close()
				r = rc
			}
		}
		if count != 1 {
			return p, fmt.Errorf("payload member not unique")
		}
	}
	h1, h2, hm, hc := sha1.New(), sha256.New(), md5.New(), crc32.NewIEEE()
	n, e := io.Copy(io.MultiWriter(h1, h2, hm, hc), io.LimitReader(r, (512<<20)+1))
	if e != nil || n != p.Size || !strings.EqualFold(hex.EncodeToString(h1.Sum(nil)), p.SHA1) {
		return p, fmt.Errorf("payload differs from exact recognition")
	}
	p.MD5 = hex.EncodeToString(hm.Sum(nil))
	p.SHA256 = hex.EncodeToString(h2.Sum(nil))
	p.CRC32 = fmt.Sprintf("%08x", hc.Sum32())
	return p, nil
}
func buildProviderEvidence(p *Plan, entry Entry) (*ProviderEvidence, error) {
	rel, e := cleanRelative(filepath.ToSlash(entry.Source.Game))
	if e != nil {
		return nil, e
	}
	var doc providerDocument
	gameFile, e := readProviderYAML(p.DataRoot, filepath.Join(p.DataRoot, filepath.FromSlash(rel), "game.yaml"), &doc)
	if e != nil {
		return nil, e
	}
	if _, ok := doc.Releases[entry.Source.Release]; !ok {
		return nil, fmt.Errorf("recognized release absent from source card")
	}
	var fps fingerprintDocument
	fingerFile, e := readProviderYAML(p.DataRoot, filepath.Join(p.DataRoot, filepath.FromSlash(rel), "fingerprints.yaml"), &fps)
	if e != nil {
		return nil, e
	}
	proof, e := payloadProof(entry)
	if e != nil {
		return nil, e
	}
	return assembleProviderEvidence(entry, doc, fps, proof, []FileState{gameFile, fingerFile})
}

// ProjectSnapshot reuses the native consumer normalization over a portable API
// snapshot. It does not read local catalog files or fetch a metadata provider.
func ProjectSnapshot(record exporter.Record, gameYAML, fingerprintYAML []byte, proof VerificationProof, before map[string]json.RawMessage) (*ProviderEvidence, map[string]string, error) {
	var doc providerDocument
	var fps fingerprintDocument
	if err := yaml.Unmarshal(gameYAML, &doc); err != nil {
		return nil, nil, err
	}
	if err := yaml.Unmarshal(fingerprintYAML, &fps); err != nil {
		return nil, nil, err
	}
	_, knownRelease := doc.Releases[record.Release]
	_, knownBasis := doc.Releases[record.MetadataRelease]
	if !knownRelease && !(record.MatchedBy == "game-name" && record.Release == "" && knownBasis) {
		return nil, nil, fmt.Errorf("snapshot release missing")
	}
	optical := record.Disc != nil && record.MatchedBy == "game-name" && record.Release == "" && record.SHA1 == "" && exporter.ValidDiscObservation(record.ROM, record.Size, record.Disc) && proof.Size == 0 && proof.SHA1 == "" && proof.SHA256 == "" && proof.MD5 == "" && proof.CRC32 == ""
	if record.Status != "matched" || record.Disc != nil && !optical || !optical && (record.MatchedBy != "hash" && record.MatchedBy != "game-name" || record.Size != proof.Size || record.SHA1 != proof.SHA1 || len(proof.SHA256) != 64 || len(proof.MD5) != 32 || len(proof.CRC32) != 8) {
		return nil, nil, fmt.Errorf("snapshot proof incomplete")
	}
	proof.Flags = map[string]bool{}
	proof.Authority = ""
	proof.Fingerprint = ""
	proof.HasheousMatchIndex = nil
	proof.HasheousQuery = ""
	entry := Entry{Source: record, Before: before}
	evidence, err := assembleProviderEvidence(entry, doc, fps, proof, nil)
	if err != nil {
		return nil, nil, err
	}
	// These two limitations belong only to the HTTP edit transport. Native-runtime
	// consumers can write source slugs and documented local media projections.
	limits := []string{}
	for _, limit := range evidence.Unsupported {
		if !strings.HasPrefix(limit, "IGDB source slug:") && !strings.HasPrefix(limit, "EmuMovies:") {
			limits = append(limits, limit)
		}
	}
	evidence.Unsupported = limits
	return evidence, providerForm(entry, evidence), nil
}

func assembleProviderEvidence(entry Entry, doc providerDocument, fps fingerprintDocument, proof VerificationProof, files []FileState) (*ProviderEvidence, error) {
	if entry.Source.Payload != nil && len(entry.Source.Payload.Components) > 0 {
		var expected []exporter.PayloadProof
		for _, fp := range fps.Items {
			if fp.Release == entry.Source.Release && fp.Source.Kind == "dat" && strings.EqualFold(fp.Source.Name, "No-Intro") {
				expected = append(expected, exporter.PayloadProof{Size: fp.File.Size, SHA1: strings.ToLower(fp.File.SHA1), SHA256: strings.ToLower(fp.File.SHA256), MD5: strings.ToLower(fp.File.MD5), CRC32: strings.ToLower(fp.File.CRC32)})
			}
		}
		if entry.Source.MatchedBy != "hash" || !exporter.ExactPayloadSet(*entry.Source.Payload, expected) {
			return nil, fmt.Errorf("compound input lacks complete authoritative set proof")
		}
		proof.Components = entry.Source.Payload.Components
	}
	evidence := &ProviderEvidence{Files: files, Slots: map[string]ProviderSlot{}, References: map[string]string{}, Unsupported: []string{"OpenVGDB: no native provider slot; shared selected text/manual provenance retained", "EmuMovies: media-only source; selected stock cover/manual profile, no video import", "RetroAchievements: reference-only; native ID edit can fetch and no offline raw payload contract"}}
	sourceNames := make([]string, 0, len(doc.Sources))
	for name := range doc.Sources {
		sourceNames = append(sourceNames, name)
	}
	sort.Strings(sourceNames)
	for _, source := range sourceNames {
		for index, m := range doc.Sources[source].Matches {
			descriptions := map[string]string{}
			if values, ok := m["descriptions"].(map[string]any); ok {
				for lang, v := range values {
					if text, ok := v.(string); ok && strings.TrimSpace(text) != "" {
						descriptions[lang] = text
					}
				}
			}
			if len(descriptions) > 0 {
				evidence.SourceTexts = append(evidence.SourceTexts, SourceTextSnapshot{Source: source, MatchIndex: index, Scope: sourceScope(m, entry.Source.Release), SourceRelease: textValue(m["release"]), Descriptions: descriptions})
			}
		}
	}

	for _, fp := range fps.Items {
		if entry.Source.MatchedBy != "hash" {
			break
		}
		if fp.Release != entry.Source.Release || fp.File.Size != proof.Size || !strings.EqualFold(fp.File.SHA1, proof.SHA1) {
			continue
		}
		if fp.File.MD5 != "" && !strings.EqualFold(fp.File.MD5, proof.MD5) || fp.File.SHA256 != "" && !strings.EqualFold(fp.File.SHA256, proof.SHA256) || fp.File.CRC32 != "" && !strings.EqualFold(fp.File.CRC32, proof.CRC32) {
			return nil, fmt.Errorf("authoritative fingerprint disagrees with payload")
		}
		if fp.Source.Kind == "dat" && strings.EqualFold(fp.Source.Name, "No-Intro") {
			proof.Authority = "No-Intro DAT"
			proof.Fingerprint = fp.ID
			proof.Flags["nointro_match"] = true
		}
	}
	for _, name := range []string{"igdb", "screenscraper", "launchbox", "hasheous"} {
		m, index, id, scope, e := chooseMatch(doc.Sources[name].Matches, sourceID[name], entry.Source.Release)
		if e != nil {
			evidence.Conflicts = append(evidence.Conflicts, ProviderConflict{Provider: name, Reason: e.Error()})
			continue
		}
		if id == 0 {
			continue
		}
		slot := ProviderSlot{ID: id, Source: name, MatchIndex: index, Scope: scope, SourceRelease: textValue(m["release"])}
		if name != "hasheous" {
			slot.Metadata = normalizeProvider(name, m)
			slot.SourceDate = textValue(m["release_date"])
			slot.SourceSlug = textValue(m["slug"])
		}
		evidence.Slots[name] = slot
		if slot.SourceSlug != "" {
			evidence.Unsupported = append(evidence.Unsupported, "IGDB source slug: native top-level edit field absent; source_slug retained for correct page link")
		}
		if name == "igdb" {
			for _, k := range []string{"platforms", "relationships", "game_type"} {
				if m[k] != nil {
					evidence.Unsupported = append(evidence.Unsupported, "IGDB "+k+": source lacks full native structured representation")
				}
			}
		}
	}
	reviews := map[int64]providerrefs.IGDBDecision{}
	for _, decision := range doc.Manual.ProviderReferences.IGDB {
		if err := decision.Validate(); err != nil {
			return nil, err
		}
		if _, exists := reviews[decision.FromID]; exists {
			return nil, fmt.Errorf("duplicate reviewed IGDB decision")
		}
		reviews[decision.FromID] = decision
	}
	// Provider references establish game identity, never payload verification. Require
	// consensus across every reference in the explicit card before inheritance.
	for ref, target := range map[string]string{"igdb_id": "igdb", "thegamesdb_id": "tgdb", "retroachievements_id": "retroachievements"} {
		_, _, id, _, refErr := chooseMatch(doc.Sources["hasheous"].Matches, ref, entry.Source.Release)
		if refErr != nil {
			evidence.Conflicts = append(evidence.Conflicts, ProviderConflict{Provider: "hasheous." + ref, Reason: refErr.Error()})
			continue
		}
		if id == 0 {
			continue
		}
		if target == "igdb" {
			if decision, ok := reviews[id]; ok {
				evidence.ReferenceReviews = append(evidence.ReferenceReviews, decision)
				evidence.References["hasheous.igdb_id"] = strconv.FormatInt(id, 10)
				evidence.Unsupported = append(evidence.Unsupported, fmt.Sprintf("Reviewed Hasheous IGDB reference %d: %s; %s", id, decision.Action, decision.Reason))
				if decision.Action == "suppress" {
					if _, exists := evidence.Slots["igdb"]; exists {
						return nil, fmt.Errorf("suppressed IGDB reference conflicts with own provider snapshot")
					}
					continue
				}
				own, exists := evidence.Slots["igdb"]
				if !exists || own.ID != decision.ToID || own.Source != "igdb" || own.SourceSlug == "" {
					return nil, fmt.Errorf("reviewed IGDB replacement requires its verified direct identity-link snapshot")
				}
				continue
			}
		}
		if target == "retroachievements" {
			evidence.References[target] = strconv.FormatInt(id, 10)
			continue
		}
		if old, ok := evidence.Slots[target]; ok {
			if old.ID != id {
				// A provider's own snapshot owns its identity. An indirect foreign
				// reference is useful evidence, not authority to replace that ID.
				evidence.References["hasheous."+ref] = strconv.FormatInt(id, 10)
				evidence.Unsupported = append(evidence.Unsupported, fmt.Sprintf("Hasheous %s reference %d conflicts with direct %s ID %d; direct provider retained, cross-reference only", ref, id, target, old.ID))
			}
			continue
		}
		evidence.Slots[target] = ProviderSlot{ID: id, Source: "hasheous-reference", MatchIndex: -1, Scope: "card-identity-only"}
	}
	// An exact scoped Hasheous response must additionally agree with the actual
	// payload hash query. Truncated source query prefixes are retained as such.
	for i, m := range doc.Sources["hasheous"].Matches {
		if entry.Source.MatchedBy != "hash" {
			break
		}
		scope := sourceScope(m, entry.Source.Release)
		if scope != "exact-release" && scope != "listed-release" {
			continue
		}
		q := strings.SplitN(textValue(m["matched_query"]), ":", 2)
		if len(q) != 2 || len(q[1]) < 8 {
			continue
		}
		digest := map[string]string{"md5": proof.MD5, "sha1": proof.SHA1, "sha256": proof.SHA256, "crc32": proof.CRC32}[strings.ToLower(q[0])]
		if digest == "" || !strings.HasPrefix(digest, strings.ToLower(q[1])) {
			continue
		}
		proof.HasheousMatchIndex = &i
		proof.HasheousQuery = textValue(m["matched_query"])
		for _, sig := range stringValues(m["signatures"]) {
			if key := signatureFields[sig]; key != "" {
				proof.Flags[key] = true
			}
		}
		break
	}
	evidence.Verification = proof
	if slot, ok := evidence.Slots["hasheous"]; ok {
		slot.Metadata = map[string]any{}
		for _, key := range signatureFields {
			slot.Metadata[key] = proof.Flags[key]
		}
		evidence.Slots["hasheous"] = slot
	}
	sort.Slice(evidence.Conflicts, func(i, j int) bool {
		return evidence.Conflicts[i].Provider < evidence.Conflicts[j].Provider
	})
	return evidence, nil
}
func providerForm(entry Entry, evidence *ProviderEvidence) map[string]string {
	form := proposedForm(entry.Source, entry.Before)
	for _, name := range providerNames {
		slot, ok := evidence.Slots[name]
		if !ok {
			continue
		}
		idKey := nativeID[name]
		metadataKey := nativeMetadata[name]
		existing := positiveRawID(entry.Before[idKey])
		hasMetadata := metadataKey != "" && nonemptyRaw(entry.Before[metadataKey])
		if fieldLocked(entry.Before, idKey) || fieldLocked(entry.Before, metadataKey) {
			slot.Preserved = "locked provider field"
			evidence.Slots[name] = slot
			continue
		}
		if existing != 0 && existing != slot.ID {
			slot.Preserved = "existing different provider identity"
			evidence.Slots[name] = slot
			continue
		}
		if hasMetadata && existing == 0 {
			slot.Preserved = "unbound existing provider metadata"
			evidence.Slots[name] = slot
			continue
		}
		if hasMetadata && providerHasMedia(entry.Before[metadataKey]) {
			slot.Preserved = "existing provider media; whole slot preserved to avoid refresh"
			evidence.Slots[name] = slot
			continue
		}
		desired := map[string]any{}
		for key, value := range slot.Metadata {
			desired[key] = value
		}
		if hasMetadata {
			var old map[string]json.RawMessage
			if json.Unmarshal(entry.Before[metadataKey], &old) != nil || old == nil {
				slot.Preserved = "unrecognized existing provider metadata"
				evidence.Slots[name] = slot
				continue
			}
			merged := map[string]any{}
			for key, value := range old {
				merged[key] = value
			}
			filled := false
			for key, value := range desired {
				// A newly proven positive flag may upgrade a previous default false;
				// donor signatures never enter desired, and true is not downgraded.
				verifiedUpgrade := name == "hasheous" && value == true && strings.TrimSpace(string(old[key])) == "false"
				if !nonemptyRaw(old[key]) || verifiedUpgrade {
					merged[key] = value
					filled = true
				}
			}
			if !filled {
				slot.Preserved = "existing provider metadata (fill-only)"
				evidence.Slots[name] = slot
				continue
			}
			desired = merged
			slot.Preserved = "existing fields retained; blank known fields filled"
			evidence.Slots[name] = slot
		}
		if existing == 0 {
			form[idKey] = strconv.FormatInt(slot.ID, 10)
		}
		if metadataKey != "" && len(desired) > 0 {
			b, _ := json.Marshal(desired)
			form["raw_"+metadataKey] = string(b)
		}
	}
	// Retain owner title/prose in the native re-match handler unless proposedForm
	// selected an allowed empty/filesystem-derived value. Other fields stay fill-only.
	if len(form) > 0 {
		if _, ok := form["name"]; !ok {
			form["name"] = rawString(entry.Before, "name")
		}
		if _, ok := form["summary"]; !ok {
			form["summary"] = rawString(entry.Before, "summary")
		}
	}
	return form
}
func positiveRawID(b json.RawMessage) int64 {
	var n int64
	if json.Unmarshal(b, &n) == nil && n > 0 {
		return n
	}
	return 0
}
func (c *Client) offlineProviders(ctx context.Context) error {
	var heartbeat struct {
		System struct {
			Version string `json:"VERSION"`
		} `json:"SYSTEM"`
		Sources map[string]bool `json:"METADATA_SOURCES"`
	}
	if e := c.request(ctx, "GET", "/heartbeat", "", nil, &heartbeat); e != nil {
		return e
	}
	if heartbeat.System.Version != "5.4.0" {
		return fmt.Errorf("offline provider adapter requires reviewed RomM 5.4.0 contract")
	}
	for _, key := range []string{"IGDB_API_ENABLED", "SS_API_ENABLED", "LAUNCHBOX_API_ENABLED", "HASHEOUS_API_ENABLED"} {
		value, ok := heartbeat.Sources[key]
		if !ok || value {
			return fmt.Errorf("offline provider import requires %s=false", key)
		}
	}
	return nil
}

// AddProviders adds native identities/data to the selected stock-API media and fill-only
// provider import. It never broadens the selected inventory or writes the server.
func AddProviders(ctx context.Context, c *Client, p *Plan) error {
	if p.Operation != "" {
		return fmt.Errorf("provider plan requires ordinary selection")
	}
	if e := c.offlineProviders(ctx); e != nil {
		return e
	}
	p.Operation = providerImport
	p.CoverBytes = 0
	for i := range p.Entries {
		entry := &p.Entries[i]
		if !coverWritable(entry.Before) {
			entry.Cover = nil
		}
		if entry.Cover != nil {
			p.CoverBytes += entry.Cover.Size
		}
		evidence, e := buildProviderEvidence(p, *entry)
		if e != nil {
			return fmt.Errorf("ROM %d provider evidence: %w", entry.ROM.ID, e)
		}
		entry.Providers = evidence
		entry.Form = providerForm(*entry, evidence)
	}
	return nil
}
func expectedProviderForm(p *Plan, entry Entry) (map[string]string, error) {
	if entry.Providers == nil || entry.DateRepair != nil {
		return nil, fmt.Errorf("invalid provider operation")
	}
	evidence, e := buildProviderEvidence(p, entry)
	if e != nil {
		return nil, e
	}
	form := providerForm(entry, evidence)
	// JSON canonicalization allows native numeric JSON decoding without weakening evidence.
	a, _ := json.Marshal(evidence)
	b, _ := json.Marshal(entry.Providers)
	if !sameJSON(a, b) {
		return nil, fmt.Errorf("provider sources/evidence changed")
	}
	return form, nil
}
func verifyProviderResponse(entry Entry, after map[string]json.RawMessage) error {
	if entry.Providers == nil {
		return nil
	}
	for key, value := range entry.Form {
		switch {
		case strings.HasPrefix(key, "raw_"):
			target := strings.TrimPrefix(key, "raw_")
			if !sameJSON([]byte(value), after[target]) {
				return fmt.Errorf("RomM did not retain submitted %s", target)
			}
		case strings.HasSuffix(key, "_id"):
			if strconv.FormatInt(positiveRawID(after[key]), 10) != value {
				return fmt.Errorf("RomM did not retain submitted %s", key)
			}
		}
	}
	for _, key := range []string{"name", "summary", "manual_metadata", "url_cover", "ra_id", "moby_id", "gamelist_id", "libretro_id", "url_manual", "slug", "merged_screenshots"} {
		if key == "manual_metadata" {
			if _, ok := entry.Form["raw_manual_metadata"]; ok {
				continue
			}
		}
		if key == "name" || key == "summary" {
			if value, ok := entry.Form[key]; ok {
				if rawString(after, key) != value {
					return fmt.Errorf("submitted owner field not retained")
				}
				continue
			}
		}
		if key == "url_cover" && entry.Cover != nil {
			continue
		}
		if !sameOptionalJSON(entry.Before[key], after[key]) {
			return fmt.Errorf("provider edit changed protected %s", key)
		}
	}
	for _, key := range []string{"path_cover_small", "path_cover_large", "path_manual"} {
		if (entry.Cover == nil || key == "path_manual") && !sameResourcePath(rawString(entry.Before, key), rawString(after, key)) {
			return fmt.Errorf("provider edit changed protected %s", key)
		}
	}
	for _, name := range providerNames {
		for _, key := range []string{nativeID[name], nativeMetadata[name]} {
			if key == "" {
				continue
			}
			if _, changed := entry.Form[key]; changed {
				continue
			}
			if _, changed := entry.Form["raw_"+key]; changed {
				continue
			}
			if !sameOptionalJSON(entry.Before[key], after[key]) {
				return fmt.Errorf("provider edit changed preserved %s", key)
			}
		}
	}
	return nil
}

// PrintProviderSummary presents the exact intended scope without requiring JSON tooling.
func PrintProviderSummary(w io.Writer, p *Plan) {
	if p.Operation != providerImport {
		return
	}
	games := map[string]bool{}
	changed := 0
	counts := map[string]int{}
	metadata := map[string]int{}
	verified := 0
	table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "ROM ID\tCatalog game\tIGDB\tSS\tLaunchBox\tHasheous\tExact verification")
	for _, e := range p.Entries {
		games[e.Source.Game] = true
		if len(e.Form) > 0 || e.Cover != nil || e.Manual != nil {
			changed++
		}
		cols := []string{}
		for _, name := range []string{"igdb", "screenscraper", "launchbox", "hasheous"} {
			s, ok := e.Providers.Slots[name]
			v := "-"
			if ok {
				counts[name]++
				v = strconv.FormatInt(s.ID, 10)
				if len(s.Metadata) > 0 {
					metadata[name]++
					v += "+data"
				} else {
					v += " ID-only"
				}
				if s.Preserved != "" {
					v += " preserved"
				}
			}
			cols = append(cols, v)
		}
		flags := []string{}
		for k, v := range e.Providers.Verification.Flags {
			if v {
				flags = append(flags, strings.TrimSuffix(k, "_match"))
			}
		}
		sort.Strings(flags)
		if len(flags) > 0 {
			verified++
		}
		fmt.Fprintf(table, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", e.ROM.ID, filepath.Base(e.Source.Game), cols[0], cols[1], cols[2], cols[3], strings.Join(flags, ","))
	}
	table.Flush()
	for _, entry := range p.Entries {
		changes := humanChanges(entry)
		if len(changes) > 0 {
			fmt.Fprintf(w, "Changes id %d: %s\n", entry.ROM.ID, strings.Join(changes, "; "))
		}
	}
	fmt.Fprintf(w, "Provider plan: %d files -> %d explicit catalog games; %d changed, %d preserved/no-op; %d exact-verified files; media %.2f MiB covers + %.2f MiB manuals.\n", len(p.Entries), len(games), changed, len(p.Entries)-changed, verified, float64(p.CoverBytes)/(1<<20), float64(p.ManualBytes)/(1<<20))
	for _, name := range []string{"igdb", "screenscraper", "launchbox", "hasheous"} {
		fmt.Fprintf(w, "%s: %d identities, %d metadata snapshots, %d ID-only.\n", name, counts[name], metadata[name], counts[name]-metadata[name])
	}
	fmt.Fprintln(w, "TGDB: ID references only. RetroAchievements: reference-only (no lookup/achievement claim). OpenVGDB: shared text/manual provenance; no native slot. EmuMovies: existing media profile, no video copy.\nOwner title, prose, manual dates and covers preserved. Native grouping uses provider game identity; verification is release/hash-specific. Review private JSON for exact scopes, hashes, skipped fields and recovery baselines.")
}

func allowedEditField(key string, providers bool) bool {
	if key == "name" || key == "summary" || key == "raw_manual_metadata" {
		return true
	}
	if !providers {
		return false
	}
	for _, name := range providerNames {
		if key == nativeID[name] || nativeMetadata[name] != "" && key == "raw_"+nativeMetadata[name] {
			return true
		}
	}
	return false
}

func sameOptionalJSON(a, b []byte) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return sameJSON(a, b)
}
func providerHasMedia(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return true
	}
	for key, value := range fields {
		if (strings.HasSuffix(key, "_url") || strings.HasSuffix(key, "_path") || key == "images") && nonemptyRaw(value) {
			return true
		}
	}
	return false
}

// RomM's response adds updated_at as a cache-busting ts query to resource URLs.
// Compare the actual resource and every other URL component, not that revision.
func sameResourcePath(a, b string) bool {
	if a == b {
		return true
	}
	left, e := url.Parse(a)
	if e != nil {
		return false
	}
	right, e := url.Parse(b)
	if e != nil {
		return false
	}
	lq, e := url.ParseQuery(left.RawQuery)
	if e != nil {
		return false
	}
	rq, e := url.ParseQuery(right.RawQuery)
	if e != nil {
		return false
	}
	lq.Del("ts")
	rq.Del("ts")
	left.RawQuery = lq.Encode()
	right.RawQuery = rq.Encode()
	return left.String() == right.String()
}

func humanChanges(entry Entry) []string {
	var changes []string
	keys := make([]string, 0, len(entry.Form))
	for key := range entry.Form {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := entry.Form[key]
		if strings.HasPrefix(key, "raw_") {
			field := strings.TrimPrefix(key, "raw_")
			var previous, next map[string]json.RawMessage
			_ = json.Unmarshal(entry.Before[field], &previous)
			_ = json.Unmarshal([]byte(value), &next)
			fields := make([]string, 0, len(next))
			for f := range next {
				fields = append(fields, f)
			}
			sort.Strings(fields)
			for _, f := range fields {
				if !sameOptionalJSON(previous[f], next[f]) {
					shown := string(next[f])
					if len(shown) > 80 {
						shown = shown[:80] + "..."
					}
					changes = append(changes, field+"."+f+"="+shown)
				}
			}
		} else if (key == "name" || key == "summary") && rawString(entry.Before, key) == value {
			continue
		} else {
			changes = append(changes, key+" set")
		}
	}
	if entry.Cover != nil {
		changes = append(changes, fmt.Sprintf("cover upload %d bytes", entry.Cover.Size))
	}
	if entry.Manual != nil {
		changes = append(changes, fmt.Sprintf("manual upload %d bytes", entry.Manual.Size))
	}
	return changes
}
