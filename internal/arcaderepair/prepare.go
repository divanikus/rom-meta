// Package arcaderepair is the binding-aware MAME backend used by catalog fold.
package arcaderepair

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/dat"
)

type Group struct {
	Sources      []string                     `json:"sources"`
	Target       string                       `json:"target"`
	Title        string                       `json:"title"`
	Before       map[string]map[string]string `json:"before"`
	After        map[string]string            `json:"after"`
	Releases     int                          `json:"releases,omitempty"`
	Fingerprints int                          `json:"fingerprints,omitempty"`
	OtherFiles   int                          `json:"other_files,omitempty"`
	Conflict     string                       `json:"conflict,omitempty"`
}

type Report struct {
	Version       int      `json:"version"`
	DataRoot      string   `json:"data_root"`
	Snapshot      string   `json:"snapshot"`
	BeforeCards   int      `json:"before_cards"`
	AfterCards    int      `json:"after_cards"`
	Sets          int      `json:"sets"`
	Groups        []Group  `json:"groups"`
	BindingBefore string   `json:"binding_before"`
	BindingAfter  string   `json:"binding_after"`
	Releases      int      `json:"preserved_releases"`
	Matches       int      `json:"preserved_source_matches"`
	Fingerprints  int      `json:"preserved_fingerprints"`
	OtherFiles    int      `json:"preserved_other_files"`
	Conflicts     []string `json:"conflicts,omitempty"`
}

// Prepare recalculates the currently accepted snapshot and stages changed cards
// in a new review directory. It never writes into the input catalog.
func Prepare(root, output string, progress func(string)) (Report, error) {
	return PrepareMatching(root, output, "", progress)
}

// PrepareMatching limits changes to complete groups matching a title or folder.
// Grouping and naming still use the complete accepted source, including sets
// outside a partial import. Unselected cards and their bindings stay unchanged.
func PrepareMatching(root, output, match string, progress func(string)) (Report, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return Report{}, err
	}
	if within(root, output) || within(output, root) {
		return Report{}, fmt.Errorf("review output must be outside the catalog")
	}
	if _, err = catalogfs.Lstat(output); !os.IsNotExist(err) {
		return Report{}, fmt.Errorf("review output must be a new directory: %s", output)
	}
	entries, err := catalogfs.ReadDir(filepath.Join(root, "bindings", "romsets", "mame"))
	if err != nil {
		return Report{}, err
	}
	var snapshots []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			snapshots = append(snapshots, e.Name())
		}
	}
	if len(snapshots) != 1 {
		return Report{}, fmt.Errorf("MAME folding requires exactly one accepted snapshot; found %d", len(snapshots))
	}
	snapshot := snapshots[0]
	if !component(snapshot) {
		return Report{}, fmt.Errorf("unsafe snapshot")
	}
	sourceDir := filepath.Join(root, "bindings", "romsets", "mame", snapshot)
	doc, err := readDocument(filepath.Join(sourceDir, "_source.yaml"))
	if err != nil {
		return Report{}, err
	}
	source := catalogyaml.Scalar(catalogyaml.Value(doc.Root(), "file"))
	if !component(source) {
		return Report{}, fmt.Errorf("unsafe source name")
	}
	bindings, err := readDocument(filepath.Join(sourceDir, "catalog.yaml"))
	if err != nil {
		return Report{}, err
	}
	var selected []string
	if err = catalogyaml.Mapping(catalogyaml.Value(bindings.Root(), "sets"), func(name string, _ *yaml.Node) error {
		selected = append(selected, name)
		return nil
	}); err != nil {
		return Report{}, err
	}
	if len(selected) == 0 {
		return Report{}, fmt.Errorf("accepted MAME snapshot has no set bindings")
	}
	sort.Strings(selected)
	if err = catalogfs.MkdirAll(output, 0755); err != nil {
		return Report{}, err
	}
	if progress != nil {
		progress("Recalculating MAME grouping")
	}
	next, err := dat.ImportArcade(filepath.Join(sourceDir, source), dat.ArcadeOptions{
		ImportOptions:      dat.ImportOptions{DataRoot: filepath.Join(output, "empty")},
		ClassificationPath: filepath.Join(sourceDir, "classifications.zip"), RoutingPath: filepath.Join(sourceDir, "routing.json"), Fold: true, Sets: selected, Offline: true,
	})
	if err != nil {
		return Report{}, err
	}
	if next.Snapshot != snapshot || len(next.Conflicts) > 0 {
		return Report{}, fmt.Errorf("snapshot changed or import conflicts; cannot repair")
	}
	return prepareProjection(root, output, next, match, progress)
}

