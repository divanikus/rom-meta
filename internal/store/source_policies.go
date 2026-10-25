package store

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"rommeta/internal/catalogfs"
)

// SourcePoliciesPath returns the root-level source policy file path.
func SourcePoliciesPath(dataRoot string) string {
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	return filepath.Join(dataRoot, "source-policies.yaml")
}

// EnsureSourcePolicy creates or updates the root-level policy for a media source.
func EnsureSourcePolicy(dataRoot, source, scope, note string) (string, error) {
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	absRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return "", err
	}
	path := SourcePoliciesPath(absRoot)
	contents := ""
	if existing, err := catalogfs.ReadFile(path); err == nil {
		contents = string(existing)
	} else if !os.IsNotExist(err) {
		return "", err
	}

	stanza := sourcePolicyStanza(source, scope, note)
	updated := upsertSourcePolicyStanza(contents, source, stanza)
	if err := catalogfs.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func sourcePolicyStanza(source, scope, note string) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(source)
	b.WriteString(":\n")
	b.WriteString("    media:\n")
	b.WriteString("      redistributable: false\n")
	if strings.TrimSpace(scope) != "" {
		b.WriteString("      scope: ")
		b.WriteString(scope)
		b.WriteString("\n")
	}
	if strings.TrimSpace(note) != "" {
		b.WriteString("      note: ")
		b.WriteString(strconv.Quote(note))
		b.WriteString("\n")
	}
	return b.String()
}

func upsertSourcePolicyStanza(contents, source, stanza string) string {
	if strings.TrimSpace(contents) == "" {
		return defaultSourcePoliciesConfigWith(stanza)
	}

	lines := strings.Split(strings.TrimRight(contents, "\n"), "\n")
	sourcesStart := -1
	for i, line := range lines {
		if countIndent(line) == 0 && strings.TrimSpace(line) == "sources:" {
			sourcesStart = i
			break
		}
	}
	if sourcesStart == -1 {
		return strings.TrimRight(contents, "\n") + "\n\nsources:\n" + stanza
	}

	insertAt := len(lines)
	for i := sourcesStart + 1; i < len(lines); i++ {
		value := strings.TrimSpace(lines[i])
		if value == "" {
			continue
		}
		indent := countIndent(lines[i])
		if indent == 0 {
			insertAt = i
			break
		}
		if indent == 2 && strings.HasSuffix(value, ":") && strings.TrimSuffix(value, ":") == source {
			end := i + 1
			for end < len(lines) {
				next := strings.TrimSpace(lines[end])
				if next != "" && countIndent(lines[end]) <= 2 {
					break
				}
				end++
			}
			replacement := strings.Split(strings.TrimRight(stanza, "\n"), "\n")
			updated := append([]string{}, lines[:i]...)
			updated = append(updated, replacement...)
			updated = append(updated, lines[end:]...)
			return strings.Join(updated, "\n") + "\n"
		}
	}

	replacement := strings.Split(strings.TrimRight(stanza, "\n"), "\n")
	updated := append([]string{}, lines[:insertAt]...)
	updated = append(updated, replacement...)
	updated = append(updated, lines[insertAt:]...)
	return strings.Join(updated, "\n") + "\n"
}

func defaultSourcePoliciesConfigWith(stanza string) string {
	return `schema_version: 0

default:
  media:
    redistributable: false
    scope: review

sources:
` + stanza
}

func countIndent(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' {
			return count
		}
		count++
	}
	return count
}
