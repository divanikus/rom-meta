package catalogyaml

import (
	"fmt"
	"strings"
)

// PreserveUnknownTopLevel replaces generated top-level fields while preserving
// all other top-level blocks from an existing human-edited document.
func PreserveUnknownTopLevel(existing, generated string, ownedKeys ...string) (string, error) {
	existing = normalize(existing)
	generated = normalize(generated)
	if strings.TrimSpace(existing) == "" {
		return ensureFinalNewline(generated), nil
	}
	owned := map[string]bool{}
	for _, key := range ownedKeys {
		key = strings.TrimSpace(key)
		if key != "" {
			owned[key] = true
		}
	}
	blocks, preamble, err := splitTopLevel(existing)
	if err != nil {
		return "", err
	}
	var preserved []string
	for _, block := range blocks {
		if !owned[block.key] {
			preserved = append(preserved, strings.Trim(block.raw, "\n"))
		}
	}
	var parts []string
	if strings.TrimSpace(preamble) != "" {
		parts = append(parts, strings.Trim(preamble, "\n"))
	}
	if strings.TrimSpace(generated) != "" {
		parts = append(parts, strings.Trim(generated, "\n"))
	}
	parts = append(parts, preserved...)
	return ensureFinalNewline(strings.Join(parts, "\n\n")), nil
}

// HasAnyTopLevel reports whether a document contains any of the named
// top-level fields.
func HasAnyTopLevel(contents string, keys ...string) (bool, error) {
	blocks, _, err := splitTopLevel(normalize(contents))
	if err != nil {
		return false, err
	}
	wanted := map[string]bool{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			wanted[key] = true
		}
	}
	for _, block := range blocks {
		if wanted[block.key] {
			return true, nil
		}
	}
	return false, nil
}

type topLevelBlock struct {
	key string
	raw string
}

func splitTopLevel(contents string) ([]topLevelBlock, string, error) {
	lines := strings.Split(strings.TrimRight(contents, "\n"), "\n")
	var starts []int
	var keys []string
	for index, line := range lines {
		key, ok := topLevelKey(line)
		if !ok {
			continue
		}
		starts = append(starts, index)
		keys = append(keys, key)
	}
	if len(starts) == 0 {
		return nil, contents, fmt.Errorf("catalog YAML has no top-level fields")
	}
	preamble := strings.Join(lines[:starts[0]], "\n")
	blocks := make([]topLevelBlock, 0, len(starts))
	for index, start := range starts {
		end := len(lines)
		if index+1 < len(starts) {
			end = starts[index+1]
		}
		blocks = append(blocks, topLevelBlock{
			key: keys[index],
			raw: strings.Join(lines[start:end], "\n"),
		})
	}
	return blocks, preamble, nil
}

func topLevelKey(line string) (string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(strings.TrimSpace(line), "#") {
		return "", false
	}
	index := strings.IndexByte(line, ':')
	if index <= 0 {
		return "", false
	}
	key := strings.TrimSpace(line[:index])
	if key == "" {
		return "", false
	}
	return strings.Trim(key, "\"'"), true
}

func normalize(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

func ensureFinalNewline(value string) string {
	return strings.TrimRight(value, "\n") + "\n"
}
