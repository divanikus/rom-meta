package serveui

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogfs"
	"rommeta/internal/enrichment"
	"rommeta/internal/exporter"
	"rommeta/internal/fetchstate"
	"rommeta/internal/platformnames"
	"rommeta/internal/screenscraper"
	"rommeta/internal/store"
	"rommeta/internal/taskprogress"
)

type enrichmentChoice struct {
	Workers   int    `json:"workers"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	Available bool   `json:"available"`
	Default   bool   `json:"default"`
	Reason    string `json:"reason,omitempty"`
}
type enrichmentOptions struct {
	Platform string             `json:"platform"`
	Revision string             `json:"revision"`
	Choices  []enrichmentChoice `json:"choices"`
}
type enrichmentSelection struct {
	Source string `json:"source"`
	Type   string `json:"type"`
}
type enrichmentRequest struct {
	Platforms   []string              `json:"platforms,omitempty"`
	Concurrency int                   `json:"concurrency,omitempty"`
	Platform    string                `json:"platform"`
	Revision    string                `json:"revision"`
	Jobs        []enrichmentSelection `json:"jobs"`
}
type enrichmentTask struct {
	RetryOperation string            `json:"retry_operation,omitempty"`
	Platform       string            `json:"platform"`
	Workers        int               `json:"workers"`
	Source         string            `json:"source"`
	Type           string            `json:"type"`
	State          string            `json:"state"`
	Counts         enrichment.Counts `json:"counts"`
	Error          string            `json:"error,omitempty"`
}
type enrichmentState struct {
	Platforms   []string                      `json:"platforms"`
	Concurrency int                           `json:"concurrency"`
	Workers     map[int]taskprogress.Snapshot `json:"workers,omitempty"`
	Jobs        []enrichmentTask              `json:"jobs"`
	Current     int                           `json:"current"`
	Progress    taskprogress.Snapshot         `json:"progress"`
}

func cloneOperation(op *libraryOperation) libraryOperation {
	result := *op
	if op.Export != nil {
		state := *op.Export
		result.Export = &state
	}
	if op.Enrichment != nil {
		state := *op.Enrichment
		state.Jobs = append([]enrichmentTask(nil), state.Jobs...)
		state.Platforms = append([]string(nil), state.Platforms...)
		state.Workers = map[int]taskprogress.Snapshot{}
		for id, p := range op.Enrichment.Workers {
			state.Workers[id] = p
		}
		result.Enrichment = &state
	}
	return result
}

// Provider configuration is server-side. Browser input can select only known
// source/type pairs for one exact catalog platform, never credentials or URLs.
func (s *server) enrichmentChoices(platform string) (enrichmentOptions, store.SourcesConfig, *platformnames.Registry, error) {
	result := enrichmentOptions{Platform: platform, Choices: []enrichmentChoice{}}
	if !catalogedit.Component(platform) {
		return result, store.SourcesConfig{}, nil, fmt.Errorf("choose a catalog platform")
	}
	path, err := catalogfs.EvalSymlinks(filepath.Join(s.root, "platforms", platform))
	if err != nil {
		return result, store.SourcesConfig{}, nil, fmt.Errorf("platform is unavailable")
	}
	relative, err := filepath.Rel(s.realRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return result, store.SourcesConfig{}, nil, fmt.Errorf("platform must stay inside this library")
	}
	info, err := catalogfs.Stat(path)
	if err != nil || !info.IsDir() {
		return result, store.SourcesConfig{}, nil, fmt.Errorf("platform is unavailable")
	}
	config, err := store.LoadSources(s.root)
	if err != nil {
		return result, config, nil, fmt.Errorf("cannot read source settings; check config/sources.yaml")
	}
	registry, _, err := platformnames.LoadRegistry(s.root)
	if err != nil {
		return result, config, nil, fmt.Errorf("cannot read platform mappings; check config/platforms.yaml")
	}
	registry.MergeAliases(config.PlatformAliases)
	hash := sha256.New()
	for _, name := range []string{"config/sources.yaml", "config/platforms.yaml", "config/secrets.yaml", "config/catalog.yaml"} {
		data, e := os.ReadFile(filepath.Join(s.root, name))
		if e != nil && !os.IsNotExist(e) {
			return result, config, nil, fmt.Errorf("cannot read library configuration")
		}
		hash.Write([]byte(name))
		hash.Write(data)
	}
	for _, typ := range []string{"metadata", "artwork", "video"} {
		order := append([]string(nil), config.Fetch[typ]...)
		for _, name := range []string{"launchbox", "openvgdb", "igdb", "screenscraper", "hasheous", "emumovies"} {
			if !slices.Contains(order, name) {
				order = append(order, name)
			}
		}
		seen := map[string]bool{}
		for _, name := range order {
			if seen[name] || !enrichment.Supported(enrichment.Job{Source: name, Type: typ}) {
				continue
			}
			seen[name] = true
			reason := enrichment.MappingReason(enrichment.Job{Source: name, Type: typ}, platform, registry, config)
			for _, source := range config.Sources {
				if source.Name != name {
					continue
				}
				for _, key := range store.SourceRequiredCredentials(name) {
					value, ok := store.CredentialValue(config, source, key)
					if !ok && name == "screenscraper" && (typ == "artwork" || key == "dev_id" || key == "dev_password") && config.Secrets.Err == nil {
						value = config.Secrets.Values[name][key]
						ok = value != ""
					}
					hash.Write([]byte(name + "\x00" + key + "\x00" + value))
					if (!ok || strings.TrimSpace(value) == "") && reason == "" && (typ == "metadata" || name == "screenscraper" || name == "emumovies") {
						reason = "Credentials are missing. Configure this source on the server."
					}
				}
			}
			result.Choices = append(result.Choices, enrichmentChoice{Workers: enrichment.Concurrency(enrichment.Options{}, config, enrichment.Job{Source: name, Type: typ}), Source: name, Type: typ, Available: reason == "", Default: reason == "" && slices.Contains(config.Fetch[typ], name), Reason: reason})
		}
	}
	result.Revision = fmt.Sprintf("%x", hash.Sum(nil))
	return result, config, &registry, nil
}
func (s *server) enrichmentOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.allowEdit {
		writeError(w, 403, fmt.Errorf("enrichment requires serve --edit"))
		return
	}
	options, _, _, err := s.enrichmentChoices(r.URL.Query().Get("platform"))
	if err != nil {
		writeError(w, 400, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, options)
}
func (s *server) enrichmentPlan(w http.ResponseWriter, r *http.Request) {
	var input enrichmentRequest
	if !s.editRequest(w, r, &input) {
		return
	}
	platforms := input.Platforms
	if len(platforms) == 0 && input.Platform != "" {
		platforms = []string{input.Platform}
	}
	if len(platforms) == 0 || len(platforms) > 256 {
		writeError(w, 400, fmt.Errorf("select between 1 and 256 platforms"))
		return
	}
	if input.Concurrency < 0 || input.Concurrency > 32 {
		writeError(w, 400, fmt.Errorf("parallel workers must be between 1 and 32, or 0 for source defaults"))
		return
	}
	if len(input.Jobs) == 0 || len(input.Jobs) > 12 {
		writeError(w, 400, fmt.Errorf("select at least one supported source and data type"))
		return
	}
	selected := map[enrichmentSelection]bool{}
	for _, job := range input.Jobs {
		if selected[job] || !enrichment.Supported(enrichment.Job{Source: job.Source, Type: job.Type}) {
			writeError(w, 400, fmt.Errorf("duplicate or unsupported enrichment task"))
			return
		}
		selected[job] = true
	}
	state := &enrichmentState{Jobs: []enrichmentTask{}, Platforms: append([]string(nil), platforms...), Concurrency: input.Concurrency}
	seen := map[string]bool{}
	available := 0
	for _, platform := range platforms {
		if seen[platform] {
			writeError(w, 400, fmt.Errorf("duplicate platform"))
			return
		}
		seen[platform] = true
		options, config, _, err := s.enrichmentChoices(platform)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		if options.Revision != input.Revision {
			writeError(w, 409, fmt.Errorf("source settings changed; reload choices and review the selection"))
			return
		}
		for _, choice := range options.Choices {
			if !selected[enrichmentSelection{choice.Source, choice.Type}] {
				continue
			}
			task := enrichmentTask{Platform: platform, Source: choice.Source, Type: choice.Type, State: "pending"}
			task.Workers = enrichment.Concurrency(enrichment.Options{Concurrency: input.Concurrency, ConcurrencySet: input.Concurrency > 0}, config, enrichment.Job{Source: choice.Source, Type: choice.Type})
			if !choice.Available {
				if len(platforms) == 1 {
					writeError(w, 400, fmt.Errorf("%s: %s", choice.Source, choice.Reason))
					return
				}
				task.State, task.Error = "skipped", choice.Reason
			} else {
				available++
			}
			state.Jobs = append(state.Jobs, task)
		}
	}
	if available == 0 {
		writeError(w, 400, fmt.Errorf("no selected source is available for these platforms"))
		return
	}
	s.operationsMu.Lock()
	s.expireOperationsLocked()
	if len(s.operations)+s.uploads >= maxLibraryOperations {
		s.operationsMu.Unlock()
		writeError(w, 409, fmt.Errorf("dismiss an old operation before preparing another"))
		return
	}
	op := &libraryOperation{ID: operationID(), Kind: "enrichment", Platform: platforms[0], State: "ready", Created: time.Now().UTC(), Updated: time.Now().UTC(), Revision: 1, Enrichment: state, stamp: input.Revision}
	if s.operations == nil {
		s.operations = map[string]*libraryOperation{}
	}
	s.operations[op.ID] = op
	snapshot := cloneOperation(op)
	s.operationsMu.Unlock()
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, snapshot)
}
func (s *server) enrichmentStart(w http.ResponseWriter, r *http.Request) {
	var input importAction
	if !s.editRequest(w, r, &input) {
		return
	}
	if err := s.beginEnrichment(input.ID, input.Revision); err != nil {
		message := strings.ReplaceAll(err.Error(), s.root, "library")
		s.changeOperation(input.ID, func(op *libraryOperation) {
			if op.Kind == "enrichment" {
				op.Error = message
			}
		})
		writeError(w, 409, fmt.Errorf("%s", message))
		return
	}
	snapshot, _ := s.operationSnapshot(input.ID)
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, snapshot)
}

var enrichmentURLs = regexp.MustCompile(`(?i)(?:https?|ftp)://[^\s<>"']+`)

func enrichmentText(config store.SourcesConfig, text string) string {
	for _, values := range config.Secrets.Values {
		for _, value := range values {
			if value != "" {
				text = strings.ReplaceAll(text, value, "[redacted]")
				text = strings.ReplaceAll(text, url.QueryEscape(value), "[redacted]")
			}
		}
	}
	for _, source := range config.Sources {
		for _, key := range source.Credentials {
			if value, ok := store.CredentialValue(config, source, key.Name); ok && value != "" {
				text = strings.ReplaceAll(text, value, "[redacted]")
				text = strings.ReplaceAll(text, url.QueryEscape(value), "[redacted]")
			}
		}
	}
	text = enrichmentURLs.ReplaceAllStringFunc(text, func(raw string) string {
		u, e := url.Parse(raw)
		if e != nil {
			return "[provider URL]"
		}
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	})
	if config.Root != "" {
		text = strings.ReplaceAll(text, config.Root, "library")
	}
	if len(text) > 800 {
		text = text[:800] + "…"
	}
	return text
}
func (s *server) runEnrichment(op libraryOperation, config store.SourcesConfig, registry *platformnames.Registry) {
	run := s.enrichRun
	if run == nil {
		run = enrichment.Run
	}
	batch := catalogchanges.Begin(exporter.RefreshPlatformIndex)
	failed, issues := 0, 0
	quota := map[string]bool{}
	for index, task := range op.Enrichment.Jobs {
		if task.State == "skipped" {
			issues++
			continue
		}
		if quota[task.Source] {
			issues++
			s.changeOperation(op.ID, func(current *libraryOperation) {
				current.Enrichment.Jobs[index].State = "skipped"
				current.Enrichment.Jobs[index].Error = "Daily quota reached earlier in this operation."
			})
			continue
		}
		s.changeOperation(op.ID, func(current *libraryOperation) {
			current.Enrichment.Current = index
			current.Enrichment.Progress = taskprogress.Snapshot{Stage: "starting"}
			current.Enrichment.Workers = map[int]taskprogress.Snapshot{}
			current.Enrichment.Jobs[index].State = "running"
		})
		progress := func(p taskprogress.Snapshot) {
			p.Item = enrichmentText(config, p.Item)
			if p.Transfers != nil {
				copy := *p.Transfers
				p.Transfers = &copy
			}
			s.changeOperation(op.ID, func(current *libraryOperation) {
				previous := current.Enrichment.Progress
				if p.Worker > 0 {
					if current.Enrichment.Workers == nil {
						current.Enrichment.Workers = map[int]taskprogress.Snapshot{}
					}
					if p.BytesCurrent < 0 {
						delete(current.Enrichment.Workers, p.Worker)
					} else {
						current.Enrichment.Workers[p.Worker] = p
					}
					previous.Item = p.Item
					previous.BytesCurrent = 0
					previous.BytesTotal = 0
					if p.Transfers != nil {
						previous.Transfers = p.Transfers
					}
					current.Enrichment.Progress = previous
					return
				}
				current.Enrichment.Progress = p
			})
		}
		result, err := run(enrichment.Job{Source: task.Source, Type: task.Type, Platform: task.Platform, Automatic: true}, enrichment.Options{DataRoot: s.root, Platform: task.Platform, PlatformRegistry: registry, Apply: true, Concurrency: op.Enrichment.Concurrency, ConcurrencySet: op.Enrichment.Concurrency > 0}, config, progress)
		counts := enrichment.Summarize(result)
		status, message := "succeeded", ""
		if err != nil {
			issues++
			message = enrichmentText(config, err.Error())
			var partial *fetchstate.PartialError
			if errors.As(err, &partial) || errors.Is(err, screenscraper.ErrDailyQuota) {
				status = "incomplete"
			} else {
				status = "failed"
				failed++
			}
			if errors.Is(err, screenscraper.ErrDailyQuota) {
				quota[task.Source] = true
			}
		}
		s.changeOperation(op.ID, func(current *libraryOperation) {
			j := &current.Enrichment.Jobs[index]
			j.State = status
			j.Counts = counts
			j.Error = message
		})
	}
	s.changeOperation(op.ID, func(current *libraryOperation) {
		current.Enrichment.Progress = taskprogress.Snapshot{Stage: "indexing"}
	})
	warning := ""
	if err := batch.Finish(); err != nil {
		warning = "Saved data is retained; index refresh needs attention: " + enrichmentText(config, err.Error())
	}
	s.summaryMu.Lock()
	s.summaries = nil
	s.summaryMu.Unlock()
	s.changeOperation(op.ID, func(current *libraryOperation) {
		current.State = "succeeded"
		if issues > 0 {
			current.State = "incomplete"
		}
		if failed == len(op.Enrichment.Jobs) {
			current.State = "failed"
		}
		current.Warning = warning
		current.Enrichment.Progress = taskprogress.Snapshot{Stage: "complete"}
	})
}