func prepareProjection(root, output string, next dat.ArcadeResult, match string, progress func(string)) (r Report, err error) {
	r = Report{Version: 1, DataRoot: root, Snapshot: next.Snapshot, Sets: len(next.Bindings)}
	bindingRel := filepath.Join("bindings", "romsets", "mame", next.Snapshot, "catalog.yaml")
	bindingBefore, err := fileHash(filepath.Join(root, bindingRel))
	if err != nil {
		return r, err
	}
	oldDoc, err := readDocument(filepath.Join(root, bindingRel))
	if err != nil {
		return r, err
	}
	var old struct {
		Sets map[string]dat.ArcadeBinding `yaml:"sets"`
	}
	if err = oldDoc.Root().Decode(&old); err != nil {
		return r, err
	}
	if len(old.Sets) != len(next.Bindings) {
		return r, fmt.Errorf("set inventory differs; repair cannot add or remove sets")
	}
	forward := map[string]string{}
	backward := map[string][]string{}
	for name, b := range old.Sets {
		n, ok := next.Bindings[name]
		if !ok || b.Platform != "Arcade" || n.Platform != b.Platform || n.Family != b.Family || n.Hardware != b.Hardware || n.Release != b.Release || !component(b.Game) || !component(n.Game) || !component(b.Release) {
			return r, fmt.Errorf("%s changes release or hardware identity", name)
		}
		if v, ok := forward[b.Game]; ok && v != n.Game {
			return r, fmt.Errorf("%s would split; automatic enriched repair is merge/rename only", b.Game)
		}
		forward[b.Game] = n.Game
	}
	for from, to := range forward {
		backward[to] = append(backward[to], from)
	}
	r.BeforeCards = len(forward)
	wanted := map[string][]string{}
	selectedSources := map[string]bool{}
	match = strings.ToLower(strings.TrimSpace(match))
	for target, from := range backward {
		if len(from) == 1 && from[0] == target {
			continue
		}
		if match != "" && !strings.Contains(strings.ToLower(target+"\n"+next.Titles[target]+"\n"+next.Snapshot+"\n"+strings.Join(from, "\n")), match) {
			continue
		}
		wanted[target] = from
		for _, source := range from {
			selectedSources[source] = true
		}
	}
	selectedForward := map[string]string{}
	// Reserve unrelated cards too; a destination may never replace one.
	entries, err := catalogfs.ReadDir(filepath.Join(root, "platforms", "Arcade"))
	if err != nil {
		return r, err
	}
	existing := map[string]string{}
	for _, e := range entries {
		existing[strings.ToLower(e.Name())] = e.Name()
	}
	for _, target := range keys(wanted) {
		from := wanted[target]
		sort.Strings(from)
		if next.Titles[target] == "" {
			return r, fmt.Errorf("missing display title for %s", target)
		}
		if progress != nil {
			progress(target)
		}
		g := Group{Sources: from, Target: target, Title: next.Titles[target], Before: map[string]map[string]string{}}
		if original, ok := existing[strings.ToLower(target)]; ok && !selectedSources[original] {
			g.Conflict = fmt.Sprintf("destination occupied by unselected or unbound card: %s", original)
		} else if err = prepareGroup(root, output, &g, &r, old.Sets); err != nil {
			g.Conflict = err.Error()
		}
		if g.Conflict != "" {
			r.Conflicts = append(r.Conflicts, fmt.Sprintf("%s: %s", target, g.Conflict))
		} else {
			for _, source := range from {
				selectedForward[source] = target
			}
		}
		r.Groups = append(r.Groups, g)
	}
	// Change only the game projection, keeping the original binding document and
	// all its unknown fields, hardware evidence and set/release identities.
	sets := catalogyaml.Value(oldDoc.Root(), "sets")
	afterCards := map[string]bool{}
	for name, oldBinding := range old.Sets {
		target := oldBinding.Game
		if selected := selectedForward[target]; selected != "" {
			target = selected
		}
		afterCards[target] = true
		if target == oldBinding.Game {
			continue
		}
		if err = catalogyaml.Set(catalogyaml.Value(sets, name), "game", scalar(target)); err != nil {
			return r, err
		}
	}
	r.AfterCards = len(afterCards)
	data, err := oldDoc.Marshal()
	if err != nil {
		return r, err
	}
	if err = copyFile(filepath.Join(root, bindingRel), filepath.Join(output, "original", bindingRel)); err != nil {
		return r, err
	}
	if err = writeNew(filepath.Join(output, "staged", bindingRel), data); err != nil {
		return r, err
	}
	r.BindingBefore, err = fileHash(filepath.Join(output, "original", bindingRel))
	if err != nil {
		return r, err
	}
	if r.BindingBefore != bindingBefore {
		return r, fmt.Errorf("set bindings changed while preparing fold")
	}
	r.BindingAfter, err = fileHash(filepath.Join(output, "staged", bindingRel))
	if err != nil {
		return r, err
	}
	data, err = json.MarshalIndent(r, "", "  ")
	if err != nil {
		return r, err
	}
	if err = writeNew(filepath.Join(output, "review.json"), append(data, '\n')); err != nil {
		return r, err
	}
	if err = writeNew(filepath.Join(output, "PREVIEW.md"), []byte(preview(r))); err != nil {
		return r, err
	}
	return r, nil
}

