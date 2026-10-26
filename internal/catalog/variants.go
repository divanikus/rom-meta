package catalog

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

func validateFingerprints(root, path string, game *yaml.Node, issues *[]Issue) {
	raw, err := catalogfs.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		addIssue(root, issues, path, err.Error())
		return
	}
	doc, err := catalogyaml.Parse(raw)
	if err != nil {
		addIssue(root, issues, path, err.Error())
		return
	}
	items := catalogyaml.Value(doc.Root(), "items")
	if items == nil {
		items = catalogyaml.Value(doc.Root(), "roms")
	}
	owners := map[string]string{}
	err = catalogyaml.Sequence(items, func(item *yaml.Node) error {
		release, variant, err := catalogyaml.FingerprintTarget(game, item)
		if err != nil {
			addIssue(root, issues, path, err.Error())
			return nil
		}
		file := catalogyaml.Value(item, "file")
		size := catalogyaml.Int64(catalogyaml.Value(file, "size"))
		sha1 := strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(file, "sha1")))
		sha256 := strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(file, "sha256")))
		kind := catalogyaml.Scalar(catalogyaml.Value(file, "kind"))
		if variant != "" && (size <= 0 || (sha1 == "" && sha256 == "")) && kind != "chd-combined-sha1" {
			addIssue(root, issues, path, fmt.Sprintf("variant %q fingerprint requires size and a strong hash", variant))
		}
		owner := "release:" + release
		if variant != "" {
			owner = "variant:" + variant
		}
		for _, digest := range []string{"sha1:" + sha1, "sha256:" + sha256} {
			if strings.HasSuffix(digest, ":") {
				continue
			}
			key := fmt.Sprintf("%s:%d:%s", kind, size, digest)
			if old, ok := owners[key]; ok && old != owner && (variant != "" || strings.HasPrefix(old, "variant:")) {
				addIssue(root, issues, path, "same fingerprint identifies conflicting release/variant targets")
			}
			owners[key] = owner
		}
		return nil
	})
	if err != nil {
		addIssue(root, issues, path, err.Error())
	}
}
