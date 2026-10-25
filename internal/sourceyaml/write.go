package sourceyaml

import (
	"fmt"
	"os"

	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

// Samba can persist Hidden on dot-prefixed temporary files after their rename.
const metadataTempPattern = atomicfile.TempPattern

// WriteSourceMetadata atomically inserts or replaces one sources.<source> stanza.
func WriteSourceMetadata(path, source, body string) error {
	contents := "{}\n"
	if existing, err := catalogfs.ReadFile(path); err == nil {
		contents = string(existing)
	} else if !os.IsNotExist(err) {
		return err
	}
	document, err := catalogyaml.Parse([]byte(contents))
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	fragment, err := catalogyaml.Parse([]byte(fmt.Sprintf("sources:\n  %s:\n%s", source, body)))
	if err != nil {
		return fmt.Errorf("parse generated %s source stanza: %w", source, err)
	}
	generatedSource := catalogyaml.Value(catalogyaml.Value(fragment.Root(), "sources"), source)
	if generatedSource == nil {
		return fmt.Errorf("generated %s source stanza is empty", source)
	}
	sources, err := catalogyaml.EnsureMapping(document.Root(), "sources")
	if err != nil {
		return err
	}
	if err := catalogyaml.Set(sources, source, generatedSource); err != nil {
		return err
	}
	updated, err := document.Marshal()
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, updated, 0o644)
}

// UpsertSourceStanza preserves all content outside one top-level source block.
