// Package catalogedit applies explicit local corrections while retaining source evidence.
package catalogedit

import (
	"crypto/sha256"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"regexp"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"sort"
	"strings"
	"time"
)

var Fields = []string{"title", "aliases", "alternate_titles", "description", "descriptions", "release_date", "developer", "publisher", "genres", "players"}

type Patch map[string]any
type Card struct {
	Revision string         `json:"revision"`
	Fields   map[string]any `json:"fields"`
	YAML     string         `json:"yaml"`
}

func Editable(key string) bool {
	for _, field := range Fields {
		if key == field {
			return true
		}
	}
	return false
}
func Hash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func Component(s string) bool {
	return s != "" && s == strings.TrimSpace(s) && s != "." && s != ".." && !strings.HasSuffix(s, ".") && !strings.ContainsAny(s, "/\\:<>\"|?*\x00") && !strings.HasPrefix(s, ".")
}
func GamePath(root, platform, folder string) (string, error) {
	if !Component(platform) || !Component(folder) {
		return "", fmt.Errorf("invalid platform or game folder")
	}
	base, err := catalogfs.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "platforms", platform, folder)
	real, err := catalogfs.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	inside := func(base, path string) bool {
		rel, e := filepath.Rel(base, path)
		return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	if !inside(base, real) {
		return "", fmt.Errorf("game path escapes catalog")
	}
	for _, name := range []string{"game.yaml", "fingerprints.yaml"} {
		resolved, e := catalogfs.EvalSymlinks(filepath.Join(path, name))
		if os.IsNotExist(e) && name == "fingerprints.yaml" {
			continue
		}
		if e != nil {
			return "", e
		}
		if !inside(real, resolved) {
			return "", fmt.Errorf("document path escapes game")
		}
	}
	return path, nil
}
func Inspect(data []byte) (Card, error) {
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		return Card{}, err
	}
	card := Card{Revision: Hash(data), Fields: map[string]any{}, YAML: string(data)}
	for _, key := range Fields {
		if node := catalogyaml.Value(doc.Root(), key); node != nil {
			var value any
			if err := node.Decode(&value); err != nil {
				return Card{}, err
			}
			card.Fields[key] = value
		}
	}
	return card, nil
}
func Read(root, platform, folder string) (Card, error) {
	path, err := GamePath(root, platform, folder)
	if err != nil {
		return Card{}, err
	}
	data, err := catalogfs.ReadFile(filepath.Join(path, "game.yaml"))
	if err != nil {
		return Card{}, err
	}
	return Inspect(data)
}

var language = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$`)
var date = regexp.MustCompile(`^\d{4}(-\d{2}(-\d{2})?)?$`)

func validDate(value string) bool {
	if !date.MatchString(value) {
		return false
	}
	layout := map[int]string{4: "2006", 7: "2006-01", 10: "2006-01-02"}[len(value)]
	_, err := time.Parse(layout, value)
	return err == nil
}

func Apply(data []byte, patch Patch) ([]byte, error) {
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(patch))
	for key := range patch {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := patch[key]
		if !Editable(key) {
			return nil, fmt.Errorf("field %q cannot be edited here", key)
		}
		if value == nil {
			if key == "title" {
				return nil, fmt.Errorf("title cannot be empty")
			}
			Delete(doc.Root(), key)
			continue
		}
		switch key {
		case "aliases", "alternate_titles", "genres":
			values, ok := value.([]any)
			if !ok {
				if list, yes := value.([]string); yes {
					values = make([]any, len(list))
					for i, v := range list {
						values[i] = v
					}
					ok = true
				}
			}
			if !ok || len(values) > 256 {
				return nil, fmt.Errorf("%s must be a list of at most 256 names", key)
			}
			clean := []string{}
			seen := map[string]bool{}
			for _, item := range values {
				v, ok := item.(string)
				if !ok || len(v) > 4096 {
					return nil, fmt.Errorf("invalid %s value", key)
				}
				v = strings.TrimSpace(v)
				if v != "" && !seen[v] {
					clean = append(clean, v)
					seen[v] = true
				}
			}
			value = clean
		case "descriptions":
			values, ok := value.(map[string]any)
			if !ok || len(values) > 64 {
				return nil, fmt.Errorf("descriptions must map languages to text")
			}
			clean := map[string]string{}
			for lang, item := range values {
				v, ok := item.(string)
				if !ok || !language.MatchString(lang) || len(v) > 1024*1024 {
					return nil, fmt.Errorf("invalid description language or text")
				}
				if strings.TrimSpace(v) != "" {
					clean[lang] = v
				}
			}
			value = clean
		default:
			v, ok := value.(string)
			if !ok || len(v) > 1024*1024 {
				return nil, fmt.Errorf("%s must be text", key)
			}
			if key == "title" && (strings.TrimSpace(v) == "" || len(v) > 1024) {
				return nil, fmt.Errorf("enter a title (up to 1024 bytes)")
			}
			if key == "release_date" && v != "" && !validDate(v) {
				return nil, fmt.Errorf("use YYYY, YYYY-MM or YYYY-MM-DD for the date")
			}
			if strings.TrimSpace(v) == "" {
				Delete(doc.Root(), key)
				continue
			}
			value = v
		}
		node := &yaml.Node{}
		if err := node.Encode(value); err != nil {
			return nil, err
		}
		if err := catalogyaml.Set(doc.Root(), key, node); err != nil {
			return nil, err
		}
	}
	if _, err := catalogyaml.Variants(doc.Root()); err != nil {
		return nil, err
	}
	return doc.Marshal()
}
func Delete(node *yaml.Node, key string) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}
func Save(root, platform, folder, revision string, patch Patch) (Card, error) {
	path, err := GamePath(root, platform, folder)
	if err != nil {
		return Card{}, err
	}
	file := filepath.Join(path, "game.yaml")
	data, err := catalogfs.ReadFile(file)
	if err != nil {
		return Card{}, err
	}
	if revision == "" || Hash(data) != revision {
		return Card{}, fmt.Errorf("card changed since it was opened; reload before saving")
	}
	updated, err := Apply(data, patch)
	if err != nil {
		return Card{}, err
	}
	if err := atomicfile.WriteFile(file, updated, 0644); err != nil {
		return Card{}, err
	}
	return Inspect(updated)
}
