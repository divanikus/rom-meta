package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

type manifest struct {
	SchemaVersion int               `json:"schema_version"`
	Snapshots     []snapshot        `json:"snapshots,omitempty"`
	Entries       []entry           `json:"entries"`
	Holds         []json.RawMessage `json:"holds,omitempty"`
}
type snapshot struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url,omitempty"`
}
type variant struct {
	Key       string   `json:"key"`
	Title     string   `json:"title,omitempty"`
	Kind      string   `json:"kind"`
	BasedOn   string   `json:"based_on,omitempty"`
	Languages []string `json:"languages,omitempty"`
	Group     string   `json:"group,omitempty"`
	Version   string   `json:"version,omitempty"`
}
type fingerprint struct {
	Kind   string `json:"kind,omitempty"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA1   string `json:"sha1,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	MD5    string `json:"md5,omitempty"`
	CRC32  string `json:"crc32,omitempty"`
}
type input struct {
	Path    string `json:"path"`
	Member  string `json:"member,omitempty"`
	Size    int64  `json:"size,omitempty"`
	MTimeNS int64  `json:"mtime_ns,omitempty"`
}
type evidence struct {
	Relationship  string           `json:"relationship"`
	Links         []map[string]any `json:"links"`
	Normalization []string         `json:"normalization,omitempty"`
}

type newGame struct {
	Title string `json:"title"`
}

// datReference names an exact published output in a pinned local translation XML DAT.
// It supplies output identity only; the reviewed game relationship stays separate.
type datReference struct {
	Path           string `json:"path"`
	Member         string `json:"member,omitempty"`
	Name           string `json:"name"`
	SourcePlatform string `json:"source_platform,omitempty"`
}
type entry struct {
	NewGame   *newGame            `json:"new_game,omitempty"`
	DAT       *datReference       `json:"dat,omitempty"`
	Published *publishedReference `json:"published,omitempty"`
	Platform  string              `json:"platform"`
	Game      string              `json:"game"`
	Variant   variant             `json:"variant"`
	File      fingerprint         `json:"file"`
	Evidence  evidence            `json:"evidence"`
	Expected  map[string]string   `json:"expected_documents"`
	Inputs    []input             `json:"inputs,omitempty"`
}

