package serveui

import (
	"archive/zip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rommeta/internal/catalogchanges"
	"rommeta/internal/catalogedit"
	"rommeta/internal/dat"
	"rommeta/internal/exporter"
	"rommeta/internal/exportjob"
)

const maxDATUpload = 256 << 20
const maxDATExpanded = 512 << 20
const maxLibraryOperations = 32

// Operation records belong to this server, never to the initiating HTTP request.
// Inactive receipts expire after 24 hours, dismissal or server restart. Uploaded
// inputs are removed after success/dismissal/expiry in this process.
type libraryOperation struct {
	Export          *exportState `json:"export,omitempty"`
	exportJob       *exportjob.Job
	exportDocuments map[string]string
	exportStarted   bool
	ID              string             `json:"id"`
	Kind            string             `json:"kind"`
	Enrichment      *enrichmentState   `json:"enrichment,omitempty"`
	Filename        string             `json:"filename"`
	State           string             `json:"state"`
	Created         time.Time          `json:"created"`
	Updated         time.Time          `json:"updated"`
	Revision        int                `json:"revision"`
	Member          string             `json:"member,omitempty"`
	Members         []string           `json:"members,omitempty"`
	Platform        string             `json:"platform,omitempty"`
	Progress        dat.ImportProgress `json:"progress"`
	Summary         *importSummary     `json:"summary,omitempty"`
	Error           string             `json:"error,omitempty"`
	Warning         string             `json:"warning,omitempty"`
	path            string
	stamp           string
	dir             string
}
type importSummary struct {
	Dataset      string           `json:"dataset"`
	Format       string           `json:"format"`
	Authority    string           `json:"authority"`
	Platform     string           `json:"platform"`
	Games        int              `json:"games"`
	Releases     int              `json:"releases"`
	Fingerprints int              `json:"fingerprints"`
	Added        int              `json:"added"`
	Reconciled   int              `json:"reconciled"`
	Action       string           `json:"action"`
	Conflicts    []string         `json:"conflicts,omitempty"`
	Samples      []dat.GameSample `json:"samples,omitempty"`
	Written      int              `json:"written"`
}
type importAction struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Member   string `json:"member,omitempty"`
	Platform string `json:"platform,omitempty"`
}

