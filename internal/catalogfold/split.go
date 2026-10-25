package catalogfold

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/releasepolicy"
	"rommeta/internal/taskprogress"
)

// SplitPlan explicitly owns every release and extra file of each reviewed card.
// Content keys are durable negative boundaries, never positive match evidence.
type SplitPlan struct {
	Version  int          `json:"version"`
	Platform string       `json:"platform"`
	Groups   []SplitGroup `json:"groups"`
}
type SplitGroup struct {
	Folder             string             `json:"folder"`
	GameSHA256         string             `json:"game_sha256"`
	FingerprintsSHA256 string             `json:"fingerprints_sha256"`
	Reason             string             `json:"reason"`
	Parts              []SplitPart        `json:"parts"`
	Files              []SplitFile        `json:"files"`
	SourceRoutes       []SplitSourceRoute `json:"source_routes,omitempty"`
}
type SplitSourceRoute struct {
	Source     string `json:"source"`
	MatchIndex int    `json:"match_index"`
	Folder     string `json:"folder"`
}
type SplitPart struct {
	Folder     string   `json:"folder"`
	Title      string   `json:"title"`
	ContentKey string   `json:"content_key"`
	Releases   []string `json:"releases"`
}
type SplitFile struct {
	Path    string        `json:"path"`
	Size    int64         `json:"size"`
	MtimeNS int64         `json:"mtime_ns"`
	SHA256  string        `json:"sha256,omitempty"`
	Targets []SplitTarget `json:"targets"`
	Reason  string        `json:"reason"`
}
type SplitTarget struct {
	Folder string `json:"folder"`
	Path   string `json:"path"`
}
type SplitOptions struct {
	DataRoot, ReviewPlan string
	Apply                bool
	RegionPriority       []string
	RegionGroups         map[string][]string
	Progress             taskprogress.Func
}
type SplitResult struct {
	Platform     string `json:"platform"`
	Groups       int    `json:"groups"`
	NewCards     int    `json:"new_cards"`
	Releases     int    `json:"releases"`
	Fingerprints int    `json:"fingerprints"`
	Files        int    `json:"files"`
	LinkedFiles  int    `json:"linked_files"`
	CopiedFiles  int    `json:"copied_files"`
	Backup       string `json:"backup,omitempty"`
	Apply        bool   `json:"apply"`
}

func Split(options SplitOptions) (SplitResult, error) {
	var result SplitResult
	data, err := catalogfs.ReadFile(options.ReviewPlan)
	if err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	decoder.DisallowUnknownFields()
	var plan SplitPlan
	if err = decoder.Decode(&plan); err != nil {
		return result, err
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("split plan must contain one JSON document")
	}
	if plan.Version != 1 || !reviewComponent(plan.Platform) || len(plan.Groups) == 0 {
		return result, fmt.Errorf("split plan requires version 1, a safe platform and reviewed groups")
	}
	root, err := filepath.Abs(options.DataRoot)
	if err != nil {
		return result, err
	}
	result = SplitResult{Platform: plan.Platform, Groups: len(plan.Groups), Apply: options.Apply}
	// Preflight every group and target before staging any mutation.
	reserved := map[string]bool{}
	entries, err := catalogfs.ReadDir(filepath.Join(root, "platforms", plan.Platform))
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		reserved[strings.ToLower(entry.Name())] = true
	}
	seen := map[string]bool{}
	for i, group := range plan.Groups {
		options.Progress.Update("checking split plan", i+1, len(plan.Groups), group.Folder)
		if !reviewComponent(group.Folder) || seen[strings.ToLower(group.Folder)] || strings.TrimSpace(group.Reason) == "" || len(group.Parts) < 2 {
			return result, fmt.Errorf("invalid or duplicate split group %q", group.Folder)
		}
		seen[strings.ToLower(group.Folder)] = true
		primary := 0
		keys := map[string]bool{}
		for _, part := range group.Parts {
			key := strings.ToLower(part.Folder)
			if !reviewComponent(part.Folder) || strings.TrimSpace(part.Title) == "" || strings.TrimSpace(part.ContentKey) == "" || keys[part.ContentKey] || len(part.Releases) == 0 {
				return result, fmt.Errorf("invalid split part in %q", group.Folder)
			}
			keys[part.ContentKey] = true
			if part.Folder == group.Folder {
				primary++
			} else if reserved[key] {
				return result, fmt.Errorf("split target already exists: %s", part.Folder)
			}
			reserved[key] = true
		}
		if primary != 1 {
			return result, fmt.Errorf("split %q must retain its original folder exactly once", group.Folder)
		}
		releases, fps, err := checkSplitGroup(root, plan.Platform, group, options)
		if err != nil {
			return result, fmt.Errorf("%s: %w", group.Folder, err)
		}
		result.Releases += releases
		result.Fingerprints += fps
		result.Files += len(group.Files)
		result.NewCards += len(group.Parts) - 1
	}
	if !options.Apply {
		return result, nil
	}
	tx, err := beginPlatformTransaction(root, plan.Platform)
	if err != nil {
		return result, err
	}
	defer tx.abort()
	for i, group := range plan.Groups {
		options.Progress.Update("splitting cards", i+1, len(plan.Groups), group.Folder)
		if _, _, err = checkSplitGroup(tx.root, plan.Platform, group, options); err != nil {
			return result, err
		}
		if err = applySplitGroup(tx.root, plan.Platform, group, options, &result); err != nil {
			return result, err
		}
	}
	// Detect changed inputs after the potentially long platform clone too.
	for _, group := range plan.Groups {
		if _, _, err = checkSplitGroup(root, plan.Platform, group, options); err != nil {
			return result, err
		}
	}
	options.Progress.Update("committing split", 1, 1, plan.Platform)
	if err = tx.commit(); err != nil {
		return result, err
	}
	result.Backup = tx.backupPath
	return result, nil
}