func checkJSONKeys(raw []byte) error {
	// encoding/json otherwise accepts duplicate keys and silently keeps the last.
	tokens := json.NewDecoder(bytes.NewReader(raw))
	var check func() error
	check = func() error {
		t, err := tokens.Token()
		if err != nil {
			return err
		}
		d, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch d {
		case '{':
			seen := map[string]bool{}
			for tokens.More() {
				k, err := tokens.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return fmt.Errorf("duplicate or invalid JSON key %q", k)
				}
				seen[key] = true
				if err := check(); err != nil {
					return err
				}
			}
		case '[':
			for tokens.More() {
				if err := check(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter")
		}
		_, err = tokens.Token()
		return err
	}
	if err := check(); err != nil {
		return err
	}
	if _, err := tokens.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func decodeManifest(raw []byte) (*manifest, error) {
	if err := checkJSONKeys(raw); err != nil {
		return nil, err
	}
	var m manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m.SchemaVersion != 1 || len(m.Entries) == 0 {
		return nil, fmt.Errorf("need schema_version 1 and at least one entry")
	}
	return &m, nil
}

func safeName(s string) bool {
	if s == "" || s == "." || s == ".." || strings.TrimSpace(s) != s || strings.HasSuffix(s, ".") || strings.ContainsAny(s, "/\\:*?\"<>|") {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
		return false
	}
	return true
}

func safeRelative(s string) bool {
	if s == "" || path.IsAbs(s) || strings.Contains(s, "\\") || path.Clean(s) != s {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if !safeName(part) {
			return false
		}
	}
	return true
}
func validHash(s string, n int) bool { b, e := hex.DecodeString(s); return e == nil && len(b) == n }
func validateEntry(e entry) error {
	for _, s := range []string{e.Platform, e.Game, e.Variant.Key} {
		if !safeName(s) || strings.HasPrefix(s, ".") {
			return fmt.Errorf("unsafe catalog name %q", s)
		}
	}
	if e.Variant.BasedOn != "" && (!safeName(e.Variant.BasedOn) || strings.HasPrefix(e.Variant.BasedOn, ".")) {
		return fmt.Errorf("unsafe base reference")
	}
	if e.Variant.BasedOn == "" && e.Evidence.Relationship != "reviewed" {
		return fmt.Errorf("game-only association requires reviewed game evidence")
	}
	if strings.HasPrefix(e.Game, "_") {
		return fmt.Errorf("leading-underscore game is skipped by core catalog validation")
	}
	if e.Variant.Kind == "" {
		return fmt.Errorf("variant kind required")
	}
	if e.File.Kind == "chd-combined-sha1" {
		if e.File.Name == "" || e.File.Size < 0 || !validHash(e.File.SHA1, 20) || strings.Trim(e.File.SHA1, "0") == "" || e.File.SHA256 != "" || e.File.MD5 != "" || e.File.CRC32 != "" {
			return fmt.Errorf("CHD identity needs combined SHA1, optional logical size and no raw-file digests")
		}
	} else if e.File.Kind != "" || e.File.Name == "" || e.File.Size <= 0 || (e.File.SHA1 == "" && e.File.SHA256 == "") {
		return fmt.Errorf("need supported representation, payload name, positive size and SHA1 or SHA256")
	}
	for _, h := range []struct {
		s string
		n int
	}{{e.File.SHA1, 20}, {e.File.SHA256, 32}, {e.File.MD5, 16}, {e.File.CRC32, 4}} {
		if h.s != "" && !validHash(h.s, h.n) {
			return fmt.Errorf("invalid payload digest")
		}
	}
	if e.NewGame != nil && (strings.TrimSpace(e.NewGame.Title) == "" || e.Variant.BasedOn != "" || e.Evidence.Relationship != "reviewed") {
		return fmt.Errorf("new game needs a title and game-only reviewed evidence")
	}
	_, hasGame := e.Expected["game.yaml"]
	_, hasFP := e.Expected["fingerprints.yaml"]
	if len(e.Expected) != 2 || !hasGame || !hasFP || (e.NewGame == nil && (!validHash(e.Expected["game.yaml"], 32) || !validHash(e.Expected["fingerprints.yaml"], 32))) || (e.NewGame != nil && (e.Expected["game.yaml"] != "" || e.Expected["fingerprints.yaml"] != "")) {
		return fmt.Errorf("expected SHA256 of both catalog documents required")
	}
	switch e.Evidence.Relationship {
	case "source-release-name", "source-base-hash", "reviewed":
	default:
		return fmt.Errorf("unsupported relationship evidence %q", e.Evidence.Relationship)
	}
	if len(e.Evidence.Links) == 0 {
		return fmt.Errorf("source links required")
	}
	for _, link := range e.Evidence.Links {
		for _, key := range []string{"source", "entry", "parent"} {
			if s, ok := link[key].(string); !ok || strings.TrimSpace(s) == "" {
				return fmt.Errorf("source link needs %s", key)
			}
		}
	}
	if e.DAT != nil && e.Published != nil {
		return fmt.Errorf("choose one published output reference: DAT or structured source")
	}
	if e.Published != nil {
		if err := validatePublishedReference(*e.Published); err != nil {
			return err
		}
	}
	if len(e.Inputs) == 0 && e.DAT == nil && e.Published == nil {
		return fmt.Errorf("actual input or an exact published output reference required")
	}
	for _, in := range e.Inputs {
		if !safeRelative(in.Path) || (in.Member != "" && !safeRelative(in.Member)) || in.Size < 0 || in.MTimeNS < 0 {
			return fmt.Errorf("unsafe or invalid ROM input %q", in.Path)
		}
		if strings.Split(in.Path, "/")[0] != e.Platform {
			return fmt.Errorf("input must start with the exact platform folder")
		}
	}
	return nil
}

func within(root, name string) bool {
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		name = strings.ToLower(name)
	}
	rel, err := filepath.Rel(root, name)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
