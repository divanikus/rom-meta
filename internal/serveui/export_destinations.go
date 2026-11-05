package serveui

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/exporter"
)

type exportDestination struct {
	ID          string               `json:"id" yaml:"id"`
	Name        string               `json:"name" yaml:"name"`
	Target      string               `json:"target" yaml:"target"`
	Path        string               `json:"path" yaml:"path"`
	MediaRoot   string               `json:"media_root,omitempty" yaml:"media_root,omitempty"`
	ROMRoot     string               `json:"rom_root,omitempty" yaml:"rom_root,omitempty"`
	Platform    string               `json:"platform,omitempty" yaml:"platform,omitempty"`
	All         bool                 `json:"all,omitempty" yaml:"all,omitempty"`
	ProfileYAML string               `json:"profile_yaml,omitempty" yaml:"profile_yaml,omitempty"`
	ProfileName string               `json:"profile_name,omitempty" yaml:"profile_name,omitempty"`
	Extra       map[string]yaml.Node `json:"-" yaml:",inline"`
}
type exportDestinationsDocument struct {
	SchemaVersion int                  `yaml:"schema_version"`
	Destinations  []exportDestination  `yaml:"destinations"`
	Extra         map[string]yaml.Node `yaml:",inline"`
}
type exportDestinationsResponse struct {
	Revision     string              `json:"revision"`
	Host         string              `json:"host"`
	OS           string              `json:"os"`
	Destinations []exportDestination `json:"destinations"`
}
type exportDestinationRequest struct {
	Revision    string            `json:"revision"`
	Destination exportDestination `json:"destination"`
	Delete      bool              `json:"delete,omitempty"`
}

