package catalogyaml

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Document is a parsed catalog YAML syntax tree. Keeping the yaml.Node tree
// preserves unknown fields and gives all catalog consumers one structural view.
type Document struct {
	root yaml.Node
}

// Parse validates a catalog YAML document and requires a mapping root.
func Parse(contents []byte) (*Document, error) {
	contents = sanitizeInput(contents)
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(false)
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("catalog YAML root must be a mapping")
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("invalid trailing YAML document: %w", err)
		}
		return nil, fmt.Errorf("catalog YAML must contain exactly one document")
	}
	return &Document{root: root}, nil
}

func sanitizeInput(contents []byte) []byte {
	return []byte(strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, string(contents)))
}

// Root returns the document mapping node.
func (document *Document) Root() *yaml.Node {
	if document == nil || len(document.root.Content) == 0 {
		return nil
	}
	return document.root.Content[0]
}

// Marshal renders the document tree after structural updates.
func (document *Document) Marshal() ([]byte, error) {
	if document == nil {
		return nil, fmt.Errorf("catalog YAML document is nil")
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document.root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// Value returns a mapping value by key.
func Value(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

// Set replaces or appends a mapping value while retaining all other nodes.
func Set(mapping *yaml.Node, key string, value *yaml.Node) error {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping")
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content[index+1] = value
			return nil
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key}, value,
	)
	return nil
}

// EnsureMapping returns an existing child mapping or appends an empty one.
func EnsureMapping(mapping *yaml.Node, key string) (*yaml.Node, error) {
	if existing := Value(mapping, key); existing != nil {
		if existing.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s must be a mapping", key)
		}
		return existing, nil
	}
	created := &yaml.Node{Kind: yaml.MappingNode}
	if err := Set(mapping, key, created); err != nil {
		return nil, err
	}
	return created, nil
}

// StringSequence creates a block sequence of scalar strings.
func StringSequence(values []string) *yaml.Node {
	sequence := &yaml.Node{Kind: yaml.SequenceNode, Style: 0}
	for _, value := range values {
		sequence.Content = append(sequence.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: value, Style: yaml.DoubleQuotedStyle})
	}
	return sequence
}

// Mapping visits mapping entries in source order.
func Mapping(node *yaml.Node, visit func(key string, value *yaml.Node) error) error {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected mapping", node.Line)
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if err := visit(node.Content[index].Value, node.Content[index+1]); err != nil {
			return err
		}
	}
	return nil
}

// Sequence visits sequence items in source order.
func Sequence(node *yaml.Node, visit func(item *yaml.Node) error) error {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: expected sequence", node.Line)
	}
	for _, item := range node.Content {
		if err := visit(item); err != nil {
			return err
		}
	}
	return nil
}

// Scalar returns a scalar value or an empty string for another node kind.
func Scalar(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

// Strings accepts either one scalar or a YAML sequence of scalars.
func Strings(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		if value := strings.TrimSpace(node.Value); value != "" {
			return []string{value}
		}
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil
	}
	var result []string
	for _, item := range node.Content {
		if value := strings.TrimSpace(Scalar(item)); value != "" {
			result = append(result, value)
		}
	}
	return result
}

// Int64 decodes a positive integer scalar.
func Int64(node *yaml.Node) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(Scalar(node)), 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}
