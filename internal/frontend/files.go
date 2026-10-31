package frontend

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type fingerprint struct {
	exists bool
	hash   string
}
type fileChange struct {
	root, path, source, hash string
	data                     []byte
}

func digest(name string) (fingerprint, error) {
	f, err := os.Open(name)
	if os.IsNotExist(err) {
		return fingerprint{}, nil
	}
	if err != nil {
		return fingerprint{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fingerprint{}, err
	}
	if !info.Mode().IsRegular() {
		return fingerprint{}, fmt.Errorf("not a regular file: %s", name)
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return fingerprint{}, err
	}
	return fingerprint{true, hex.EncodeToString(h.Sum(nil))}, nil
}
func hashBytes(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func canonicalRoot(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("source and destination roots are required")
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}
func inside(root, name string) bool {
	rel, err := filepath.Rel(root, name)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func safeJoin(root, relative string) (string, error) {
	relative = strings.ReplaceAll(relative, "\\", "/")
	for _, part := range strings.Split(relative, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "<>:\"|?*\r\n\x00") || strings.TrimRight(part, " .") != part {
			return "", fmt.Errorf("unsafe package/library path: %q", relative)
		}
	}
	name := filepath.Join(root, filepath.FromSlash(relative))
	if !inside(root, name) {
		return "", fmt.Errorf("path escapes root")
	}
	for current := name; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if current == root {
				break
			}
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("linked package/library paths are unsupported: %s", current)
		}
		if current == root {
			break
		}
	}
	return name, nil
}
func (p *Plan) read(root, relative string, optional bool) ([]byte, error) {
	if root == p.Package && p.source != nil {
		if _, err := safeJoin(root, relative); err != nil {
			return nil, err
		}
		file, ok := p.source.Files[relative]
		if !ok {
			if optional {
				return nil, nil
			}
			return nil, fmt.Errorf("missing generated file: %s", relative)
		}
		if file.Data == nil {
			return nil, fmt.Errorf("generated metadata bytes missing: %s", relative)
		}
		return file.Data, nil
	}
	name, err := safeJoin(root, relative)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(name)
	if os.IsNotExist(err) && optional {
		p.guards[name] = fingerprint{}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.guards[name] = fingerprint{true, hashBytes(data)}
	return data, nil
}
func (p *Plan) write(relative string, data []byte, source string) (bool, error) {
	return p.writeTo(p.Root, relative, data, source)
}
func (p *Plan) writeTo(root, relative string, data []byte, source string) (bool, error) {
	name, err := safeJoin(root, relative)
	if err != nil {
		return false, err
	}
	old, err := digest(name)
	if err != nil {
		return false, err
	}
	p.guards[name] = old
	want := hashBytes(data)
	if source != "" {
		f, err := p.packageProof(source)
		if err != nil {
			return false, err
		}
		if !f.exists {
			return false, fmt.Errorf("missing package asset: %s", source)
		}
		if p.source == nil {
			p.guards[source] = f
		}
		want = f.hash
	}
	if old.exists && old.hash == want {
		return false, nil
	}
	if old.exists && source != "" {
		return false, fmt.Errorf("existing asset differs; refusing overwrite: %s", name)
	}
	for _, change := range p.files {
		if change.root == root && strings.EqualFold(change.path, relative) {
			if change.hash != want {
				return false, fmt.Errorf("conflicting planned output: %s", relative)
			}
			return false, nil
		}
	}
	p.files = append(p.files, fileChange{root: root, path: relative, source: source, hash: want, data: data})
	display := relative
	if root != p.Root {
		display = filepath.Join(root, filepath.FromSlash(relative))
	}
	p.Files = append(p.Files, display)
	return true, nil
}
func (p *Plan) validate() error {
	checked, total := 0, len(p.external)+len(p.guards)
	for name, expected := range p.external {
		p.reportProgress("check files", filepath.Base(name), checked, total)
		checked++
		got, err := digest(name)
		if err != nil {
			return err
		}
		if got != expected {
			return fmt.Errorf("playlist changed since planning: %s", name)
		}
	}
	for name, expected := range p.guards {
		p.reportProgress("check files", filepath.Base(name), checked, total)
		checked++
		root := p.Root
		if inside(p.Package, name) {
			root = p.Package
		} else if p.MediaRoot != "" && inside(p.MediaRoot, name) {
			root = p.MediaRoot
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if _, err = safeJoin(root, relative); err != nil {
			return err
		}
		got, err := digest(name)
		if err != nil {
			return err
		}
		if got != expected {
			return fmt.Errorf("file changed since planning; rebuild merge plan: %s", name)
		}
	}
	p.reportProgress("check files", "", total, total)
	return nil
}
func checkProcesses(target string) error {
	if runtime.GOOS == "windows" {
		body, err := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
		if err != nil {
			return fmt.Errorf("cannot check frontend processes: %w", err)
		}
		rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
		if err != nil {
			return err
		}
		names := []string{"LaunchBox.exe", "BigBox.exe"}
		if target == "es-de" {
			names = []string{"ES-DE.exe", "emulationstation.exe"}
		}
		for _, row := range rows {
			for _, name := range names {
				if len(row) > 0 && strings.EqualFold(row[0], name) {
					return fmt.Errorf("close %s before applying the export", name)
				}
			}
		}
	} else if target == "es-de" && runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return fmt.Errorf("cannot check ES-DE processes: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == "" || entry.Name()[0] < '0' || entry.Name()[0] > '9' {
				continue
			}
			comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
			if err != nil {
				continue
			}
			name := strings.TrimSpace(string(comm))
			if name == "ES-DE" || name == "es-de" || name == "emulationstatio" {
				return fmt.Errorf("close ES-DE before applying the export")
			}
		}
	}
	return nil
}

