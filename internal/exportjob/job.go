package exportjob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"rommeta/internal/exportclient"
	"rommeta/internal/exporter"
	"rommeta/internal/frontend"
	"strings"
)

type Job struct {
	rebuild func([]*exporter.Plan) (*frontend.Plan, error)
	Native  *frontend.Plan
	Plans   []*exporter.Plan
	Skipped []exporter.SkippedSystem
}

func transientOutput(options exporter.Options, destination, api string) (string, error) {
	entropy := make([]byte, 12)
	if _, err := rand.Read(entropy); err != nil {
		return "", err
	}
	for _, parent := range []string{filepath.Dir(destination), os.TempDir()} {
		out, err := filepath.Abs(filepath.Join(parent, "rommeta-direct-"+hex.EncodeToString(entropy)))
		if err != nil {
			return "", err
		}
		roots := []string{options.ROMRoot, destination}
		if api == "" {
			roots = append(roots, options.DataRoot)
		}
		overlap := false
		for _, root := range roots {
			if root == "" {
				continue
			}
			abs, err := filepath.Abs(root)
			if err != nil {
				return "", err
			}
			rel, err := filepath.Rel(abs, out)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				overlap = true
			}
		}
		if !overlap {
			return out, nil
		}
	}
	return "", fmt.Errorf("cannot select temporary preparation outside the input and destination trees")
}

// Build retains the reviewed native plan and input guards without destination writes.
func Build(options exporter.Options, api string, systems []string, all bool, destination, mediaRoot, token string) (*Job, error) {
	if mediaRoot != "" && options.Target != "es-de" {
		return nil, fmt.Errorf("media root is only supported for ES-DE")
	}
	stage, err := transientOutput(options, destination, api)
	if err != nil {
		return nil, err
	}
	options.Out = stage
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
		options.Context = ctx
	}
	var plans []*exporter.Plan
	var skipped []exporter.SkippedSystem
	var checkInputs func() error
	var remoteAsset func(string) (io.ReadCloser, error)
	if api != "" {
		job, err := exportclient.Build(ctx, exportclient.Options{BatchOptions: exporter.BatchOptions{Options: options, All: all, Systems: systems}, API: api, Token: token})
		if err != nil {
			return nil, err
		}
		plans, skipped, stage = job.Batch.Plans, job.Batch.Skipped, job.Batch.Out
		checkInputs = func() error { return job.CheckInputs(ctx) }
		remoteAsset = func(source string) (io.ReadCloser, error) { return job.OpenAsset(ctx, source) }
	} else if options.Platform != "" {
		plan, err := exporter.Build(options)
		if err != nil {
			return nil, err
		}
		plans = []*exporter.Plan{plan}
		stage = plan.Out
		checkInputs = func() error { return exporter.CheckInputs(plan) }
	} else {
		batch, err := exporter.BuildBatch(exporter.BatchOptions{Options: options, All: all, Systems: systems})
		if err != nil {
			return nil, err
		}
		plans, skipped, stage = batch.Plans, batch.Skipped, batch.Out
		checkInputs = func() error {
			for _, plan := range batch.Plans {
				if err := exporter.CheckInputs(plan); err != nil {
					return err
				}
			}
			return nil
		}
	}
	if options.Progress != nil {
		options.Progress("native fields", options.Target, 0, 0)
	}
	rebuild := func(selected []*exporter.Plan) (*frontend.Plan, error) {
		library, files, err := exporter.DestinationData(selected)
		if err != nil {
			return nil, err
		}
		source := &frontend.Source{Library: library, Check: checkInputs, Files: map[string]frontend.SourceFile{}}
		for name, file := range files {
			open := file.Open
			if remoteAsset != nil && file.SourcePath != "" {
				path := file.SourcePath
				open = func() (io.ReadCloser, error) { return remoteAsset(path) }
			}
			source.Files[name] = frontend.SourceFile{Data: file.Data, SHA256: file.SHA256, Size: file.Size, Open: open}
		}
		nativeOptions := frontend.Options{Progress: options.Progress, Target: options.Target, PackageRoot: stage, Destination: destination, MediaRoot: mediaRoot, Source: source}
		plan, err := frontend.Build(nativeOptions)
		if err != nil {
			return nil, err
		}

		return plan, nil
	}
	plan, err := rebuild(plans)
	if err != nil {
		return nil, err
	}
	return &Job{Native: plan, Plans: plans, Skipped: skipped, rebuild: rebuild}, nil
}

type ManualBinding struct {
	ID string `json:"id"`
	exporter.ManualMatch
}

// Review rebuilds native output from the retained scan without reading ROM payloads.
func (j *Job) WithExcluded(excluded map[string]bool) (*Job, error) {
	return j.WithReview(excluded, nil)
}
func (j *Job) WithReview(excluded map[string]bool, bindings []ManualBinding) (*Job, error) {
	choices := map[string]exporter.ManualMatch{}
	for _, b := range bindings {
		if _, exists := choices[b.ID]; exists || excluded[b.ID] {
			return nil, fmt.Errorf("duplicate or excluded manual selection")
		}
		choices[b.ID] = b.ManualMatch
	}
	used := 0
	if j.rebuild == nil {
		return nil, fmt.Errorf("this plan cannot be revised; prepare it again")
	}
	plans := make([]*exporter.Plan, 0, len(j.Plans))
	for _, p := range j.Plans {
		copy := *p
		copy.Records = nil
		copy.LaunchBox = nil
		for _, r := range p.Records {
			if !excluded[InputKey(p.Platform, r.ROM)] {
				if choice, ok := choices[InputKey(p.Platform, r.ROM)]; ok {
					var err error
					r, err = exporter.BindManual(p, r, choice)
					if err != nil {
						return nil, err
					}
					used++
				}
				r.Fields = maps.Clone(r.Fields)
				copy.Records = append(copy.Records, r)
			}
		}
		if err := exporter.FinalizeExport(&copy); err != nil {
			return nil, err
		}
		plans = append(plans, &copy)
	}
	if used != len(choices) {
		return nil, fmt.Errorf("manual selection refers to an unknown input")
	}
	native, err := j.rebuild(plans)
	if err != nil {
		return nil, err
	}
	return &Job{Native: native, Plans: plans, Skipped: j.Skipped, rebuild: j.rebuild}, nil
}
func InputKey(platform, rom string) string { return platform + "\n" + rom }
