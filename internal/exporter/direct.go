package exporter

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"rommeta/internal/catalogfs"
)

// CheckInputs rechecks only ROM observations; it does not copy or open media.
func CheckInputs(plan *Plan) error {
	for i := range plan.Records {
		if plan.Context != nil {
			if err := plan.Context.Err(); err != nil {
				return err
			}
		}
		if err := checkRecordInput(plan, &plan.Records[i]); err != nil {
			return err
		}
	}
	return nil
}
func checkRecordInput(plan *Plan, record *Record) error {
	if record.Catalog != nil {
		if err := CheckROMSetObservation(plan.ROMRoot, record.ROM, record.Catalog); err != nil {
			return err
		}
	}
	if len(record.HashRequired) > 0 {
		return fmt.Errorf("unresolved ROM hash request: %s", record.ROM)
	}
	if record.Disc != nil && record.SHA1 == "" {
		if err := CheckDiscObservation(plan.ROMRoot, plan.System, record.ROM, record.Disc); err != nil {
			return err
		}
	}
	if record.SHA1 != "" {
		inputName := record.ROM
		if record.LaunchFile != "" {
			inputName = record.LaunchFile
		}
		input := filepath.Join(plan.ROMRoot, filepath.FromSlash(inputName))
		real, err := realPath(input)
		if err != nil {
			return err
		}
		if !within(plan.ROMRoot, real) {
			return fmt.Errorf("ROM path escaped input root")
		}
		reader := plan.System
		if record.Representation == "romset-payload" {
			reader = "romset"
		}
		hashes, member, err := hashROMContext(plan.Context, real, reader)
		if err != nil {
			return err
		}
		if hashes.sha1 != record.SHA1 || hashes.size != record.Size || member != record.Member {
			return fmt.Errorf("ROM changed since planning: %s", record.ROM)
		}
	}
	for _, part := range record.Playlist {
		if part.Disc != nil && part.SHA1 == "" {
			if err := CheckDiscObservation(plan.ROMRoot, plan.System, part.ROM, part.Disc); err != nil {
				return err
			}
			continue
		}
		path, err := realPath(filepath.Join(plan.ROMRoot, filepath.FromSlash(part.ROM)))
		if err != nil {
			return err
		}
		if !within(plan.ROMRoot, path) {
			return fmt.Errorf("playlist input escapes ROM root")
		}
		h, _, err := hashROMContext(plan.Context, path, plan.System)
		if err != nil {
			return err
		}
		if h.size != part.Size || h.sha1 != part.SHA1 {
			return fmt.Errorf("playlist member changed since planning: %s", part.ROM)
		}
	}
	return nil
}

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
