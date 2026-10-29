package sourceyaml

import (
	"errors"
	"fmt"
	"os"

	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
)

// Samba can persist Hidden on dot-prefixed temporary files after their rename.
const metadataTempPattern = atomicfile.TempPattern

// ErrSourcePreserved prevents a late/direct writer from replacing owner-reviewed data.
var ErrSourcePreserved = errors.New("source metadata is preserved by fetch.preserve_sources")

// WriteSourceMetadata atomically inserts or replaces one unpreserved source stanza.
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
	preserved, err := catalogyaml.SourcePreserved(document.Root(), source)
	if err != nil {
		return err
	}
	if preserved {
		return fmt.Errorf("%s: %w", source, ErrSourcePreserved)
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
