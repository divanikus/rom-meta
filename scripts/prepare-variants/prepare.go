package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/catalog"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

const receiptName = ".variant-preparation.json"

type copyFile struct {
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	source       string
	info         os.FileInfo
	data         []byte
}
type checkedInput struct {
	path string
	info os.FileInfo
}
type bundle struct {
	game, fp *catalogyaml.Document
	files    map[string]*copyFile
	entries  []entry
}
type report struct {
	SchemaVersion  int               `json:"schema_version"`
	ManifestSHA256 string            `json:"manifest_sha256"`
	Media          string            `json:"media"`
	Status         string            `json:"status"`
	Entries        int               `json:"entries"`
	Bundles        int               `json:"bundles"`
	Added          int               `json:"added"`
	Existing       int               `json:"existing"`
	Bytes          int64             `json:"bytes"`
	SkippedMedia   int               `json:"skipped_media"`
	Files          []*copyFile       `json:"files"`
	Holds          []json.RawMessage `json:"holds,omitempty"`
	Errors         []string          `json:"errors,omitempty"`
	Validation     []catalog.Issue   `json:"validation,omitempty"`
}

func progress(o options, format string, args ...any) {
	if o.Progress != nil {
		fmt.Fprintf(o.Progress, format+"\n", args...)
	}
}

