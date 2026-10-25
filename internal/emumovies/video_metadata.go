package emumovies

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

// A resumed pass adds only successful downloads; earlier video/artwork matches
// and recovery evidence belong to the catalog, not to this acquisition batch.
func appendVideoSourceMatches(path, body string) error {
	contents, err := catalogfs.ReadFile(path)
	if os.IsNotExist(err) {
		contents = []byte("{}\n")
	} else if err != nil {
		return err
	}
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return err
	}
	fragment, err := catalogyaml.Parse([]byte("sources:\n  emumovies:\n" + body))
	if err != nil {
		return err
	}
	generated := catalogyaml.Value(catalogyaml.Value(catalogyaml.Value(fragment.Root(), "sources"), sourceName), "matches")
	sources, err := catalogyaml.EnsureMapping(document.Root(), "sources")
	if err != nil {
		return err
	}
	source, err := catalogyaml.EnsureMapping(sources, sourceName)
	if err != nil {
		return err
	}
	matches := catalogyaml.Value(source, "matches")
	if matches == nil || matches.Tag == "!!null" {
		matches = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if err = catalogyaml.Set(source, "matches", matches); err != nil {
			return err
		}
	}
	if matches.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s: sources.emumovies.matches must be a sequence", path)
	}
	known := map[string]bool{}
	for _, match := range matches.Content {
		if key := catalogyaml.Scalar(catalogyaml.Value(match, "matched_path")); key != "" {
			known[key] = true
		}
	}
	added := 0
	for _, match := range generated.Content {
		key := catalogyaml.Scalar(catalogyaml.Value(match, "matched_path"))
		if key == "" {
			return fmt.Errorf("generated EmuMovies match has no source path")
		}
		if known[key] {
			continue
		}
		matches.Content = append(matches.Content, match)
		known[key] = true
		added++
	}
	if added == 0 {
		return nil
	}
	updated, err := document.Marshal()
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, updated, 0o644)
}
