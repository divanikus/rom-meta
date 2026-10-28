package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"rommeta/internal/platformnames"
)

// This reference proves a published modified output. Parent-game assertions are
// separate and may be explicitly reviewed without naming an exact retail base.
type publishedReference struct {
	Format string `json:"format"`
	Path   string `json:"path"`
	Record string `json:"record"`
}

func validatePublishedReference(r publishedReference) error {
	if (r.Format != "romhack-archive" && r.Format != "hackhash") || !safeRelative(r.Path) || !validHash(r.Record, 32) {
		return fmt.Errorf("invalid structured published-output reference")
	}
	return nil
}
func publishedRecordReference(s sourceRecord) *publishedReference {
	format := ""
	switch s.Link["source"] {
	case "Romhack Archive":
		format = "romhack-archive"
	case "HackHash detailed":
		format = "hackhash"
	default:
		return nil
	}
	path, _ := s.Link["snapshot"].(string)
	entry, _ := s.Link["entry"].(string)
	// Bind the source entry and modified output, without claiming retail bytes
	// or an exact patch/credit identity.
	raw, _ := json.Marshal(struct {
		Platform, Entry string
		File            fingerprint
	}{s.Platform, entry, s.Output})
	return &publishedReference{Format: format, Path: path, Record: digest(raw)}
}
func rhaReferenceRoot(path string) (string, error) {
	parts := strings.Split(path, "/")
	if len(parts) < 5 || parts[len(parts)-1] != "romhack.json" || parts[len(parts)-5] != "database" {
		return "", fmt.Errorf("published RHA path must retain database/platform/base/output/romhack.json")
	}
	root := strings.Join(parts[:len(parts)-5], "/")
	if root == "" {
		root = "."
	}
	return root, nil
}
func mergePublishedSnapshots(m *manifest, add []snapshot) error {
	for _, s := range add {
		found := false
		for _, old := range m.Snapshots {
			if old.Path == s.Path {
				if !strings.EqualFold(old.SHA256, s.SHA256) {
					return fmt.Errorf("pinned source changed: %s", s.Path)
				}
				found = true
				break
			}
		}
		if !found {
			m.Snapshots = append(m.Snapshots, s)
		}
	}
	return nil
}
func checkPublishedFile(supplied, output fingerprint) error {
	if (supplied.Name != "" && supplied.Name != output.Name) || supplied.Kind != output.Kind || (supplied.Size != 0 && supplied.Size != output.Size) {
		return fmt.Errorf("supplied output differs from published modified output")
	}
	for _, pair := range [][2]string{{supplied.SHA1, output.SHA1}, {supplied.SHA256, output.SHA256}, {supplied.MD5, output.MD5}, {supplied.CRC32, output.CRC32}} {
		if pair[0] != "" && !strings.EqualFold(pair[0], pair[1]) {
			return fmt.Errorf("supplied hash differs from published modified output")
		}
	}
	return nil
}
func resolvePublished(m *manifest, o options) error {
	cache := map[string]*sourceSet{}
	var registry platformnames.Registry
	registryLoaded := false
	for i := range m.Entries {
		e := &m.Entries[i]
		if e.Published == nil {
			continue
		}
		ref := *e.Published
		if e.DAT != nil {
			return fmt.Errorf("DAT and structured published output cannot be combined")
		}
		if err := validatePublishedReference(ref); err != nil {
			return err
		}
		if len(e.Evidence.Links) == 0 {
			return fmt.Errorf("published output needs an original-game relationship")
		}
		if o.Sources == "" {
			return fmt.Errorf("--sources required for published outputs")
		}
		root := ref.Path
		if ref.Format == "romhack-archive" {
			var err error
			root, err = rhaReferenceRoot(ref.Path)
			if err != nil {
				return err
			}
		}
		key := ref.Format + "\x00" + root
		set := cache[key]
		if set == nil {
			set = &sourceSet{}
			var err error
			if ref.Format == "romhack-archive" {
				err = loadRHA(o, root, set)
			} else {
				if !registryLoaded {
					registry, _, err = platformnames.LoadRegistry(o.Catalog)
					if err != nil {
						return err
					}
					registryLoaded = true
				}
				err = loadHackHash(o, ref.Path, set, &registry)
			}
			if err != nil {
				return err
			}
			cache[key] = set
			if err = mergePublishedSnapshots(m, set.Snapshots); err != nil {
				return err
			}
		}
		matched := false
		for _, s := range set.Records {
			candidate := publishedRecordReference(s)
			if candidate == nil || *candidate != ref {
				continue
			}
			if err := checkPublishedFile(e.File, s.Output); err != nil {
				return err
			}
			e.File = s.Output
			matched = true
			link := map[string]any{}
			for k, v := range s.Link {
				link[k] = v
			}
			link["role"] = "modified-output"
			link["record_id"] = ref.Record
			link["source_parent"] = s.Parent
			if parent, _ := link["parent"].(string); parent == "" {
				link["parent"] = e.Game
			}
			e.Evidence.Links = append(e.Evidence.Links, link)
		}
		if !matched {
			return fmt.Errorf("published modified output is missing or unsupported: %s", ref.Path)
		}
	}
	return nil
}
