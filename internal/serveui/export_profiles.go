package serveui

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"net/http"
	"rommeta/internal/atomicfile"
	"rommeta/internal/catalogedit"
	"rommeta/internal/exportsettings"
	"strings"
)

type savedProfilesResponse struct {
	Revision string                        `json:"revision"`
	Profiles []exportsettings.SavedProfile `json:"profiles"`
}
type savedProfileRequest struct {
	Revision string                      `json:"revision"`
	Profile  exportsettings.SavedProfile `json:"profile"`
	Delete   bool                        `json:"delete,omitempty"`
}

func (s *server) exportProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.allowEdit {
			writeError(w, 403, fmt.Errorf("saved profiles require serve --edit"))
			return
		}
		doc, revision, _, err := exportsettings.ReadProfiles(s.realRoot)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, savedProfilesResponse{revision, doc.Profiles})
		return
	}
	var input savedProfileRequest
	if !s.editRequest(w, r, &input) {
		return
	}
	input.Profile.Name = strings.TrimSpace(input.Profile.Name)
	if !input.Delete && (input.Profile.Name == "" || len(input.Profile.Name) > 120) {
		writeError(w, 400, fmt.Errorf("give this profile a name (up to 120 characters)"))
		return
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
	doc, revision, mode, err := exportsettings.ReadProfiles(s.realRoot)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	if input.Revision != revision {
		writeError(w, 409, fmt.Errorf("saved profiles changed; reload the list before saving"))
		return
	}
	index := -1
	for i, p := range doc.Profiles {
		if p.ID == input.Profile.ID {
			index = i
		}
	}
	if input.Profile.ID != "" && index < 0 {
		writeError(w, 409, fmt.Errorf("profile no longer exists; reload the list"))
		return
	}
	destinations, _, _, err := exportsettings.ReadDestinations(s.realRoot)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	if input.Delete {
		if index < 0 {
			writeError(w, 400, fmt.Errorf("choose a saved profile"))
			return
		}
		for _, d := range destinations.Destinations {
			if d.ProfileID == input.Profile.ID {
				writeError(w, 409, fmt.Errorf("profile is used by destination %q; choose another profile there before removing it", d.Name))
				return
			}
		}
		doc.Profiles = append(doc.Profiles[:index], doc.Profiles[index+1:]...)
	} else {
		_, content, target, err := s.resolveExportProfile(input.Profile.ProfileYAML, input.Profile.Target)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		input.Profile.ProfileYAML = content
		input.Profile.Target = target
		for _, d := range destinations.Destinations {
			if d.ProfileID != "" && d.ProfileID == input.Profile.ID && d.Target != target {
				writeError(w, 400, fmt.Errorf("profile is used by %s destination %q; save a new profile for %s", d.Target, d.Name, target))
				return
			}
		}
		for i, p := range doc.Profiles {
			if i != index && strings.EqualFold(p.Name, input.Profile.Name) {
				writeError(w, 400, fmt.Errorf("a profile with this name already exists"))
				return
			}
		}
		if index < 0 {
			if len(doc.Profiles) >= 100 {
				writeError(w, 400, fmt.Errorf("at most 100 saved profiles are supported"))
				return
			}
			input.Profile.ID = operationID()
			doc.Profiles = append(doc.Profiles, input.Profile)
		} else {
			input.Profile.Extra = doc.Profiles[index].Extra
			doc.Profiles[index] = input.Profile
		}
	}
	data, err := yaml.Marshal(doc)
	if err == nil && len(data) > 2<<20 {
		err = fmt.Errorf("saved profiles exceed 2 MiB; use smaller profiles")
	}
	if err != nil {
		writeError(w, 400, err)
		return
	}
	path, err := exportsettings.File(s.realRoot, "export-profiles.yaml")
	if err == nil {
		err = atomicfile.WriteFile(path, data, mode)
	}
	if err != nil {
		writeError(w, 500, err)
		return
	}
	doc, revision, _, err = exportsettings.ReadProfiles(s.realRoot)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, savedProfilesResponse{revision, doc.Profiles})
}
