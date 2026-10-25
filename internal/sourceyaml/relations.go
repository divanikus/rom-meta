package sourceyaml

import (
	"fmt"
	"io"
	"strconv"
)

// Relationship connects records within the containing metadata source.
type Relationship struct {
	Type        string `yaml:"type" json:"type"`
	TargetID    string `yaml:"target_id" json:"target_id"`
	SourceField string `yaml:"source_field,omitempty" json:"source_field,omitempty"`
}

func (r Relationship) CanFoldEdition() bool {
	switch r.Type {
	case "edition_of", "expanded_edition_of", "port_of":
		return true
	}
	return false
}

func RenderRelationships(w io.Writer, indent string, relationships []Relationship) {
	if len(relationships) == 0 {
		fmt.Fprintln(w, indent+"relationships: []")
		return
	}
	fmt.Fprintln(w, indent+"relationships:")
	for _, r := range relationships {
		fmt.Fprintf(w, "%s  - type: %s\n%s    target_id: %s\n", indent, r.Type, indent, strconv.Quote(r.TargetID))
		if r.SourceField != "" {
			fmt.Fprintf(w, "%s    source_field: %s\n", indent, r.SourceField)
		}
	}
}