func checkSplitGroup(root, platform string, group SplitGroup, options SplitOptions) (int, int, error) {
	if err := verifyReviewedMember(root, platform, ReviewedMember{Folder: group.Folder, GameSHA256: group.GameSHA256, FingerprintsSHA256: group.FingerprintsSHA256}); err != nil {
		return 0, 0, err
	}
	path := filepath.Join(root, "platforms", platform, group.Folder)
	game, err := catalogfs.ReadFile(filepath.Join(path, "game.yaml"))
	if err != nil {
		return 0, 0, err
	}
	fp, err := catalogfs.ReadFile(filepath.Join(path, "fingerprints.yaml"))
	if err != nil {
		return 0, 0, err
	}
	_, releases, fps, err := renderSplitDocuments(game, fp, group, options)
	if err != nil {
		return 0, 0, err
	}
	expected := map[string]bool{}
	parts := map[string]bool{}
	targets := map[string]bool{}
	for _, part := range group.Parts {
		parts[part.Folder] = true
	}
	for _, file := range group.Files {
		key := strings.ToLower(filepath.ToSlash(file.Path))
		if !splitRelativePath(file.Path) || key == "game.yaml" || key == "fingerprints.yaml" || expected[key] || len(file.Targets) == 0 || strings.TrimSpace(file.Reason) == "" {
			return 0, 0, fmt.Errorf("invalid or uncovered extra file %q", file.Path)
		}
		expected[key] = true
		full := filepath.Join(path, filepath.FromSlash(file.Path))
		info, err := catalogfs.Lstat(full)
		if err != nil {
			return 0, 0, err
		}
		if !info.Mode().IsRegular() || info.Size() != file.Size || info.ModTime().UnixNano() != file.MtimeNS {
			return 0, 0, fmt.Errorf("stale or nonregular split file %s", file.Path)
		}
		if file.SHA256 != "" {
			hash, err := splitFileHash(full)
			if err != nil {
				return 0, 0, err
			}
			if !strings.EqualFold(hash, file.SHA256) {
				return 0, 0, fmt.Errorf("split file SHA256 changed: %s", file.Path)
			}
		}
		for _, target := range file.Targets {
			key := strings.ToLower(target.Folder + "/" + filepath.ToSlash(target.Path))
			if !parts[target.Folder] || !splitRelativePath(target.Path) || strings.EqualFold(target.Path, "game.yaml") || strings.EqualFold(target.Path, "fingerprints.yaml") || targets[key] {
				return 0, 0, fmt.Errorf("invalid or colliding split file target %s", key)
			}
			targets[key] = true
		}
	}
	count := 0
	err = catalogfs.WalkDir(path, func(full string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked split input: %s", full)
		}
		if entry.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(path, full)
		key := strings.ToLower(filepath.ToSlash(rel))
		if key == "game.yaml" || key == "fingerprints.yaml" {
			return nil
		}
		if !expected[key] {
			return fmt.Errorf("unplanned split file: %s", rel)
		}
		count++
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	if count != len(expected) {
		return 0, 0, fmt.Errorf("split file inventory changed")
	}
	return releases, fps, nil
}
func splitRelativePath(path string) bool {
	if path == "" || strings.Contains(path, "\\") || filepath.IsAbs(path) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if !reviewComponent(part) {
			return false
		}
	}
	return true
}
func splitFileHash(path string) (string, error) {
	file, err := catalogfs.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

type splitDocument struct{ game, fp []byte }

func renderSplitDocuments(gameBytes, fpBytes []byte, group SplitGroup, options SplitOptions) (map[string]splitDocument, int, int, error) {
	game, err := catalogyaml.Parse(gameBytes)
	if err != nil {
		return nil, 0, 0, err
	}
	fp, err := catalogyaml.Parse(fpBytes)
	if err != nil {
		return nil, 0, 0, err
	}
	if catalogyaml.Value(game.Root(), "arcade") != nil || catalogyaml.Value(game.Root(), "variants") != nil {
		return nil, 0, 0, fmt.Errorf("versioned/derived bindings need a separate partition adapter")
	}
	owners := map[string]string{}
	partitions := map[string]SplitPart{}
	for _, part := range group.Parts {
		partitions[part.Folder] = part
		for _, release := range part.Releases {
			if owners[release] != "" {
				return nil, 0, 0, fmt.Errorf("release occurs in multiple partitions: %s", release)
			}
			owners[release] = part.Folder
		}
	}
	releases := catalogyaml.Value(game.Root(), "releases")
	count := 0
	err = catalogyaml.Mapping(releases, func(name string, value *yaml.Node) error {
		if owners[name] == "" {
			return fmt.Errorf("release omitted from split: %s", name)
		}
		if catalogyaml.Value(value, "mame") != nil {
			return fmt.Errorf("MAME bindings cannot be split here")
		}
		count++
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	if count != len(owners) {
		return nil, 0, 0, fmt.Errorf("split names a nonexistent release")
	}
	items := catalogyaml.Value(fp.Root(), "items")
	if items == nil || items.Kind != yaml.SequenceNode {
		return nil, 0, 0, fmt.Errorf("split needs canonical fingerprint items")
	}
	for _, item := range items.Content {
		release := catalogyaml.Scalar(catalogyaml.Value(item, "release"))
		if owners[release] == "" {
			return nil, 0, 0, fmt.Errorf("unowned fingerprint release %q", release)
		}
	}
	routes := map[string]map[int]string{}
	for _, route := range group.SourceRoutes {
		stanza := catalogyaml.Value(catalogyaml.Value(game.Root(), "sources"), route.Source)
		matches := catalogyaml.Value(stanza, "matches")
		if matches == nil || matches.Kind != yaml.SequenceNode || route.MatchIndex < 0 || route.MatchIndex >= len(matches.Content) || partitions[route.Folder].Folder == "" {
			return nil, 0, 0, fmt.Errorf("invalid source route")
		}
		if routes[route.Source] == nil {
			routes[route.Source] = map[int]string{}
		}
		if routes[route.Source][route.MatchIndex] != "" {
			return nil, 0, 0, fmt.Errorf("duplicate source route")
		}
		if _, err := splitSourceMatch(matches.Content[route.MatchIndex], owners, route.Folder, route.Folder, group.Folder); err != nil {
			return nil, 0, 0, err
		}
		routes[route.Source][route.MatchIndex] = route.Folder
	}
	result := map[string]splitDocument{}
	for _, part := range group.Parts {
		current, err := catalogyaml.Parse(gameBytes)
		if err != nil {
			return nil, 0, 0, err
		}
		primary := part.Folder == group.Folder
		if !primary {
			current, err = catalogyaml.Parse([]byte("title: ''\n"))
			if err != nil {
				return nil, 0, 0, err
			}
			if old := catalogyaml.Value(game.Root(), "fold"); old != nil {
				if err = catalogyaml.Set(current.Root(), "fold", cloneSplitNode(old)); err != nil {
					return nil, 0, 0, err
				}
			}
		}
		root := current.Root()
		_ = catalogyaml.Set(root, "title", &yaml.Node{Kind: yaml.ScalarNode, Value: part.Title, Style: yaml.DoubleQuotedStyle})
		rs := &yaml.Node{Kind: yaml.MappingNode}
		_ = catalogyaml.Mapping(releases, func(name string, node *yaml.Node) error {
			if owners[name] == part.Folder {
				_ = catalogyaml.Set(rs, name, cloneSplitNode(node))
			}
			return nil
		})
		_ = catalogyaml.Set(root, "releases", rs)
		chosen := catalogyaml.Scalar(catalogyaml.Value(game.Root(), "default_release"))
		if owners[chosen] != part.Folder {
			chosen = releasepolicy.Choose(part.Releases, options.RegionPriority, options.RegionGroups)
		}
		_ = catalogyaml.Set(root, "default_release", &yaml.Node{Kind: yaml.ScalarNode, Value: chosen, Style: yaml.DoubleQuotedStyle})
		fold, err := catalogyaml.EnsureMapping(root, "fold")
		if err != nil {
			return nil, 0, 0, err
		}
		_ = catalogyaml.Set(fold, "content_key", &yaml.Node{Kind: yaml.ScalarNode, Value: part.ContentKey, Style: yaml.DoubleQuotedStyle})
		sources := &yaml.Node{Kind: yaml.MappingNode}
		err = catalogyaml.Mapping(catalogyaml.Value(game.Root(), "sources"), func(source string, stanza *yaml.Node) error {
			matches := catalogyaml.Value(stanza, "matches")
			selected := &yaml.Node{Kind: yaml.SequenceNode}
			matchIndex := -1
			if err := catalogyaml.Sequence(matches, func(match *yaml.Node) error {
				matchIndex++
				copy, err := splitSourceMatch(match, owners, part.Folder, routes[source][matchIndex], group.Folder)
				if err != nil {
					return err
				}
				if copy != nil {
					selected.Content = append(selected.Content, copy)
				}
				return nil
			}); err != nil {
				return err
			}
			if !primary && len(selected.Content) == 0 {
				return nil
			}
			copy := cloneSplitNode(stanza)
			if matches != nil {
				if err := catalogyaml.Set(copy, "matches", selected); err != nil {
					return err
				}
			}
			return catalogyaml.Set(sources, source, copy)
		})
		if err != nil {
			return nil, 0, 0, err
		}
		if len(sources.Content) > 0 {
			_ = catalogyaml.Set(root, "sources", sources)
		}
		fingerprints, err := catalogyaml.Parse(fpBytes)
		if err != nil {
			return nil, 0, 0, err
		}
		if !primary {
			fingerprints, err = catalogyaml.Parse([]byte("items: []\n"))
			if err != nil {
				return nil, 0, 0, err
			}
		}
		selected := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range items.Content {
			if owners[catalogyaml.Scalar(catalogyaml.Value(item, "release"))] == part.Folder {
				selected.Content = append(selected.Content, cloneSplitNode(item))
			}
		}
		_ = catalogyaml.Set(fingerprints.Root(), "items", selected)
		g, err := current.Marshal()
		if err != nil {
			return nil, 0, 0, err
		}
		f, err := fingerprints.Marshal()
		if err != nil {
			return nil, 0, 0, err
		}
		// Partitioning must not emit aliases whose anchors stayed in another part.
		if _, err = catalogyaml.Parse(g); err != nil {
			return nil, 0, 0, fmt.Errorf("partition game document: %w", err)
		}
		if _, err = catalogyaml.Parse(f); err != nil {
			return nil, 0, 0, fmt.Errorf("partition fingerprint document: %w", err)
		}
		result[part.Folder] = splitDocument{g, f}
	}
	return result, count, len(items.Content), nil
}
func cloneSplitNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	copy := *node
	copy.Content = nil
	for _, child := range node.Content {
		copy.Content = append(copy.Content, cloneSplitNode(child))
	}
	// Alias targets are immutable; retaining the pointer also avoids cycles.
	return &copy
}

func applySplitGroup(root, platform string, group SplitGroup, options SplitOptions, result *SplitResult) error {
	platformPath := filepath.Join(root, "platforms", platform)
	source := filepath.Join(platformPath, group.Folder)
	g, err := catalogfs.ReadFile(filepath.Join(source, "game.yaml"))
	if err != nil {
		return err
	}
	f, err := catalogfs.ReadFile(filepath.Join(source, "fingerprints.yaml"))
	if err != nil {
		return err
	}
	documents, _, _, err := renderSplitDocuments(g, f, group, options)
	if err != nil {
		return err
	}
	for _, file := range group.Files {
		original := filepath.Join(source, filepath.FromSlash(file.Path))
		keepOriginal := false
		for _, target := range file.Targets {
			destination := filepath.Join(platformPath, target.Folder, filepath.FromSlash(target.Path))
			if destination == original {
				keepOriginal = true
				continue
			}
			if err = catalogfs.MkdirAll(filepath.Dir(destination), 0755); err != nil {
				return err
			}
			if _, err = catalogfs.Lstat(destination); !os.IsNotExist(err) {
				return fmt.Errorf("split destination exists: %s", destination)
			}
			if err = catalogfs.Link(original, destination); err == nil {
				a, e := catalogfs.Stat(original)
				if e != nil {
					return e
				}
				b, e := catalogfs.Stat(destination)
				if e != nil {
					return e
				}
				if !catalogfs.SameFile(a, b) {
					return fmt.Errorf("split hard-link verification failed")
				}
				result.LinkedFiles++
			} else {
				if err = copyFoldFile(original, destination); err != nil {
					return err
				}
				a, e := splitFileHash(original)
				if e != nil {
					return e
				}
				b, e := splitFileHash(destination)
				if e != nil {
					return e
				}
				if a != b {
					return fmt.Errorf("split copy SHA256 verification failed")
				}
				result.CopiedFiles++
			}
		}
		if !keepOriginal {
			if err = catalogfs.Remove(original); err != nil {
				return err
			}
		}
	}
	for folder, doc := range documents {
		path := filepath.Join(platformPath, folder)
		if err = catalogfs.MkdirAll(path, 0755); err != nil {
			return err
		}
		if err = catalogfs.WriteFile(filepath.Join(path, "game.yaml"), doc.game, 0644); err != nil {
			return err
		}
		if err = catalogfs.WriteFile(filepath.Join(path, "fingerprints.yaml"), doc.fp, 0644); err != nil {
			return err
		}
	}
	var dirs []string
	if err = catalogfs.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != source {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		entries, err := catalogfs.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err = catalogfs.Remove(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

// Multi-release provider records are projected onto each owning partition.
// Only scope fields change; every other payload field is copied unchanged.
func splitSourceMatch(match *yaml.Node, owners map[string]string, part, route, primary string) (*yaml.Node, error) {
	if match == nil || match.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("source match must be a mapping")
	}
	scopes := map[string]bool{}
	release := catalogyaml.Scalar(catalogyaml.Value(match, "release"))
	if release != "" {
		if owners[release] == "" {
			return nil, fmt.Errorf("unowned source release %q", release)
		}
		scopes[owners[release]] = true
	}
	if err := catalogyaml.Sequence(catalogyaml.Value(match, "releases"), func(item *yaml.Node) error {
		name := splitSourceRelease(item)
		if owners[name] == "" {
			return fmt.Errorf("unowned source release %q", name)
		}
		scopes[owners[name]] = true
		return nil
	}); err != nil {
		return nil, err
	}
	if route != "" {
		for owner := range scopes {
			if owner != route {
				return nil, fmt.Errorf("source route contradicts exact release ownership")
			}
		}
	}
	if len(scopes) == 0 {
		owner := primary
		if route != "" {
			owner = route
		}
		if owner != part {
			return nil, nil
		}
		return cloneSplitNode(match), nil
	}
	if !scopes[part] {
		return nil, nil
	}
	copy := cloneSplitNode(match)
	if release != "" && owners[release] != part {
		for i := 0; i < len(copy.Content); i += 2 {
			if copy.Content[i].Value == "release" {
				copy.Content = append(copy.Content[:i], copy.Content[i+2:]...)
				break
			}
		}
	}
	if list := catalogyaml.Value(copy, "releases"); list != nil {
		selected := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range list.Content {
			if owners[splitSourceRelease(item)] == part {
				selected.Content = append(selected.Content, item)
			}
		}
		if err := catalogyaml.Set(copy, "releases", selected); err != nil {
			return nil, err
		}
	}
	return copy, nil
}

// OpenVGDB keeps complete per-release payloads in mapping entries; other
// providers use scalar release names. Routing filters entries, never their data.
func splitSourceRelease(item *yaml.Node) string {
	if item != nil && item.Kind == yaml.MappingNode {
		return catalogyaml.Scalar(catalogyaml.Value(item, "release"))
	}
	return catalogyaml.Scalar(item)
}