func preview(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Arcade repair preview\n\n%d -> %d cards; %d MAME sets retained.\n\nThis is a prepared local projection. The input catalog has not been changed.\n", r.BeforeCards, r.AfterCards, r.Sets)
	if len(r.Conflicts) > 0 {
		b.WriteString("\n## Unresolved conflicts (apply is blocked)\n\n")
		for _, conflict := range r.Conflicts {
			fmt.Fprintf(&b, "- %s\n", conflict)
		}
	}
	b.WriteString("\n## Merges\n")
	for _, g := range r.Groups {
		if len(g.Sources) < 2 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n\n", g.Target)
		for _, from := range g.Sources {
			fmt.Fprintf(&b, "- %s\n", from)
		}
	}
	b.WriteString("\n## Renames\n\n")
	for _, g := range r.Groups {
		if len(g.Sources) == 1 {
			fmt.Fprintf(&b, "- %s -> %s\n", g.Sources[0], g.Target)
		}
	}
	fmt.Fprintf(&b, "\nPreserved in changed cards: %d releases, %d source matches, %d fingerprints, %d other file entries. Original files are in original/; prepared files are in staged/.\n", r.Releases, r.Matches, r.Fingerprints, r.OtherFiles)
	return b.String()
}

func prepareGroup(root, output string, g *Group, r *Report, bindings map[string]dat.ArcadeBinding) error {
	var game, fp *catalogyaml.Document
	files := map[string]string{}
	seenReleases := map[string]bool{}
	wantReleases, wantMatches, wantFP := 0, 0, 0
	other := 0
	checks := map[string][]*yaml.Node{}
	for _, from := range g.Sources {
		dir := filepath.Join(root, "platforms", "Arcade", from)
		manifest, err := treeHashes(dir)
		if err != nil {
			return err
		}
		g.Before[from] = manifest
		original := filepath.Join(output, "original", "platforms", "Arcade", from)
		for _, name := range keys(manifest) {
			if err = copyFile(filepath.Join(dir, name), filepath.Join(original, name)); err != nil {
				return err
			}
			if h, err := fileHash(filepath.Join(original, name)); err != nil || h != manifest[name] {
				return fmt.Errorf("input changed while copied: %s/%s", from, name)
			}
			if name == "game.yaml" || name == "fingerprints.yaml" {
				continue
			}
			key := strings.ToLower(name)
			if prev, ok := files[key]; ok {
				h, err := fileHash(prev)
				if err != nil {
					return err
				}
				if h != manifest[name] {
					return fmt.Errorf("different files occupy %s; both originals retained", name)
				}
			} else {
				files[key] = filepath.Join(original, name)
			}
			other++
		}
		d, err := readDocument(filepath.Join(original, "game.yaml"))
		if err != nil {
			return err
		}
		if len(g.Sources) > 1 && hasAnchors(d.Root()) {
			return fmt.Errorf("YAML anchors require manual review: %s", from)
		}
		f, err := readDocument(filepath.Join(original, "fingerprints.yaml"))
		if err != nil {
			return err
		}
		if len(g.Sources) > 1 && hasAnchors(f.Root()) {
			return fmt.Errorf("fingerprint YAML anchors require manual review: %s", from)
		}
		err = catalogyaml.Mapping(catalogyaml.Value(d.Root(), "releases"), func(id string, n *yaml.Node) error {
			b, ok := bindings[id]
			if !ok || b.Game != from || b.Release != id || seenReleases[id] {
				return fmt.Errorf("unbound or duplicate release: %s", id)
			}
			seenReleases[id] = true
			return nil
		})
		if err != nil {
			return err
		}
		a, b, c := counts(d.Root(), f.Root())
		wantReleases += a
		wantMatches += b
		wantFP += c
		if len(g.Sources) > 1 {
			if err = archiveChecks(d.Root(), from, checks); err != nil {
				return err
			}
		}
		if game == nil {
			game, fp = d, f
		} else {
			for _, key := range []string{"title", "default_release"} {
				if err = catalogyaml.Set(d.Root(), key, catalogyaml.Value(game.Root(), key)); err != nil {
					return err
				}
			}
			if err = mergeNode(game.Root(), d.Root(), "game"); err != nil {
				return err
			}
			if err = mergeNode(fp.Root(), f.Root(), "fingerprints"); err != nil {
				return err
			}
		}
	}
	for id, b := range bindings {
		if contains(g.Sources, b.Game) && !seenReleases[id] {
			return fmt.Errorf("binding has no release: %s", id)
		}
	}
	if err := catalogyaml.Set(game.Root(), "title", scalar(g.Title)); err != nil {
		return err
	}
	if len(g.Sources) > 1 {
		for provider, states := range checks {
			n := catalogyaml.Value(catalogyaml.Value(game.Root(), "sources"), provider)
			if n == nil {
				continue
			}
			if matches := catalogyaml.Value(n, "matches"); matches != nil && len(matches.Content) > 0 {
				continue
			}
			if len(states) != len(g.Sources) {
				continue
			}
			latest := ""
			allMissing := true
			for _, state := range states {
				if catalogyaml.Scalar(catalogyaml.Value(state, "status")) != "not_found" {
					allMissing = false
				}
				if at := catalogyaml.Scalar(catalogyaml.Value(state, "checked_at")); at > latest {
					latest = at
				}
			}
			if allMissing {
				if err := catalogyaml.Set(n, "status", scalar("not_found")); err != nil {
					return err
				}
				if latest != "" {
					if err := catalogyaml.Set(n, "checked_at", scalar(latest)); err != nil {
						return err
					}
				}
			}
		}
	}
	a, b, c := counts(game.Root(), fp.Root())
	if a != wantReleases || b != wantMatches || c != wantFP {
		return fmt.Errorf("preservation counts differ: releases %d/%d, matches %d/%d, fingerprints %d/%d", a, wantReleases, b, wantMatches, c, wantFP)
	}
	out := filepath.Join(output, "staged", "platforms", "Arcade", g.Target)
	for name, d := range map[string]*catalogyaml.Document{"game.yaml": game, "fingerprints.yaml": fp} {
		data, err := d.Marshal()
		if err != nil {
			return err
		}
		if err = writeNew(filepath.Join(out, name), data); err != nil {
			return err
		}
	}
	for _, source := range files { // Use the original spelling of the relative path.
		var rel string
		for _, from := range g.Sources {
			base := filepath.Join(output, "original", "platforms", "Arcade", from)
			if within(base, source) {
				rel, _ = filepath.Rel(base, source)
				break
			}
		}
		if rel == "" {
			return fmt.Errorf("invalid media source")
		}
		if err := copyFile(source, filepath.Join(out, rel)); err != nil {
			return err
		}
	}
	var err error
	g.After, err = treeHashes(out)
	if err != nil {
		return err
	}
	r.Releases += a
	r.Matches += b
	r.Fingerprints += c
	r.OtherFiles += other
	g.Releases, g.Fingerprints, g.OtherFiles = a, c, other
	return nil
}

