package serveui

import (
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
	"rommeta/internal/exportsettings"
)

type exportDestination = exportsettings.Destination
type exportDestinationsDocument = exportsettings.DestinationsDocument
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
	return exportsettings.File(s.realRoot, "export-destinations.yaml")
}
func (s *server) readExportDestinations() (exportDestinationsDocument, exportDestinationsResponse, os.FileMode, error) {
	doc, revision, mode, err := exportsettings.ReadDestinations(s.realRoot)
	host, _ := os.Hostname()
	return doc, exportDestinationsResponse{Revision: revision, Host: host, OS: runtime.GOOS, Destinations: doc.Destinations}, mode, err
}
func validateExportDestination(d *exportDestination) error {
	return exportsettings.ValidateDestination(d)
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
	if !input.Delete && input.Destination.ProfileID != "" {
		if err := exportsettings.ResolveDestinationProfile(s.realRoot, &input.Destination); err != nil {
			writeError(w, 400, err)
			return
		}
		// Store the reusable reference; plans freeze the resolved content separately.
		input.Destination.ProfileYAML = ""
	}
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