func run(o options) (result *report, failure error) {
	defer func() {
		if failure != nil && result != nil && result.Status != "invalid" {
			result.Status = "blocked"
			if len(result.Errors) == 0 {
				result.Errors = append(result.Errors, failure.Error())
			}
		}
	}()
	if err := checkOutput(o); err != nil {
		return nil, err
	}
	if o.Report != "" {
		if _, err := reportPath(o); err != nil {
			return nil, err
		}
	}
	raw, err := os.ReadFile(o.Links)
	if err != nil {
		return nil, err
	}
	m, err := decodeManifest(raw)
	if err != nil {
		return nil, err
	}
	r := &report{SchemaVersion: 1, ManifestSHA256: digest(raw), Media: o.Media, Status: "preview", Entries: len(m.Entries), Holds: m.Holds}
	if err := checkSnapshots(m, o); err != nil {
		r.Errors = append(r.Errors, err.Error())
		r.Status = "blocked"
		return r, err
	}
	bundles := map[string]*bundle{}
	files := map[string]*copyFile{}
	var inputs []checkedInput
	for i, e := range m.Entries {
		if i%50 == 0 {
			progress(o, "Checking relationships and payloads: %d/%d", i, len(m.Entries))
		}
		label := fmt.Sprintf("%s/%s/%s", e.Platform, e.Game, e.Variant.Key)
		fail := func(err error) { r.Errors = append(r.Errors, label+": "+err.Error()) }
		if err := validateEntry(e); err != nil {
			fail(err)
			continue
		}
		if err := checkEvidenceSnapshots(m, e); err != nil {
			fail(err)
			continue
		}
		key := filepath.Join("platforms", e.Platform, e.Game)
		b := bundles[key]
		if b == nil {
			b = &bundle{files: map[string]*copyFile{}}
			for _, name := range []string{"game.yaml", "fingerprints.yaml"} {
				rel := filepath.Join(key, name)
				cf, err := planFile(o.Catalog, rel, true)
				if err != nil {
					fail(err)
					break
				}
				doc, err := catalogyaml.Parse(cf.data)
				if err != nil {
					fail(err)
					break
				}
				if err = rejectDuplicateYAML(doc.Root()); err != nil {
					fail(err)
					break
				}
				b.files[name] = cf
				if name == "game.yaml" {
					b.game = doc
				} else {
					b.fp = doc
				}
			}
			if b.game == nil || b.fp == nil {
				continue
			}
			bundles[key] = b
		}
		stale := false
		for name, hash := range e.Expected {
			if !strings.EqualFold(b.files[name].SourceSHA256, hash) {
				fail(fmt.Errorf("catalog document changed: %s", name))
				stale = true
			}
		}
		if stale {
			continue
		}
		valid := true
		for _, in := range e.Inputs {
			info, err := verifyInput(o.ROMRoot, in, e.File)
			if err != nil {
				fail(err)
				valid = false
				continue
			}
			p, err := regularInput(o.ROMRoot, in.Path)
			if err != nil {
				fail(err)
				valid = false
				continue
			}
			inputs = append(inputs, checkedInput{p, info})
		}
		if !valid {
			continue
		}
		changed, err := addEntry(b.game, b.fp, e)
		if err != nil {
			fail(err)
			continue
		}
		b.entries = append(b.entries, e)
		if changed {
			r.Added++
		} else {
			r.Existing++
		}
	}
	r.Bundles = len(bundles)
	keys := make([]string, 0, len(bundles))
	for key := range bundles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b := bundles[key]
		if err := checkBundle(b.game, b.fp); err != nil {
			r.Errors = append(r.Errors, key+": "+err.Error())
			continue
		}
		for _, e := range b.entries {
			if err := checkBaseEvidence(b.fp, e); err != nil {
				r.Errors = append(r.Errors, key+": "+err.Error())
			}
		}
		for name, doc := range map[string]*catalogyaml.Document{"game.yaml": b.game, "fingerprints.yaml": b.fp} {
			cf := b.files[name]
			output, err := doc.Marshal()
			if err != nil {
				return r, err
			}
			// Existing exact definitions do not cause cosmetic rewrites.
			before, err := catalogyaml.Parse(cf.data)
			if err != nil {
				return r, err
			}
			if !sameYAML(before.Root(), doc.Root()) {
				cf.data = output
			}
			cf.Size = int64(len(cf.data))
			cf.SHA256 = digest(cf.data)
			files[cf.Path] = cf
		}
	}
	if err := checkOwnership(bundles); err != nil {
		r.Errors = append(r.Errors, err.Error())
	}
	if len(r.Errors) > 0 {
		r.Status = "blocked"
		return r, fmt.Errorf("%d preparation errors; no staging catalog written", len(r.Errors))
	}
	// Only selected bundles and their descriptive platform/policy files are copied.
	// Configuration/secrets, indexes, source revision markers and other games stay out.
	platforms := map[string]bool{}
	for _, e := range m.Entries {
		platforms[e.Platform] = true
	}
	for platform := range platforms {
		for _, name := range []string{"_platform.yaml", "_catalog.yaml"} {
			rel := filepath.Join("platforms", platform, name)
			cf, err := planFile(o.Catalog, rel, true)
			if err != nil {
				return r, err
			}
			files[cf.Path] = cf
		}
	}
	policy, err := planFile(o.Catalog, "source-policies.yaml", true)
	if err == nil {
		files[policy.Path] = policy
	} else if !os.IsNotExist(err) {
		return r, err
	}
	for i, key := range keys {
		if i%50 == 0 {
			progress(o, "Inventorying selected bundles: %d/%d", i, len(keys))
		}
		err := catalogfs.WalkDir(filepath.Join(o.Catalog, key), func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(o.Catalog, name)
			if err != nil {
				return err
			}
			local, err := filepath.Rel(filepath.Join(o.Catalog, key), name)
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlink in source bundle: %s", rel)
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") {
					return fs.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if _, ok := files[filepath.ToSlash(rel)]; ok {
				return nil
			}
			media := strings.HasPrefix(filepath.ToSlash(local), "media/")
			if media && !includeMedia(local, o.Media) {
				r.SkippedMedia++
				return nil
			}
			cf, err := planFile(o.Catalog, rel, !media)
			if err != nil {
				return err
			}
			files[cf.Path] = cf
			return nil
		})
		if err != nil {
			return r, err
		}
	}
	for _, cf := range files {
		r.Files = append(r.Files, cf)
		r.Bytes += cf.Size
	}
	sort.Slice(r.Files, func(i, j int) bool { return r.Files[i].Path < r.Files[j].Path })
	progress(o, "Prepared %d entries in %d bundles; %d files / %d bytes", r.Entries, r.Bundles, len(r.Files), r.Bytes)
	if !o.Apply {
		return r, nil
	}
	if _, err := os.Lstat(o.Out); err == nil {
		if err := checkRepeat(o, r); err != nil {
			return r, err
		}
		r.Status = "unchanged"
		return r, nil
	} else if !os.IsNotExist(err) {
		return r, err
	}
	if err := checkPreconditions(o, r, inputs); err != nil {
		return r, err
	}
	out, err := resolved(o.Out)
	if err != nil {
		return r, err
	}
	if err = os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return r, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(out), "."+filepath.Base(out)+"-pending-")
	if err != nil {
		return r, err
	}
	// A failed private stage is retained for diagnosis; never remove a user tree.
	if err := materialize(o, r, stage); err != nil {
		return r, fmt.Errorf("%w; unfinished staging retained at %s", err, stage)
	}
	if err := checkPreconditions(o, r, inputs); err != nil {
		return r, fmt.Errorf("%w; unpublished staging at %s", err, stage)
	}
	if err := checkSnapshots(m, o); err != nil {
		return r, fmt.Errorf("%w; unpublished staging at %s", err, stage)
	}
	r.Validation, err = catalog.Validate(stage)
	if err != nil {
		return r, fmt.Errorf("validate %s: %w", stage, err)
	}
	if len(r.Validation) > 0 {
		r.Status = "invalid"
		return r, fmt.Errorf("%d validation issues; staging retained at %s", len(r.Validation), stage)
	}
	r.Status = "staged"
	receipt, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return r, err
	}
	if err = os.WriteFile(filepath.Join(stage, receiptName), append(receipt, '\n'), 0600); err != nil {
		return r, err
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		return r, fmt.Errorf("output appeared; unpublished staging at %s", stage)
	}
	if err = os.Rename(stage, out); err != nil {
		return r, err
	}
	return r, nil
}