func (s *server) exportDestinationsFile() (string, error) {
	path := filepath.Join(s.realRoot, "config", "export-destinations.yaml")
	for p := path; p != s.realRoot; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("export destination configuration cannot use symlinks")
		}
	}
	return path, nil
}
func (s *server) readExportDestinations() (exportDestinationsDocument, exportDestinationsResponse, os.FileMode, error) {
	doc := exportDestinationsDocument{SchemaVersion: 1, Destinations: []exportDestination{}}
	response := exportDestinationsResponse{OS: runtime.GOOS}
	response.Host, _ = os.Hostname()
	path, err := s.exportDestinationsFile()
	if err != nil {
		return doc, response, 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return doc, response, 0, err
	}
	mode := os.FileMode(0600)
	if len(data) > 0 {
		if len(data) > 2<<20 {
			return doc, response, 0, fmt.Errorf("destination configuration is too large")
		}
		if _, err = catalogyaml.Parse(data); err != nil {
			return doc, response, 0, err
		}
		if err = yaml.Unmarshal(data, &doc); err != nil {
			return doc, response, 0, err
		}
		if doc.SchemaVersion != 1 {
			return doc, response, 0, fmt.Errorf("unsupported export destinations schema")
		}
		if info, e := os.Stat(path); e == nil {
			mode = info.Mode().Perm()
		}
	}
	if doc.Destinations == nil {
		doc.Destinations = []exportDestination{}
	}
	response.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
	response.Destinations = doc.Destinations
	return doc, response, mode, nil
}
func validateExportDestination(d *exportDestination) error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len(d.Name) > 120 {
		return fmt.Errorf("give this destination a name (up to 120 characters)")
	}
	if d.Target != "es-de" && d.Target != "launchbox" {
		return fmt.Errorf("choose ES-DE or LaunchBox")
	}
	d.Path = strings.TrimSpace(d.Path)
	d.ROMRoot = strings.TrimSpace(d.ROMRoot)
	d.MediaRoot = strings.TrimSpace(d.MediaRoot)
	for _, path := range []string{d.Path, d.ROMRoot, d.MediaRoot} {
		if path != "" && !filepath.IsAbs(path) {
			return fmt.Errorf("use absolute paths on the machine running serve")
		}
		if strings.ContainsRune(path, 0) {
			return fmt.Errorf("invalid path")
		}
	}
	if d.Path == "" {
		return fmt.Errorf("choose a destination folder")
	}
	if d.MediaRoot != "" && d.Target != "es-de" {
		return fmt.Errorf("a separate media folder is only supported for ES-DE")
	}
	if len(d.ProfileYAML) > 1<<20 {
		return fmt.Errorf("profile must be at most 1 MiB")
	}
	if d.ProfileYAML != "" {
		if err := exporter.ValidateExportProfile(d.ProfileYAML); err != nil {
			return fmt.Errorf("profile: %w", err)
		}
	}
	if len(d.ProfileName) > 120 {
		return fmt.Errorf("profile name is too long")
	}
	return nil
}
func (s *server) exportDestinations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.allowEdit {
			writeError(w, 403, fmt.Errorf("export destinations require serve --edit"))
			return
		}
		_, response, _, err := s.readExportDestinations()
		if err != nil {
			writeError(w, 400, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, response)
		return
	}
	var input exportDestinationRequest
	if !s.editRequest(w, r, &input) {
		return
	}
	if !input.Delete {
		if err := validateExportDestination(&input.Destination); err != nil {
			writeError(w, 400, err)
			return
		}
	}
	if !s.editMu.TryLock() {
		writeError(w, 409, fmt.Errorf("another library operation is running; save when it finishes"))
		return
	}
	defer s.editMu.Unlock()
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		writeError(w, 409, err)
		return
	}
	defer unlock()
	doc, response, mode, err := s.readExportDestinations()
	if err != nil {
		writeError(w, 400, err)
		return
	}
	if input.Revision != response.Revision {
		writeError(w, 409, fmt.Errorf("saved destinations changed; reload them before saving"))
		return
	}
	index := -1
	for i, d := range doc.Destinations {
		if d.ID == input.Destination.ID {
			index = i
		}
	}
	if input.Destination.ID != "" && index < 0 {
		writeError(w, 409, fmt.Errorf("destination no longer exists; reload the list"))
		return
	}
	if input.Delete {
		if index < 0 {
			writeError(w, 400, fmt.Errorf("choose a saved destination"))
			return
		}
		doc.Destinations = append(doc.Destinations[:index], doc.Destinations[index+1:]...)
	} else {
		for i, d := range doc.Destinations {
			if i != index && strings.EqualFold(d.Name, input.Destination.Name) {
				writeError(w, 400, fmt.Errorf("a destination with this name already exists"))
				return
			}
		}
		if index < 0 {
			if len(doc.Destinations) >= 100 {
				writeError(w, 400, fmt.Errorf("at most 100 saved destinations are supported"))
				return
			}
			input.Destination.ID = operationID()
			doc.Destinations = append(doc.Destinations, input.Destination)
		} else {
			input.Destination.Extra = doc.Destinations[index].Extra
			doc.Destinations[index] = input.Destination
		}
	}
	data, err := yaml.Marshal(doc)
	if err == nil && len(data) > 2<<20 {
		writeError(w, 400, fmt.Errorf("saved destination settings exceed 2 MiB; use smaller profiles"))
		return
	}
	if err == nil {
		var path string
		path, err = s.exportDestinationsFile()
		if err == nil {
			err = atomicfile.WriteFile(path, data, mode)
		}
	}
	if err != nil {
		writeError(w, 500, err)
		return
	}
	_, response, _, err = s.readExportDestinations()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, response)
}

type exportFolder struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type exportFoldersResponse struct {
	Path    string         `json:"path"`
	Parent  string         `json:"parent"`
	Folders []exportFolder `json:"folders"`
}

func (s *server) exportFolders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.allowEdit {
		writeError(w, 403, fmt.Errorf("folder browsing requires serve --edit"))
		return
	}
	result := exportFoldersResponse{Folders: []exportFolder{}}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" && runtime.GOOS == "windows" {
		for drive := 'A'; drive <= 'Z'; drive++ {
			p := fmt.Sprintf("%c:/", drive)
			if info, e := os.Stat(p); e == nil && info.IsDir() {
				result.Folders = append(result.Folders, exportFolder{p, p})
			}
		}
		writeJSON(w, result)
		return
	}
	if path == "" {
		path = "/"
	}
	if !filepath.IsAbs(path) {
		writeError(w, 400, fmt.Errorf("choose an absolute server folder"))
		return
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		writeError(w, 400, fmt.Errorf("folder is unavailable on the server: %w", err))
		return
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	result.Path = real
	result.Parent = filepath.Dir(real)
	if result.Parent == real {
		result.Parent = ""
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(real, e.Name())
		info, err := os.Stat(p)
		if err == nil && info.IsDir() {
			result.Folders = append(result.Folders, exportFolder{e.Name(), p})
		}
	}
	sort.Slice(result.Folders, func(i, j int) bool {
		return strings.ToLower(result.Folders[i].Name) < strings.ToLower(result.Folders[j].Name)
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, result)
}
