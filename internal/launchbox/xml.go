// Package launchbox merges reviewed export packages into a local LaunchBox library.
package launchbox

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Keep raw element spans so unrelated native fields, comments and records survive.
type element struct {
	name       string
	start, end int
	raw        []byte
}
type document struct {
	selfClosing bool
	root        string
	data        []byte
	children    []element
	close       int
}

func parse(data []byte, root string) (*document, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	doc := &document{data: data, root: root}
	depth, roots := 0, 0
	for {
		start := int(d.InputOffset())
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots != 1 || t.Name.Local != root || t.Name.Space != "" {
					return nil, fmt.Errorf("expected one %s root", root)
				}
				depth = 1
			} else {
				if t.Name.Space != "" {
					return nil, fmt.Errorf("namespaced native elements are unsupported")
				}
				if err := d.Skip(); err != nil {
					return nil, err
				}
				end := int(d.InputOffset())
				doc.children = append(doc.children, element{t.Name.Local, start, end, data[start:end]})
			}
		case xml.EndElement:
			doc.close = start
			doc.selfClosing = start == int(d.InputOffset())
			depth = 0
		case xml.Directive:
			return nil, fmt.Errorf("XML directives are unsupported")
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(strings.TrimPrefix(string(t), "\ufeff")) != "" {
				return nil, fmt.Errorf("text outside XML root")
			}
		}
	}
	if roots != 1 || depth != 0 || doc.close == 0 {
		return nil, fmt.Errorf("incomplete %s document", root)
	}
	return doc, nil
}
func (e element) fields() (map[string]string, error) {
	doc, err := parse(e.raw, e.name)
	if err != nil {
		return nil, err
	}
	fields := map[string]string{}
	for _, c := range doc.children {
		if _, ok := fields[c.name]; ok {
			return nil, fmt.Errorf("duplicate %s field in %s", c.name, e.name)
		}
		var value struct {
			Text string `xml:",chardata"`
		}
		if err := xml.Unmarshal(c.raw, &value); err != nil {
			return nil, err
		}
		fields[c.name] = value.Text
	}
	return fields, nil
}
func leaf(name, value string) []byte {
	var b bytes.Buffer
	b.WriteString("<" + name + ">")
	_ = xml.EscapeText(&b, []byte(value))
	b.WriteString("</" + name + ">")
	return b.Bytes()
}
func (doc *document) edit(replacements map[int][]byte, additions [][]byte) []byte {
	var b bytes.Buffer
	if len(additions) > 0 && doc.selfClosing {
		b.Write(doc.data[:doc.close-2])
		b.WriteByte('>')
		for _, v := range additions {
			b.WriteString("\n  ")
			b.Write(v)
		}
		b.WriteString("\n</" + doc.root + ">")
		b.Write(doc.data[doc.close:])
		return b.Bytes()
	}
	offset := 0
	for i, c := range doc.children {
		if v, ok := replacements[i]; ok {
			b.Write(doc.data[offset:c.start])
			b.Write(v)
			offset = c.end
		}
	}
	b.Write(doc.data[offset:doc.close])
	for _, v := range additions {
		b.WriteString("\n  ")
		b.Write(v)
	}
	if len(additions) > 0 {
		b.WriteByte('\n')
	}
	b.Write(doc.data[doc.close:])
	return b.Bytes()
}
func setFields(e element, values map[string]string) ([]byte, error) {
	doc, err := parse(e.raw, e.name)
	if err != nil {
		return nil, err
	}
	replacements := map[int][]byte{}
	for i, c := range doc.children {
		if v, ok := values[c.name]; ok {
			replacements[i] = leaf(c.name, v)
			delete(values, c.name)
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var additions [][]byte
	for _, k := range keys {
		additions = append(additions, leaf(k, values[k]))
	}
	return doc.edit(replacements, additions), nil
}
