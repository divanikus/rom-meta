// Package fetchstate stores resumable, credential-free download outcomes.
package fetchstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"rommeta/internal/sourceurl"
)

type PartialError struct{ Failed int }

func (e *PartialError) Error() string {
	return fmt.Sprintf("%d operations failed; successful results were retained", e.Failed)
}
func Incomplete(failed int) error {
	if failed == 0 {
		return nil
	}
	return &PartialError{Failed: failed}
}

type Row map[string]string

type Writer struct {
	Path    string
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
	Count   int
	Err     error
}

func Open(root, source, kind string, apply bool) (*Writer, error) {
	if !apply {
		return nil, nil
	}
	dir := filepath.Join(root, ".rommeta", "runs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, source+"-"+kind+"-*.jsonl")
	if err != nil {
		return nil, err
	}
	return &Writer{Path: f.Name(), file: f, encoder: json.NewEncoder(f)}, nil
}
func (w *Writer) Write(row Row) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	clean := Row{}
	for k, v := range row {
		clean[k] = v
	}
	clean["url"] = sourceurl.StripSecrets(clean["url"])
	if w.Err == nil {
		w.Err = w.encoder.Encode(clean)
		if w.Err == nil {
			w.Count++
		}
	}
	return w.Err
}
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		if w.Err == nil {
			w.Err = err
		}
	}
	return w.Err
}
