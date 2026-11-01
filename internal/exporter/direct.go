package exporter

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfs"
)

// CheckInputs checks saved filesystem stamps without reopening or hashing ROMs.
func CheckInputs(plan *Plan) error {
	for i := range plan.Records {
		if err := checkRecordInput(plan, &plan.Records[i]); err != nil {
			return err
		}
	}
	return nil
}
func checkRecordInput(plan *Plan, record *Record) error { return record.InputGuard.Check(plan.Context) }

type mediaReader struct {
	input  *catalogfs.File
	before os.FileInfo
	ctx    context.Context
}

func (r *mediaReader) Read(p []byte) (int, error) {
	return (contextReader{ctx: r.ctx, input: r.input}).Read(p)
}
func (r *mediaReader) Close() error {
	after, err := r.input.Stat()
	closeErr := r.input.Close()
	if err != nil {
		return err
	}
	if after.Size() != r.before.Size() || !after.ModTime().Equal(r.before.ModTime()) {
		return fmt.Errorf("media changed while writing")
	}
	return closeErr
}
func openMedia(plan *Plan, media Media) (io.ReadCloser, error) {
	root := plan.DataRoot
	if plan.MediaRoot != "" {
		root = plan.MediaRoot
	}
	root, err := realPath(root)
	if err != nil {
		return nil, err
	}
	source := media.SourcePath
	if cached, ok := plan.MediaPaths[source]; ok {
		source = cached
	}
	name, err := realPath(filepath.Join(root, filepath.FromSlash(source)))
	if err != nil {
		return nil, err
	}
	if !within(root, name) {
		return nil, fmt.Errorf("media escaped data root")
	}
	input, err := catalogfs.Open(name)
	if err != nil {
		return nil, err
	}
	before, err := input.Stat()
	if err != nil {
		input.Close()
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() != media.Size {
		input.Close()
		return nil, fmt.Errorf("media changed since planning: %s", media.SourcePath)
	}
	ctx := plan.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return &mediaReader{input: input, before: before, ctx: ctx}, nil
}