// Check validates a reviewed native plan before expensive staging/downloads.
func (p *Plan) Check() error {
	if len(p.Conflicts) > 0 {
		return fmt.Errorf("export has %d conflicts; no destination changes applied", len(p.Conflicts))
	}
	if len(p.files) == 0 {
		return nil
	}
	if err := checkProcesses(p.Target); err != nil {
		return err
	}
	return p.validate()
}

func exclusiveCopy(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return closeErr
	}
	return nil
}

// Apply checks the reviewed inputs again and backs up every file it replaces.
// Failures report the backup; automatic rollback never overwrites newer data.
func (p *Plan) Apply() (backup string, err error) {
	if err = p.Check(); err != nil {
		return "", err
	}
	if len(p.files) == 0 {
		return "", nil
	}
	if p.source != nil {
		return "", fmt.Errorf("generated preview must be materialized before apply")
	}
	parent, err := safeJoin(p.Root, "Backups/RomMeta")
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	backup, err = os.MkdirTemp(parent, time.Now().Format("20060102-150405")+"-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w; recovery files and planned changes: %s", err, backup)
		}
	}()
	type receiptFile struct {
		Root   string `json:"root"`
		Path   string `json:"path"`
		New    bool   `json:"new"`
		SHA256 string `json:"sha256"`
	}
	var receipt []receiptFile
	for _, c := range p.files {
		name, _ := safeJoin(c.root, c.path)
		old := p.guards[name]
		receipt = append(receipt, receiptFile{c.root, c.path, !old.exists, c.hash})
		if old.exists {
			target := filepath.Join(backup, filepath.FromSlash(c.path))
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return backup, err
			}
			if err = exclusiveCopy(name, target); err != nil {
				return backup, err
			}
		}
	}
	data, _ := json.MarshalIndent(receipt, "", "  ")
	if err = os.WriteFile(filepath.Join(backup, "changes.json"), append(data, '\n'), 0o600); err != nil {
		return backup, err
	}
	if err = p.validate(); err != nil {
		return backup, err
	}
	// Assets first; platform XML and identity map are published afterwards.
	for i, c := range p.files {
		if p.Progress != nil {
			p.Progress("write", c.path, i, len(p.files))
		}
		target, e := safeJoin(c.root, c.path)
		if e != nil {
			return backup, e
		}
		if e = os.MkdirAll(filepath.Dir(target), 0o755); e != nil {
			return backup, e
		}
		current, e := digest(target)
		if e != nil {
			return backup, e
		}
		if current != p.guards[target] {
			return backup, fmt.Errorf("library changed during merge: %s", target)
		}
		err = p.publish(c, target, current)
		if err != nil {
			return backup, err
		}
	}
	if p.Progress != nil {
		p.Progress("write", "", len(p.files), len(p.files))
	}
	return backup, nil
}

// publish writes a new asset once or replaces metadata through one adjacent temp
// file. Internally generated packages may reuse immutable assets by hard link;
// the caller removes that task-owned package after apply, leaving final files.
func (p *Plan) publish(c fileChange, target string, current fingerprint) (err error) {
	if c.source != "" && p.reuseAssets && !current.exists {
		proof, e := digest(c.source)
		if e != nil {
			return e
		}
		if !proof.exists || proof.hash != c.hash {
			return fmt.Errorf("package asset changed: %s", c.path)
		}
		if e = os.Link(c.source, target); e == nil {
			return nil
		}
		// Different filesystems or unsupported links retain the one-copy path.
	}
	var in io.ReadCloser
	if c.source != "" {
		in, err = os.Open(c.source)
	} else {
		in = io.NopCloser(bytes.NewReader(c.data))
	}
	if err != nil {
		return err
	}
	defer in.Close()
	var out *os.File
	if current.exists {
		out, err = os.CreateTemp(filepath.Dir(target), ".rommeta-write-")
	} else {
		out, err = os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	if err != nil {
		return err
	}
	temporary := out.Name()
	committed := false
	defer func() {
		out.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(out, hash), in); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != c.hash {
		return fmt.Errorf("package asset changed while writing: %s", c.path)
	}
	if current.exists {
		proof, e := digest(target)
		if e != nil {
			return e
		}
		if proof != current {
			return fmt.Errorf("library changed during merge: %s", target)
		}
		if err = os.Chmod(temporary, 0o644); err != nil {
			return err
		}
		if err = os.Rename(temporary, target); err != nil {
			return err
		}
	}
	committed = true
	return nil
}