func operationBusyState(state string) bool {
	return state == "planning" || state == "importing" || state == "enriching" || state == "exporting"
}
func operationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func summarizeImport(result dat.ImportResult) *importSummary {
	return &importSummary{Dataset: result.Plan.DatasetName, Format: result.Plan.Format, Authority: result.Plan.Authority,
		Platform: filepath.Base(result.PlatformPath), Games: result.GameBundleCount, Releases: result.ReleaseCount, Fingerprints: result.FingerprintCount,
		Added: result.Changes.Added, Reconciled: result.Changes.Reconciled, Action: result.Changes.Action, Conflicts: result.Changes.Conflicts, Samples: result.SampleGames, Written: result.WrittenFileCount}
}
func (s *server) operationSnapshot(id string) (libraryOperation, bool) {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op, ok := s.operations[id]
	if !ok {
		return libraryOperation{}, false
	}
	return cloneOperation(op), true
}
func (s *server) changeOperation(id string, update func(*libraryOperation)) {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	if op := s.operations[id]; op != nil {
		update(op)
		op.Updated = time.Now().UTC()
	}
}
func (s *server) expireOperationsLocked() {
	for id, op := range s.operations {
		if !operationBusyState(op.State) && time.Since(op.Updated) > 24*time.Hour {
			if op.dir != "" {
				_ = os.RemoveAll(op.dir)
			}
			delete(s.operations, id)
		}
	}
}
func (s *server) operationsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.operationsMu.Lock()
	s.expireOperationsLocked()
	result := make([]libraryOperation, 0, len(s.operations))
	for _, op := range s.operations {
		result = append(result, cloneOperation(op))
	}
	s.operationsMu.Unlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, result)
}
func uploadMembers(path string) ([]string, error) {
	if strings.ToLower(filepath.Ext(path)) != ".zip" {
		return nil, nil
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read ZIP: %w", err)
	}
	defer archive.Close()
	var members []string
	seen := map[string]bool{}
	for _, file := range archive.File {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if file.FileInfo().IsDir() || (ext != ".dat" && ext != ".xml") {
			continue
		}
		if file.UncompressedSize64 > maxDATExpanded {
			return nil, fmt.Errorf("DAT member exceeds the 512 MiB expanded limit")
		}
		if seen[file.Name] {
			return nil, fmt.Errorf("ZIP contains duplicate DAT member names")
		}
		seen[file.Name] = true
		members = append(members, file.Name)
		if len(members) > 256 {
			return nil, fmt.Errorf("ZIP contains more than 256 DAT members; upload a smaller archive")
		}
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("ZIP contains no .dat or .xml files")
	}
	sort.Strings(members)
	return members, nil
}
func (s *server) importUpload(w http.ResponseWriter, r *http.Request) {
	if !s.catalogWriteAllowed(w, r) {
		return
	}
	// A slower local upload must not inherit the listener's short request timeout.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, maxDATUpload+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, 415, fmt.Errorf("upload one DAT/XML/ZIP file as multipart/form-data"))
		return
	}
	s.operationsMu.Lock()
	s.expireOperationsLocked()
	if len(s.operations)+s.uploads >= maxLibraryOperations {
		s.operationsMu.Unlock()
		writeError(w, 409, fmt.Errorf("dismiss an old operation before uploading another DAT"))
		return
	}
	s.uploads++
	s.operationsMu.Unlock()
	defer func() { s.operationsMu.Lock(); s.uploads--; s.operationsMu.Unlock() }()
	runtime := filepath.Join(s.realRoot, ".rommeta")
	if err = os.MkdirAll(runtime, 0755); err != nil {
		writeError(w, 500, err)
		return
	}
	info, err := os.Lstat(runtime)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		writeError(w, 400, fmt.Errorf("catalog runtime directory is unavailable or a symlink"))
		return
	}
	dir, err := os.MkdirTemp(runtime, "web-import-")
	if err != nil {
		writeError(w, 500, err)
		return
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	var filename, path string
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			writeError(w, 400, e)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" || filename != "" {
			_ = part.Close()
			writeError(w, 400, fmt.Errorf("send exactly one file"))
			return
		}
		filename = part.FileName()
		if len(filename) > 180 || strings.ContainsAny(filename, "/\\\x00\r\n") || filename == "." || filename == ".." {
			writeError(w, 400, fmt.Errorf("invalid upload filename"))
			return
		}
		ext := strings.ToLower(filepath.Ext(filename))
		if ext != ".dat" && ext != ".xml" && ext != ".zip" {
			writeError(w, 400, fmt.Errorf("choose a supported XML DAT (.dat or .xml), or ZIP"))
			return
		}
		path = filepath.Join(dir, filename)
		file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			writeError(w, 500, e)
			return
		}
		n, e := io.Copy(file, io.LimitReader(part, maxDATUpload+1))
		closeErr := file.Close()
		_ = part.Close()
		if e != nil || closeErr != nil || n == 0 || n > maxDATUpload {
			writeError(w, 400, fmt.Errorf("upload failed or exceeded 256 MiB; send the file again"))
			return
		}
	}
	if filename == "" {
		writeError(w, 400, fmt.Errorf("choose a file to upload"))
		return
	}
	members, err := uploadMembers(path)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	op := &libraryOperation{ID: operationID(), Kind: "dat-import", Filename: filename, State: "needs_selection", Created: time.Now().UTC(), Updated: time.Now().UTC(), Revision: 1, Members: members, path: path, dir: dir}
	if len(members) == 1 {
		op.Member = members[0]
	}
	s.operationsMu.Lock()
	if s.operations == nil {
		s.operations = map[string]*libraryOperation{}
	}
	s.operations[op.ID] = op
	s.operationsMu.Unlock()
	keep = true
	if len(members) < 2 {
		if err := s.beginImport(op.ID, 1, op.Member, "", false); err != nil {
			s.changeOperation(op.ID, func(current *libraryOperation) { current.Error = s.importError(err, *current) })
		}
	}
	snapshot, _ := s.operationSnapshot(op.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, snapshot)
}
func (s *server) importPlan(w http.ResponseWriter, r *http.Request) {
	var input importAction
	if !s.editRequest(w, r, &input) {
		return
	}
	if err := s.beginImport(input.ID, input.Revision, input.Member, input.Platform, false); err != nil {
		writeError(w, 409, err)
		return
	}
	op, _ := s.operationSnapshot(input.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, op)
}
func (s *server) importApply(w http.ResponseWriter, r *http.Request) {
	var input importAction
	if !s.editRequest(w, r, &input) {
		return
	}
	if err := s.beginImport(input.ID, input.Revision, "", "", true); err != nil {
		writeError(w, 409, err)
		return
	}
	op, _ := s.operationSnapshot(input.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, op)
}
func (s *server) operationDismiss(w http.ResponseWriter, r *http.Request) {
	var input importAction
	if !s.editRequest(w, r, &input) {
		return
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op := s.operations[input.ID]
	if op == nil {
		writeJSON(w, map[string]bool{"dismissed": true})
		return
	}
	if operationBusyState(op.State) {
		writeError(w, 409, fmt.Errorf("operation is still running; closing the browser will not interrupt it"))
		return
	}
	if op.dir != "" {
		if err := os.RemoveAll(op.dir); err != nil {
			writeError(w, 500, err)
			return
		}
	}
	delete(s.operations, input.ID)
	writeJSON(w, map[string]bool{"dismissed": true})
}
func (s *server) beginImport(id string, revision int, member, platform string, apply bool) error {
	s.operationsMu.Lock()
	op := s.operations[id]
	if op == nil {
		s.operationsMu.Unlock()
		return fmt.Errorf("operation no longer exists; upload the DAT again")
	}
	if op.Kind != "dat-import" {
		s.operationsMu.Unlock()
		return fmt.Errorf("this operation is not a DAT import")
	}
	// A repeated accepted command is a lookup, never a second worker.
	if operationBusyState(op.State) || (apply && op.State == "succeeded") {
		s.operationsMu.Unlock()
		return nil
	}
	if op.Revision != revision {
		s.operationsMu.Unlock()
		return fmt.Errorf("the plan changed; review the current operation")
	}
	if apply && op.State != "ready" {
		s.operationsMu.Unlock()
		return fmt.Errorf("prepare and review the import plan first")
	}
	if !apply {
		if strings.TrimSpace(platform) != "" && !catalogedit.Component(platform) {
			s.operationsMu.Unlock()
			return fmt.Errorf("invalid platform name")
		}
		if len(op.Members) > 0 {
			found := false
			for _, name := range op.Members {
				if name == member {
					found = true
				}
			}
			if !found {
				s.operationsMu.Unlock()
				return fmt.Errorf("select a DAT from this ZIP")
			}
		} else if member != "" {
			s.operationsMu.Unlock()
			return fmt.Errorf("this file is not a ZIP")
		}
	}
	if !s.editMu.TryLock() {
		s.operationsMu.Unlock()
		return fmt.Errorf("another catalog operation is running; try again when it finishes")
	}
	unlock, err := catalogedit.Lock(s.root)
	if err != nil {
		s.editMu.Unlock()
		s.operationsMu.Unlock()
		return err
	}
	stamp, stampErr := s.importStamp()
	if stampErr != nil || (apply && stamp != op.stamp) {
		unlock()
		s.editMu.Unlock()
		s.operationsMu.Unlock()
		return fmt.Errorf("library or import settings changed; prepare the plan again")
	}
	if !apply {
		op.Member = member
		op.Platform = strings.TrimSpace(platform)
		op.Revision++
		op.Summary = nil
		op.stamp = stamp
		op.State = "planning"
	} else {
		op.State = "importing"
	}
	op.Error = ""
	op.Warning = ""
	op.Progress = dat.ImportProgress{Stage: op.State}
	op.Updated = time.Now().UTC()
	snapshot := *op
	s.operationsMu.Unlock()
	go func() { defer s.editMu.Unlock(); defer unlock(); s.runImport(snapshot, apply) }()
	return nil
}
func (s *server) runImport(op libraryOperation, apply bool) {
	// Test seam uses the same progress callback and deterministic disconnect checks.
	run := s.datImport
	if run == nil {
		run = dat.Import
	}
	options, err := dat.LibraryOptions(dat.ImportOptions{DataRoot: s.root, MemberPath: op.Member, PlatformOverride: op.Platform, Apply: apply})
	var result dat.ImportResult
	var batch *catalogchanges.Batch
	if err == nil {
		options.Progress = func(p dat.ImportProgress) {
			s.changeOperation(op.ID, func(current *libraryOperation) { current.Progress = p })
		}
		format, e := dat.DetectFormat(op.path, op.Member)
		err = e
		if err == nil && format == "mame-listxml" {
			err = fmt.Errorf("MAME listxml needs the classified arcade workflow. Use rom-meta dat import with the CLI; this browser importer accepts ordinary XML DATs")
		}
	}
	if err == nil {
		if apply {
			batch = catalogchanges.Begin(exporter.RefreshPlatformIndex)
		}
		result, err = run(op.path, options)
	}
	warning := ""
	if batch != nil {
		if e := batch.Finish(); e != nil {
			warning = e.Error()
		}
	}
	if apply {
		s.summaryMu.Lock()
		s.summaries = nil
		s.summaryMu.Unlock()
	}
	if err == nil && !apply {
		err = dat.ValidateImportPlan(result.Plan)
	}
	if apply && err == nil {
		if cleanupErr := os.RemoveAll(op.dir); cleanupErr != nil {
			warning += " Uploaded file cleanup: " + s.importError(cleanupErr, op)
		}
	}
	s.changeOperation(op.ID, func(current *libraryOperation) {
		current.Warning = warning
		if result.PlatformPath != "" {
			current.Summary = summarizeImport(result)
		}
		if err != nil {
			current.Error = s.importError(err, op)
			current.State = "failed"
			return
		}
		if len(result.Changes.Conflicts) > 0 {
			current.State = "needs_review"
			return
		}
		if !apply {
			current.State = "ready"
			return
		}
		current.State = "succeeded"
		current.Progress = dat.ImportProgress{Stage: "complete", Current: result.GameBundleCount, Total: result.GameBundleCount}
	})
}
func (s *server) importError(err error, op libraryOperation) string {
	message := err.Error()
	message = strings.ReplaceAll(message, op.dir, "uploaded DAT")
	message = strings.ReplaceAll(message, s.realRoot, "library")
	message = strings.ReplaceAll(message, s.root, "library")
	return message
}

func (s *server) importStamp() (string, error) {
	hash := sha256.New()
	for _, name := range []string{catalogchanges.RevisionFile, "config/catalog.yaml", "config/platforms.yaml"} {
		data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(name)))
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		hash.Write([]byte(name))
		hash.Write(data)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