// Negative lookups describe the old card's release scope. Keep each observation
// with that scope instead of overwriting its timestamp or retaining a stale
// not_found beside another card's positive matches.
func archiveChecks(game *yaml.Node, from string, checks map[string][]*yaml.Node) error {
	record := &yaml.Node{Kind: yaml.MappingNode}
	values := map[string]*yaml.Node{"card": scalar(from), "title": catalogyaml.Value(game, "title"), "default_release": catalogyaml.Value(game, "default_release")}
	for _, key := range []string{"card", "title", "default_release"} {
		value := values[key]
		if value == nil {
			return fmt.Errorf("missing %s in %s", key, from)
		}
		if err := catalogyaml.Set(record, key, value); err != nil {
			return err
		}
	}
	var releases []string
	if err := catalogyaml.Mapping(catalogyaml.Value(game, "releases"), func(id string, _ *yaml.Node) error { releases = append(releases, id); return nil }); err != nil {
		return err
	}
	if err := catalogyaml.Set(record, "releases", catalogyaml.StringSequence(releases)); err != nil {
		return err
	}
	states := &yaml.Node{Kind: yaml.MappingNode}
	err := catalogyaml.Mapping(catalogyaml.Value(game, "sources"), func(provider string, n *yaml.Node) error {
		state := &yaml.Node{Kind: yaml.MappingNode}
		for _, key := range []string{"status", "checked_at"} {
			if v := catalogyaml.Value(n, key); v != nil {
				if err := catalogyaml.Set(state, key, v); err != nil {
					return err
				}
				remove(n, key)
			}
		}
		if len(state.Content) > 0 {
			checks[provider] = append(checks[provider], state)
			return catalogyaml.Set(states, provider, state)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err = catalogyaml.Set(record, "source_checks", states); err != nil {
		return err
	}
	history, err := catalogyaml.EnsureMapping(game, "arcade_repair")
	if err != nil {
		return err
	}
	seq := catalogyaml.Value(history, "original_cards")
	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode}
		if err = catalogyaml.Set(history, "original_cards", seq); err != nil {
			return err
		}
	}
	if seq.Kind != yaml.SequenceNode {
		return fmt.Errorf("arcade_repair.original_cards must be a sequence")
	}
	seq.Content = append(seq.Content, record)
	return nil
}

func remove(node *yaml.Node, key string) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

// Recurse into mappings; retain every sequence item. Scalar conflicts stop the
// migration instead of choosing a provider result or losing unknown local data.
func mergeNode(dst, src *yaml.Node, path string) error {
	if dst == nil || src == nil {
		return fmt.Errorf("missing node at %s", path)
	}
	if dst.Kind == yaml.SequenceNode && src.Kind == yaml.SequenceNode {
		dst.Content = append(dst.Content, src.Content...)
		return nil
	}
	if dst.Kind != yaml.MappingNode || src.Kind != yaml.MappingNode {
		if equalNode(dst, src) {
			return nil
		}
		return fmt.Errorf("conflicting field %s", path)
	}
	return catalogyaml.Mapping(src, func(key string, value *yaml.Node) error {
		if prior := catalogyaml.Value(dst, key); prior != nil {
			return mergeNode(prior, value, path+"."+key)
		}
		return catalogyaml.Set(dst, key, value)
	})
}

func equalNode(a, b *yaml.Node) bool {
	var x, y any
	if a.Decode(&x) != nil || b.Decode(&y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
func counts(game, fp *yaml.Node) (releases, matches, fingerprints int) {
	if n := catalogyaml.Value(game, "releases"); n != nil {
		releases = len(n.Content) / 2
	}
	_ = catalogyaml.Mapping(catalogyaml.Value(game, "sources"), func(_ string, n *yaml.Node) error {
		if m := catalogyaml.Value(n, "matches"); m != nil {
			matches += len(m.Content)
		}
		return nil
	})
	if n := catalogyaml.Value(fp, "items"); n != nil {
		fingerprints = len(n.Content)
	}
	return
}
func readDocument(path string) (*catalogyaml.Document, error) {
	b, e := catalogfs.ReadFile(path)
	if e != nil {
		return nil, e
	}
	d, e := catalogyaml.Parse(b)
	if e != nil {
		return nil, e
	}
	var value any
	if e = d.Root().Decode(&value); e != nil {
		return nil, e
	}
	return d, nil
}

func hasAnchors(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	if n.Anchor != "" || n.Kind == yaml.AliasNode {
		return true
	}
	for _, c := range n.Content {
		if hasAnchors(c) {
			return true
		}
	}
	return false
}
func scalar(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
func keys[V any](m map[string]V) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func component(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\:\x00") && !strings.HasPrefix(s, ".") && !strings.HasPrefix(s, "_") && filepath.Base(s) == s
}
func within(parent, path string) bool {
	rel, e := filepath.Rel(parent, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func fileHash(path string) (string, error) {
	f, e := catalogfs.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func treeHashes(root string) (map[string]string, error) {
	r := map[string]string{}
	seen := map[string]bool{}
	err := catalogfs.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not supported: %s", path)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular file: %s", path)
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return fmt.Errorf("case-insensitive collision: %s", rel)
		}
		seen[key] = true
		r[rel], e = fileHash(path)
		return e
	})
	return r, err
}
func writeNew(path string, data []byte) error {
	if e := catalogfs.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := catalogfs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return e
	}
	_, e = f.Write(data)
	c := f.Close()
	if e != nil {
		return e
	}
	return c
}
func copyFile(from, to string) error {
	f, e := catalogfs.Open(from)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = catalogfs.MkdirAll(filepath.Dir(to), 0755); e != nil {
		return e
	}
	o, e := catalogfs.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	_, e = io.Copy(o, f)
	c := o.Close()
	if e != nil {
		return e
	}
	return c
}
