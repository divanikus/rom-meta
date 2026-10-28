package emumovies

import (
	"rommeta/internal/fetchstate"
	"rommeta/internal/taskprogress"
)

// The recorder wraps the transport, so every attempted file (including archive
// parts) has an outcome even when later metadata or extraction fails.
type downloadRecorder struct {
	root, kind string
	client     FTPClient
	report     *fetchstate.Writer
	rows       map[string]fetchstate.Row
	transfers  *taskprogress.Transfers
}

func newDownloadRecorder(root, kind string, apply bool, client FTPClient, transfers *taskprogress.Transfers) (*downloadRecorder, error) {
	w, err := fetchstate.Open(root, "emumovies", kind, apply)
	if err != nil {
		return nil, err
	}
	return &downloadRecorder{root: root, kind: kind, client: client, report: w, rows: map[string]fetchstate.Row{}, transfers: transfers}, nil
}
func (r *downloadRecorder) add(source, target string, row fetchstate.Row) {
	row["source"] = "emumovies"
	row["kind"] = r.kind
	row["source_path"] = source
	row["target"] = relativePlanPath(r.root, target)
	r.rows[source+"\x00"+target] = row
}
func (r *downloadRecorder) List(path string) ([]FTPEntry, error) { return r.client.List(path) }
func (r *downloadRecorder) Download(path, target string) error {
	return r.DownloadWithProgress(path, target, 0, nil, path, 0, 0)
}
func (r *downloadRecorder) DownloadWithProgress(path, target string, size int64, progress taskprogress.Func, item string, worker, workers int) error {
	err := downloadWithProgress(r.client, path, target, size, progress, item, worker, workers)
	status := "downloaded"
	if err != nil {
		status = "failed"
	}
	r.transfers.Record(status)
	if reportErr := r.record(path, target, status, err); reportErr != nil {
		return reportErr
	}
	return err
}
func (r *downloadRecorder) record(path, target, status string, cause error) error {
	row := fetchstate.Row{}
	for key, value := range r.rows[path+"\x00"+target] {
		row[key] = value
	}
	row["status"] = status
	if cause != nil {
		row["error"] = cause.Error()
	}
	return r.report.Write(row)
}