func planFile(root, relative string, read bool) (*copyFile, error) {
	if !safeRelative(filepath.ToSlash(relative)) {
		return nil, fmt.Errorf("unsafe catalog file: %s", relative)
	}
	name := filepath.Join(root, relative)
	real, err := catalogfs.EvalSymlinks(name)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if !within(abs, real) {
		return nil, fmt.Errorf("catalog file escapes source: %s", relative)
	}
	info, err := catalogfs.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("nonregular catalog file: %s", relative)
	}
	cf := &copyFile{Path: filepath.ToSlash(relative), Size: info.Size(), source: name, info: info}
	if read {
		cf.data, err = catalogfs.ReadFile(name)
		if err != nil {
			return nil, err
		}
		after, err := catalogfs.Stat(name)
		if err != nil {
			return nil, err
		}
		if !catalogfs.SameFile(info, after) {
			return nil, fmt.Errorf("catalog file changed during read: %s", relative)
		}
		cf.SourceSHA256 = digest(cf.data)
		cf.SHA256 = cf.SourceSHA256
	}
	return cf, nil
}
func includeMedia(name, policy string) bool {
	if policy == "all" {
		return true
	}
	if policy == "none" {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return true
	}
	return false
}
func checkPreconditions(o options, r *report, inputs []checkedInput) error {
	for _, in := range inputs {
		after, err := os.Stat(in.path)
		if err != nil {
			return err
		}
		if !sameStat(in.info, after) {
			return fmt.Errorf("ROM changed after planning: %s", in.path)
		}
	}
	for _, cf := range r.Files {
		info, err := catalogfs.Stat(cf.source)
		if err != nil {
			return err
		}
		if !catalogfs.SameFile(cf.info, info) {
			return fmt.Errorf("source changed after planning: %s", cf.Path)
		}
		if cf.SourceSHA256 != "" {
			data, err := catalogfs.ReadFile(cf.source)
			if err != nil {
				return err
			}
			if digest(data) != cf.SourceSHA256 {
				return fmt.Errorf("source bytes changed: %s", cf.Path)
			}
		}
	}
	return nil
}
func materialize(o options, r *report, stage string) error {
	for i, cf := range r.Files {
		if i%200 == 0 {
			progress(o, "Copying staging files: %d/%d", i, len(r.Files))
		}
		target := filepath.Join(stage, filepath.FromSlash(cf.Path))
		if !within(stage, target) {
			return fmt.Errorf("unsafe output path")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if cf.data != nil {
			if err := os.WriteFile(target, cf.data, 0644); err != nil {
				return err
			}
			continue
		}
		f, err := catalogfs.Open(cf.source)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			f.Close()
			return err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(out, h), f)
		ce := out.Close()
		fe := f.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
		if fe != nil {
			return fe
		}
		if n != cf.Size {
			return fmt.Errorf("source size changed: %s", cf.Path)
		}
		cf.SHA256 = fmt.Sprintf("%x", h.Sum(nil))
	}
	return nil
}
func checkRepeat(o options, r *report) error {
	info, err := os.Lstat(o.Out)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("output must be a separate regular directory")
	}
	raw, err := os.ReadFile(filepath.Join(o.Out, receiptName))
	if err != nil {
		return fmt.Errorf("existing output lacks helper receipt: %w", err)
	}
	var old report
	if err = json.Unmarshal(raw, &old); err != nil {
		return err
	}
	if old.Status != "staged" || old.ManifestSHA256 != r.ManifestSHA256 || old.Media != r.Media || len(old.Files) != len(r.Files) {
		return fmt.Errorf("output belongs to a different preparation; choose a new directory")
	}
	for i, cf := range r.Files {
		saved := old.Files[i]
		if saved.Path != cf.Path || cf.SHA256 != "" && saved.SHA256 != cf.SHA256 {
			return fmt.Errorf("existing staging differs from planned output")
		}
		name, err := regularInput(o.Out, cf.Path)
		if err != nil {
			return err
		}
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(h, f)
		ce := f.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
		if n != saved.Size || fmt.Sprintf("%x", h.Sum(nil)) != saved.SHA256 {
			return fmt.Errorf("staging file changed: %s", cf.Path)
		}
		cf.SHA256 = saved.SHA256
	}
	return nil
}
