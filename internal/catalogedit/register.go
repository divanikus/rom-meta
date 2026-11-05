package catalogedit

import (
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

// RegisterPatch addresses records under the opened game and fingerprint revisions.
type RegisterPatch struct {
	Releases       []ReleaseEdit     `json:"releases,omitempty"`
	Fingerprints   []FingerprintEdit `json:"fingerprints,omitempty"`
	DefaultRelease *string           `json:"default_release,omitempty"`
}
type ReleaseEdit struct {
	Original    string `json:"original,omitempty"`
	Replacement string `json:"replacement,omitempty"`
	Name        string `json:"name,omitempty"`
	Fields      Patch  `json:"fields,omitempty"`
	Delete      bool   `json:"delete,omitempty"`
}
type FingerprintEdit struct {
	Index  int   `json:"index"`
	Fields Patch `json:"fields,omitempty"`
	Delete bool  `json:"delete,omitempty"`
}
type ReleaseRecord struct {
	Name   string `json:"name"`
	Fields Patch  `json:"fields"`
}
type FingerprintRecord struct {
	Index  int   `json:"index"`
	Fields Patch `json:"fields"`
}
type Register struct {
	Revision       string                `json:"revision"`
	YAML           string                `json:"yaml"`
	Releases       []ReleaseRecord       `json:"releases"`
	Fingerprints   []FingerprintRecord   `json:"fingerprints"`
	DefaultRelease string                `json:"default_release"`
	Variants       []catalogyaml.Variant `json:"variants"`
}

func readFingerprintFile(path string) ([]byte, error) {
	b, err := catalogfs.ReadFile(filepath.Join(path, "fingerprints.yaml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}
func fingerprintDocument(data []byte) (*catalogyaml.Document, *yaml.Node, error) {
	if len(data) == 0 {
		data = []byte("items: []\n")
	}
	if !utf8.Valid(data) || strings.ContainsFunc(string(data), func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) {
		return nil, nil, fmt.Errorf("fingerprints must be UTF-8 without control characters")
	}
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	if err = validateRawNode(doc.Root(), "fingerprints", 0); err != nil {
		return nil, nil, err
	}
	items := catalogyaml.Value(doc.Root(), "items")
	if items != nil && catalogyaml.Value(doc.Root(), "roms") != nil {
		return nil, nil, fmt.Errorf("fingerprints: use items or legacy roms, not both")
	}
	if items == nil {
		items = catalogyaml.Value(doc.Root(), "roms")
	}
	if items == nil {
		items = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		catalogyaml.Set(doc.Root(), "items", items)
	}
	if items.Kind != yaml.SequenceNode {
		return nil, nil, fmt.Errorf("fingerprints must be a list")
	}
	return doc, items, nil
}
func InspectRegister(game, fingerprints []byte) (Register, error) {
	result := Register{Revision: Hash(fingerprints), YAML: string(fingerprints), Releases: []ReleaseRecord{}, Fingerprints: []FingerprintRecord{}}
	doc, err := catalogyaml.Parse(game)
	if err != nil {
		return result, err
	}
	result.DefaultRelease = catalogyaml.Scalar(catalogyaml.Value(doc.Root(), "default_release"))
	result.Variants, err = catalogyaml.Variants(doc.Root())
	if err != nil {
		return result, err
	}
	err = catalogyaml.Mapping(catalogyaml.Value(doc.Root(), "releases"), func(name string, n *yaml.Node) error {
		fields, e := recordFields(n, "")
		result.Releases = append(result.Releases, ReleaseRecord{name, fields})
		return e
	})
	if err != nil {
		return result, err
	}
	_, items, err := fingerprintDocument(fingerprints)
	if err != nil {
		return result, err
	}
	for i, n := range items.Content {
		fields, e := recordFields(n, "")
		if e != nil {
			return result, e
		}
		result.Fingerprints = append(result.Fingerprints, FingerprintRecord{i, fields})
	}
	return result, nil
}
func ReadRegister(root, platform, folder string, game []byte) (Register, error) {
	path, err := GamePath(root, platform, folder)
	if err != nil {
		return Register{}, err
	}
	fp, err := readFingerprintFile(path)
	if err != nil {
		return Register{}, err
	}
	return InspectRegister(game, fp)
}
func registerText(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 1024 && !strings.ContainsFunc(value, unicode.IsControl)
}
func patchRegisterNode(node *yaml.Node, patch Patch) error {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := patch[key]
		if key == "size" {
			if number, ok := value.(float64); ok {
				if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 9007199254740991 || math.Trunc(number) != number {
					return fmt.Errorf("file size must be a nonnegative whole number of bytes")
				}
				value = int64(number)
			}
		}
		if !registerText(key) || key == "__proto__" || key == "constructor" || key == "prototype" {
			return fmt.Errorf("invalid field name %q", key)
		}
		if err := validValue(value, 0); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if value == nil {
			Delete(node, key)
			continue
		}
		// Nested file edits retain unsubmitted file fields and comments.
		if key == "file" {
			if fields, ok := value.(map[string]any); ok {
				n, err := catalogyaml.EnsureMapping(node, key)
				if err != nil {
					return err
				}
				if err = patchRegisterNode(n, fields); err != nil {
					return err
				}
				continue
			}
		}
		n := &yaml.Node{}
		if err := n.Encode(value); err != nil {
			return err
		}
		if old := catalogyaml.Value(node, key); old != nil {
			n.HeadComment = old.HeadComment
			n.LineComment = old.LineComment
			n.FootComment = old.FootComment
		}
		if err := catalogyaml.Set(node, key, n); err != nil {
			return err
		}
	}
	return nil
}
func remapScalar(node *yaml.Node, names map[string]string) {
	if node != nil && node.Kind == yaml.ScalarNode {
		if name, ok := names[node.Value]; ok {
			node.Value = name
		}
	}
}

// Traverse only model-defined source links, never authority.entry_name or custom data.
func remapSource(node *yaml.Node, names map[string]string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	remapScalar(catalogyaml.Value(node, "release"), names)
	for _, key := range []string{"matches", "releases"} {
		list := catalogyaml.Value(node, key)
		if list == nil || list.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range list.Content {
			if key == "releases" && item.Kind == yaml.ScalarNode {
				remapScalar(item, names)
			} else {
				remapSource(item, names)
			}
		}
	}
}
func ApplyRegister(game, fingerprints []byte, patch RegisterPatch) ([]byte, []byte, error) {
	if len(patch.Releases) > 2048 || len(patch.Fingerprints) > 20000 {
		return nil, nil, fmt.Errorf("too many edited records")
	}
	originalGame, originalFingerprints := game, fingerprints
	doc, err := catalogyaml.Parse(game)
	if err != nil {
		return nil, nil, err
	}
	fp, items, err := fingerprintDocument(fingerprints)
	if err != nil {
		return nil, nil, err
	}
	releases, err := catalogyaml.EnsureMapping(doc.Root(), "releases")
	if err != nil {
		return nil, nil, err
	}
	originalOrder := append([]*yaml.Node(nil), releases.Content...)
	names := map[string]string{}
	seen := map[string]bool{}
	nodes := map[string]*yaml.Node{}
	keys := map[string]*yaml.Node{}
	for i := 0; i < len(releases.Content); i += 2 {
		keys[releases.Content[i].Value] = releases.Content[i]
		nodes[releases.Content[i].Value] = releases.Content[i+1]
	}
	for _, edit := range patch.Releases {
		if edit.Original != "" {
			if nodes[edit.Original] == nil || seen[edit.Original] {
				return nil, nil, fmt.Errorf("release %q is missing or edited twice", edit.Original)
			}
			seen[edit.Original] = true
		} else if edit.Delete {
			return nil, nil, fmt.Errorf("choose an existing release to delete")
		}
		if !edit.Delete && !registerText(edit.Name) {
			return nil, nil, fmt.Errorf("release name must be nonempty text without control characters")
		}
		if edit.Delete && edit.Replacement != "" {
			names[edit.Original] = edit.Replacement
		}
		if !edit.Delete && edit.Replacement != "" {
			return nil, nil, fmt.Errorf("replacement is only used when removing a release")
		}
		if edit.Original != "" && !edit.Delete {
			names[edit.Original] = edit.Name
		}
	}
	// Remove edited keys first, allowing deliberate swaps without order dependence.
	for name := range seen {
		Delete(releases, name)
	}
	for _, edit := range patch.Releases {
		if edit.Delete {
			continue
		}
		if catalogyaml.Value(releases, edit.Name) != nil || catalogyaml.Value(catalogyaml.Value(doc.Root(), "variants"), edit.Name) != nil {
			return nil, nil, fmt.Errorf("release %q already exists or collides with a variant", edit.Name)
		}
		node := nodes[edit.Original]
		key := keys[edit.Original]
		if node == nil {
			node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			key = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str"}
		}
		if err = patchRegisterNode(node, edit.Fields); err != nil {
			return nil, nil, fmt.Errorf("release %q: %w", edit.Name, err)
		}
		for _, field := range []string{"regions", "languages"} {
			n := catalogyaml.Value(node, field)
			if n == nil {
				continue
			}
			if n.Kind != yaml.SequenceNode {
				return nil, nil, fmt.Errorf("release %q: %s must be a list", edit.Name, field)
			}
			for _, v := range n.Content {
				if v.Tag != "!!str" || !registerText(v.Value) {
					return nil, nil, fmt.Errorf("release %q: invalid %s", edit.Name, field)
				}
			}
		}
		if date := catalogyaml.Scalar(catalogyaml.Value(node, "release_date")); date != "" && !validDate(date) {
			return nil, nil, fmt.Errorf("release %q: use YYYY, YYYY-MM or YYYY-MM-DD", edit.Name)
		}
		key.Value = edit.Name
		releases.Content = append(releases.Content, key, node)
	}
	// Retain the order of surviving definitions, including renamed records.
	ordered := []*yaml.Node{}
	placed := map[*yaml.Node]bool{}
	for i := 0; i < len(originalOrder); i += 2 {
		key, node := originalOrder[i], originalOrder[i+1]
		if catalogyaml.Value(releases, key.Value) == node {
			ordered = append(ordered, key, node)
			placed[node] = true
		}
	}
	for i := 0; i < len(releases.Content); i += 2 {
		if !placed[releases.Content[i+1]] {
			ordered = append(ordered, releases.Content[i], releases.Content[i+1])
		}
	}
	releases.Content = ordered
	for _, edit := range patch.Releases {
		if edit.Delete && edit.Replacement != "" && catalogyaml.Value(releases, edit.Replacement) == nil {
			return nil, nil, fmt.Errorf("choose an existing replacement for removed release %q", edit.Original)
		}
	}
	remapScalar(catalogyaml.Value(doc.Root(), "default_release"), names)
	_ = catalogyaml.Mapping(catalogyaml.Value(doc.Root(), "sources"), func(_ string, n *yaml.Node) error { remapSource(n, names); return nil })
	_ = catalogyaml.Mapping(catalogyaml.Value(doc.Root(), "variants"), func(_ string, n *yaml.Node) error { remapScalar(catalogyaml.Value(n, "based_on"), names); return nil })
	for _, item := range items.Content {
		remapScalar(catalogyaml.Value(item, "release"), names)
	}
	if patch.DefaultRelease != nil {
		if *patch.DefaultRelease == "" {
			Delete(doc.Root(), "default_release")
		} else {
			catalogyaml.Set(doc.Root(), "default_release", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: *patch.DefaultRelease})
		}
	}
	originalCount := len(items.Content)
	edited := map[int]bool{}
	deleted := map[int]bool{}
	changed := map[*yaml.Node]bool{}
	for _, edit := range patch.Fingerprints {
		var node *yaml.Node
		if edit.Index == -1 && !edit.Delete {
			node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			items.Content = append(items.Content, node)
		} else {
			if edit.Index < 0 || edit.Index >= originalCount || edited[edit.Index] {
				return nil, nil, fmt.Errorf("fingerprint index is missing or edited twice")
			}
			edited[edit.Index] = true
			node = items.Content[edit.Index]
		}
		if edit.Delete {
			deleted[edit.Index] = true
			continue
		}
		if err = patchRegisterNode(node, edit.Fields); err != nil {
			return nil, nil, fmt.Errorf("fingerprint %d: %w", edit.Index+1, err)
		}
		changed[node] = true
	}
	kept := items.Content[:0]
	for i, item := range items.Content {
		if !deleted[i] {
			kept = append(kept, item)
		}
	}
	items.Content = kept
	game, err = doc.Marshal()
	if err != nil {
		return nil, nil, err
	}
	if _, err = ValidateRaw(string(game)); err != nil {
		return nil, nil, err
	}
	for i, item := range items.Content {
		if _, _, err = catalogyaml.FingerprintTarget(doc.Root(), item); err != nil {
			return nil, nil, fmt.Errorf("fingerprint %d: %w; reassign or remove its reference before deleting a release", i+1, err)
		}
		if changed[item] {
			if err = validateFingerprint(item); err != nil {
				return nil, nil, fmt.Errorf("fingerprint %d: %w", i+1, err)
			}
		}
	}
	fingerprints, err = fp.Marshal()
	if err != nil {
		return nil, nil, err
	}
	// Keep byte-for-byte originals when this side has no edit.
	if len(patch.Releases) == 0 && patch.DefaultRelease == nil {
		game = originalGame
	}
	renamed := false
	for old, next := range names {
		renamed = renamed || old != next
	}
	if len(patch.Fingerprints) == 0 && !renamed {
		fingerprints = originalFingerprints
	}
	return game, fingerprints, nil
}
func validateFingerprint(item *yaml.Node) error {
	file := catalogyaml.Value(item, "file")
	if file == nil || file.Kind != yaml.MappingNode {
		return fmt.Errorf("file fields are required")
	}
	if name := catalogyaml.Value(file, "name"); name == nil || name.Tag != "!!str" || !registerText(name.Value) {
		return fmt.Errorf("enter a file name")
	}
	size := catalogyaml.Value(file, "size")
	var sizeValue int64
	var sizeErr error
	if size != nil {
		sizeValue, sizeErr = strconv.ParseInt(size.Value, 10, 64)
	}
	if size == nil || size.Tag != "!!int" || sizeErr != nil || sizeValue < 0 {
		return fmt.Errorf("file size must be a nonnegative integer in bytes")
	}
	hashes := 0
	for _, hash := range []struct {
		name  string
		bytes int
	}{{"crc32", 4}, {"md5", 16}, {"sha1", 20}, {"sha256", 32}} {
		n := catalogyaml.Value(file, hash.name)
		if n == nil {
			continue
		}
		b, e := hex.DecodeString(n.Value)
		if n.Tag != "!!str" || e != nil || len(b) != hash.bytes {
			return fmt.Errorf("%s must contain exactly %d hexadecimal characters", strings.ToUpper(hash.name), hash.bytes*2)
		}
		hashes++
	}
	if hashes == 0 && catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(item, "dump"), "status")) != "nodump" {
		return fmt.Errorf("enter at least one file hash (CRC32, MD5, SHA1 or SHA256)")
	}
	return nil
}
