package catalogedit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/curation"
)

// DocumentEdit is an optional paired-document edit. YAML holds a previously
// validated draft when the browser switches between structured and raw editing.
type DocumentEdit struct {
	Revision string        `json:"revision"`
	YAML     *string       `json:"yaml,omitempty"`
	Patch    RegisterPatch `json:"patch"`
}
type PreparedDocuments struct {
	Card                                         Card
	Register                                     Register
	game, fingerprints, oldGame, oldFingerprints []byte
	path                                         string
}

func PrepareDocuments(root, platform, folder, revision string, edits []Edit, register DocumentEdit) (PreparedDocuments, error) {
	result := PreparedDocuments{}
	path, err := GamePath(root, platform, folder)
	if err != nil {
		return result, err
	}
	result.path = path
	game, err := catalogfs.ReadFile(filepath.Join(path, "game.yaml"))
	if err != nil {
		return result, err
	}
	if revision == "" || Hash(game) != revision {
		return result, fmt.Errorf("card changed since it was opened; reload before saving")
	}
	fingerprints, err := readFingerprintFile(path)
	if err != nil {
		return result, err
	}
	if register.Revision == "" || Hash(fingerprints) != register.Revision {
		return result, fmt.Errorf("fingerprints changed since they were opened; reload before saving")
	}
	result.oldGame = game
	result.oldFingerprints = fingerprints
	registry, err := curation.Load(root)
	if err != nil {
		return result, err
	}
	for _, edit := range edits {
		if edit.Target.Kind == "manual" && !registry.Contains(edit.Target.Source) {
			return result, fmt.Errorf("create or select a named source first")
		}
	}
	game, err = ApplyEdits(game, edits)
	if err != nil {
		return result, err
	}
	if register.YAML != nil {
		if len(*register.YAML) > 3<<20 {
			return result, fmt.Errorf("fingerprints YAML must be at most 3 MiB")
		}
		fingerprints = []byte(*register.YAML)
		if err = validateFingerprintDraft(result.oldFingerprints, fingerprints); err != nil {
			return result, err
		}
	}
	game, fingerprints, err = ApplyRegister(game, fingerprints, register.Patch)
	if err != nil {
		return result, err
	}
	if err = CheckRawReferences(root, platform, folder, edits, game, fingerprints); err != nil {
		return result, err
	}
	result.Card, err = Inspect(game)
	if err != nil {
		return result, err
	}
	result.Register, err = InspectRegister(game, fingerprints)
	if err != nil {
		return result, err
	}
	result.game = game
	result.fingerprints = fingerprints
	return result, nil
}
func SaveDocuments(root, platform, folder, revision string, edits []Edit, register DocumentEdit) (PreparedDocuments, error) {
	result, err := PrepareDocuments(root, platform, folder, revision, edits, register)
	if err != nil {
		return result, err
	}
	err = writeDocuments(result, os.Rename)
	return result, err
}

// Stage every changed document before touching originals. Packed documents share
// one atomic metadata.zip replacement. Loose files roll back ordinary rename
// failures; they are not a filesystem transaction across a process/power failure.
func writeDocuments(p PreparedDocuments, rename func(string, string) error) error {
	type staged struct {
		name, temp, rollback string
		existed              bool
	}
	changes := []staged{}
	defer func() {
		for _, s := range changes {
			os.Remove(s.temp)
			if s.rollback != "" {
				os.Remove(s.rollback)
			}
		}
	}()
	packed, err := catalogfs.Packed(filepath.Join(p.path, "game.yaml"))
	if err != nil {
		return err
	}
	for _, pair := range []struct {
		name      string
		old, next []byte
	}{{"game.yaml", p.oldGame, p.game}, {"fingerprints.yaml", p.oldFingerprints, p.fingerprints}} {
		if bytes.Equal(pair.old, pair.next) {
			continue
		}
		path := filepath.Join(p.path, pair.name)
		mode := os.FileMode(0644)
		info, e := catalogfs.Stat(path)
		existed := e == nil
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if existed {
			mode = info.Mode().Perm()
		}
		stage := func(data []byte) (string, error) {
			f, e := os.CreateTemp(p.path, ".rommeta-edit-*.tmp")
			if e != nil {
				return "", e
			}
			name := f.Name()
			ok := false
			defer func() {
				f.Close()
				if !ok {
					os.Remove(name)
				}
			}()
			if _, e = f.Write(data); e != nil {
				return "", e
			}
			if e = f.Chmod(mode); e != nil {
				return "", e
			}
			if e = f.Sync(); e != nil {
				return "", e
			}
			if e = f.Close(); e != nil {
				return "", e
			}
			ok = true
			return name, nil
		}
		temp, e := stage(pair.next)
		if e != nil {
			return e
		}
		s := staged{name: path, temp: temp, existed: existed}
		changes = append(changes, s)
		if !packed && existed {
			backup, e := stage(pair.old)
			if e != nil {
				return e
			}
			changes[len(changes)-1].rollback = backup
		}
	}
	if len(changes) == 0 {
		return nil
	}
	if packed {
		put := map[string]string{}
		for _, s := range changes {
			put[filepath.Base(s.name)] = s.temp
		}
		return catalogfs.PutFiles(p.path, put, nil)
	}
	for i, s := range changes {
		catalogchanges.Touch(s.name)
		if err = rename(s.temp, s.name); err != nil {
			for j := i - 1; j >= 0; j-- {
				old := changes[j]
				var e error
				if old.existed {
					e = os.Rename(old.rollback, old.name)
				} else {
					e = os.Remove(old.name)
				}
				if e != nil {
					// Retain only the exact recovery file if restoration itself failed.
					changes[j].rollback = ""
					return fmt.Errorf("save failed: %v; restore %s failed: %v; recovery file: %s", err, old.name, e, old.rollback)
				}
			}
			return err
		}
	}
	return nil
}

// A materialized draft is still untrusted input. Validate every new file identity;
// legacy untouched records remain editable without inventing missing dump data.
func validateFingerprintDraft(original, draft []byte) error {
	_, old, err := fingerprintDocument(original)
	if err != nil {
		return err
	}
	_, next, err := fingerprintDocument(draft)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	fileKey := func(item *yaml.Node) string { b, _ := yaml.Marshal(catalogyaml.Value(item, "file")); return string(b) }
	for _, item := range old.Content {
		known[fileKey(item)] = true
	}
	for i, item := range next.Content {
		if !known[fileKey(item)] {
			if err = validateFingerprint(item); err != nil {
				return fmt.Errorf("fingerprint %d: %w", i+1, err)
			}
		}
	}
	return nil
}
