package catalogyaml

import (
	"encoding/hex"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Variant describes an explicitly prepared derivative, never an official release.
type Variant struct {
	Name      string   `json:"name"`
	Title     string   `json:"title,omitempty"`
	Kind      string   `json:"kind"`
	BasedOn   string   `json:"based_on"`
	Languages []string `json:"languages,omitempty"`
	Group     string   `json:"group,omitempty"`
	Version   string   `json:"version,omitempty"`
}

// Variants validates and returns the same-bundle derivative graph in source order.
func Variants(root *yaml.Node) ([]Variant, error) {
	var result []Variant
	if Value(root, "variants") == nil {
		return nil, nil
	}
	for _, section := range []string{"releases", "variants"} {
		count := 0
		if err := Mapping(root, func(key string, _ *yaml.Node) error {
			if key == section {
				count++
			}
			return nil
		}); err != nil {
			return nil, err
		}
		if count > 1 {
			return nil, fmt.Errorf("duplicate %s section", section)
		}
		keys := map[string]bool{}
		if err := Mapping(Value(root, section), func(key string, _ *yaml.Node) error {
			if keys[key] {
				return fmt.Errorf("duplicate %s key %q", section, key)
			}
			keys[key] = true
			return nil
		}); err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	releases := Value(root, "releases")
	err := Mapping(Value(root, "variants"), func(name string, node *yaml.Node) error {
		if strings.TrimSpace(name) == "" || seen[name] {
			return fmt.Errorf("empty or duplicate variant key %q", name)
		}
		if Value(releases, name) != nil {
			return fmt.Errorf("variant %q collides with an official release key", name)
		}
		seen[name] = true
		if node == nil || node.Kind != yaml.MappingNode {
			return fmt.Errorf("variant %q must be a mapping", name)
		}
		fields := map[string]bool{}
		if err := Mapping(node, func(field string, _ *yaml.Node) error {
			if fields[field] {
				return fmt.Errorf("variant %q has duplicate field %q", name, field)
			}
			fields[field] = true
			return nil
		}); err != nil {
			return err
		}
		for _, field := range []string{"kind", "based_on"} {
			value := Value(node, field)
			if value == nil || value.Kind != yaml.ScalarNode || value.Tag != "!!str" || strings.TrimSpace(value.Value) == "" {
				return fmt.Errorf("variant %q requires a nonempty %s", name, field)
			}
		}
		result = append(result, Variant{
			Name: name, Title: Scalar(Value(node, "title")),
			Kind: Scalar(Value(node, "kind")), BasedOn: Scalar(Value(node, "based_on")),
			Languages: Strings(Value(node, "languages")), Group: Scalar(Value(node, "group")),
			Version: Scalar(Value(node, "version")),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	byName := map[string]Variant{}
	for _, variant := range result {
		byName[variant.Name] = variant
	}
	for _, variant := range result {
		if _, _, err := variantChain(releases, byName, variant.Name); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// VariantChain returns the requested variant followed by parent variants and the
// terminal official release. It never substitutes default_release or guesses names.
func VariantChain(root *yaml.Node, name string) ([]string, string, error) {
	variants, err := Variants(root)
	if err != nil {
		return nil, "", err
	}
	byName := map[string]Variant{}
	for _, variant := range variants {
		byName[variant.Name] = variant
	}
	return variantChain(Value(root, "releases"), byName, name)
}

func variantChain(releases *yaml.Node, variants map[string]Variant, name string) ([]string, string, error) {
	var chain []string
	seen := map[string]bool{}
	current := name
	for {
		variant, ok := variants[current]
		if !ok {
			return nil, "", fmt.Errorf("variant %q references missing base %q", name, current)
		}
		if seen[current] {
			return nil, "", fmt.Errorf("variant %q has a based_on cycle at %q", name, current)
		}
		seen[current] = true
		chain = append(chain, current)
		if Value(releases, variant.BasedOn) != nil {
			return chain, variant.BasedOn, nil
		}
		current = variant.BasedOn
	}
}

// FingerprintTarget enforces exclusive release/variant ownership and references.
func FingerprintTarget(root, item *yaml.Node) (release, variant string, err error) {
	release, variant = Scalar(Value(item, "release")), Scalar(Value(item, "variant"))
	if (release == "") == (variant == "") {
		return "", "", fmt.Errorf("fingerprint must reference exactly one release or variant")
	}
	if release != "" && Value(Value(root, "releases"), release) == nil {
		return "", "", fmt.Errorf("fingerprint references unknown release %q", release)
	}
	file := Value(item, "file")
	kind := Scalar(Value(file, "kind"))
	if kind == "chd-combined-sha1" {
		hash := Scalar(Value(file, "sha1"))
		decoded, e := hex.DecodeString(hash)
		if variant == "" || e != nil || len(decoded) != 20 || strings.Trim(hash, "0") == "" || Int64(Value(file, "size")) < 0 || Scalar(Value(file, "md5")) != "" || Scalar(Value(file, "sha256")) != "" || Scalar(Value(file, "crc32")) != "" {
			return "", "", fmt.Errorf("CHD combined identity requires a variant and a valid combined SHA1, separate from payload/file hashes")
		}
	}
	if variant != "" {
		if kind != "" && kind != "chd-combined-sha1" {
			return "", "", fmt.Errorf("unsupported variant fingerprint kind %q", kind)
		}
		if kind == "" {
			valid := false
			for _, candidate := range []struct {
				name string
				size int
			}{{"sha1", 20}, {"sha256", 32}} {
				text := Scalar(Value(file, candidate.name))
				if text == "" {
					continue
				}
				decoded, e := hex.DecodeString(text)
				if e != nil || len(decoded) != candidate.size {
					return "", "", fmt.Errorf("invalid variant %s", candidate.name)
				}
				valid = true
			}
			if Int64(Value(file, "size")) <= 0 || !valid {
				return "", "", fmt.Errorf("variant fingerprint requires size and a strong payload hash")
			}
		}
		if _, _, err = VariantChain(root, variant); err != nil {
			return "", "", err
		}
	}
	return release, variant, nil
}
