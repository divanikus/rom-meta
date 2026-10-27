package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
)

func checkBaseEvidence(fp *catalogyaml.Document, e entry) error {
	if e.Evidence.Relationship != "source-base-hash" {
		return nil
	}
	items := catalogyaml.Value(fp.Root(), "items")
	if items == nil {
		items = catalogyaml.Value(fp.Root(), "roms")
	}
	anyMatched := false
	for _, link := range e.Evidence.Links {
		base, ok := link["base"].(map[string]any)
		if !ok {
			continue
		}
		strong := false
		for key, n := range map[string]int{"sha1": 20, "sha256": 32, "md5": 16, "crc32": 4} {
			value, _ := base[key].(string)
			if value != "" && !validHash(value, n) {
				return fmt.Errorf("invalid source base hash")
			}
			if (key == "sha1" || key == "sha256") && value != "" {
				strong = true
			}
		}
		if !strong {
			continue
		}
		matched := false
		err := catalogyaml.Sequence(items, func(item *yaml.Node) error {
			if catalogyaml.Scalar(catalogyaml.Value(item, "release")) != e.Variant.BasedOn {
				return nil
			}
			file := catalogyaml.Value(item, "file")
			if catalogyaml.Scalar(catalogyaml.Value(file, "kind")) != "" {
				return nil
			}
			agree := true
			for _, key := range []string{"sha1", "sha256", "md5", "crc32"} {
				want, _ := base[key].(string)
				if want == "" {
					continue
				}
				got := catalogyaml.Scalar(catalogyaml.Value(file, key))
				if !strings.EqualFold(want, got) {
					agree = false
				}
			}
			matched = matched || agree
			return nil
		})
		if err != nil {
			return err
		}
		if !matched {
			return fmt.Errorf("source base hashes conflict with the selected official release")
		}
		anyMatched = true
	}
	if anyMatched {
		return nil
	}
	return fmt.Errorf("source-base-hash requires source strong hashes agreeing with the exact official base fingerprint")
}

func checkOwnership(bundles map[string]*bundle) error {
	owners := map[string]string{}
	modified := map[string]bool{}
	for key, b := range bundles {
		items := catalogyaml.Value(b.fp.Root(), "items")
		if items == nil {
			items = catalogyaml.Value(b.fp.Root(), "roms")
		}
		err := catalogyaml.Sequence(items, func(item *yaml.Node) error {
			v := catalogyaml.Scalar(catalogyaml.Value(item, "variant"))
			owner := key + "\x00release:" + catalogyaml.Scalar(catalogyaml.Value(item, "release"))
			if v != "" {
				owner = key + "\x00variant:" + v
			}
			file := catalogyaml.Value(item, "file")
			kind := catalogyaml.Scalar(catalogyaml.Value(file, "kind"))
			size := catalogyaml.Int64(catalogyaml.Value(file, "size"))
			for _, hash := range []string{"sha1", "sha256"} {
				text := strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(file, hash)))
				if text == "" {
					continue
				}
				identity := fmt.Sprintf("%s:%d:%s:%s", kind, size, hash, text)
				if old, ok := owners[identity]; ok && old != owner && (modified[identity] || v != "") {
					return fmt.Errorf("modified fingerprint has conflicting ownership across selected bundles")
				}
				owners[identity] = owner
				modified[identity] = modified[identity] || v != ""
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func checkEvidenceSnapshots(m *manifest, e entry) error {
	for _, link := range e.Evidence.Links {
		raw, hasPath := link["snapshot"]
		hash, hasHash := link["snapshot_sha256"]
		if !hasPath && !hasHash {
			continue
		}
		name, ok := raw.(string)
		want, hashOK := hash.(string)
		if !ok || !hashOK || !validHash(want, 32) {
			return fmt.Errorf("source snapshot link needs path and SHA256")
		}
		found := false
		for _, s := range m.Snapshots {
			if s.Path == name && strings.EqualFold(want, s.SHA256) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("source link references an unpinned or conflicting snapshot")
		}
	}
	return nil
}
