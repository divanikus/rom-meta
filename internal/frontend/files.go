package frontend

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type fingerprint struct {
	exists bool
	hash   string
}
type fileChange struct {
	root, path, source, hash string
	data                     []byte
	size                     int64
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
	var sourceSize int64
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
		if p.source != nil {
			file, err := p.sourceFile(source)
			if err != nil {
				return false, err
			}
			sourceSize = file.Size
			if file.Data != nil {
				sourceSize = int64(len(file.Data))
			}
		}
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
	p.files = append(p.files, fileChange{root: root, path: relative, source: source, hash: want, data: data, size: sourceSize})
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

func (p *Plan) checkReady() error {
	if len(p.Conflicts) > 0 {
		return fmt.Errorf("export has %d conflicts; no destination changes applied", len(p.Conflicts))
	}
	if len(p.files) == 0 {
		return nil
	}
	return checkProcesses(p.Target)
}

// Check validates a reviewed native plan before destination writes.
func (p *Plan) Check() error {
	if err := p.checkReady(); err != nil {
		return err
	}
	if len(p.files) == 0 {
		return nil
	}
	return p.validate()
}

// Apply validates the reviewed inputs and writes only planned destination files.
func (p *Plan) Apply() error {
	if err := p.checkReady(); err != nil {
		return err
	}
	if len(p.files) == 0 {
		return nil
	}
	if p.source != nil {
		if p.source.Check == nil {
			return fmt.Errorf("generated preview has no input validation")
		}
		for _, c := range p.files {
			if c.source != "" {
				file, e := p.sourceFile(c.source)
				if e != nil {
					return e
				}
				if file.Data == nil && (file.Open == nil || file.Size <= 0) {
					return fmt.Errorf("generated preview has no asset reader: %s", c.path)
				}
			}
		}
		if err := p.source.Check(); err != nil {
			return err
		}
	}
	if err := p.validate(); err != nil {
		return err
	}
	// Assets first; native XML is published afterwards.
	for i, c := range p.files {
		if p.Progress != nil {
			p.Progress("write", c.path, i, len(p.files))
		}
		target, e := safeJoin(c.root, c.path)
		if e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(target), 0o755); e != nil {
			return e
		}
		current, e := digest(target)
		if e != nil {
			return e
		}
		if current != p.guards[target] {
			return fmt.Errorf("library changed during merge: %s", target)
		}
		if err := p.publish(&p.files[i], target, current); err != nil {
			return err
		}
	}
	if p.Progress != nil {
		p.Progress("write", "", len(p.files), len(p.files))
	}
	return nil
}

// publish writes a new asset once or replaces metadata through one adjacent temp
// file. Direct assets are streamed from their original source; package assets
// are independently copied.
func (p *Plan) publish(c *fileChange, target string, current fingerprint) (err error) {
	var in io.ReadCloser
	if c.source != "" && p.source != nil {
		var file SourceFile
		file, err = p.sourceFile(c.source)
		if err == nil {
			if file.Data != nil {
				in = io.NopCloser(bytes.NewReader(file.Data))
			} else {
				in, err = file.Open()
			}
		}
	} else if c.source != "" {
		in, err = os.Open(c.source)
	} else {
		in = io.NopCloser(bytes.NewReader(c.data))
	}
	if err != nil {
		return err
	}
	inputClosed := false
	defer func() {
		if !inputClosed {
			_ = in.Close()
		}
	}()
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
	var reader io.Reader = in
	if c.source != "" && p.source != nil {
		reader = io.LimitReader(in, c.size+1)
	}
	size, copyErr := io.Copy(io.MultiWriter(out, hash), reader)
	closeErr := in.Close()
	inputClosed = true
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if c.source != "" && p.source != nil && size != c.size {
		return fmt.Errorf("source asset size changed while writing: %s", c.path)
	}
	if err = out.Close(); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if strings.HasPrefix(c.hash, "deferred:") {
		c.hash = actual
	} else if actual != c.hash {
		return fmt.Errorf("source asset changed while writing: %s", c.path)
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
