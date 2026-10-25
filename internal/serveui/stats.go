package serveui

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"rommeta/internal/catalogstate"
	"rommeta/internal/taskprogress"
)

// A request owns its scan. No background task or writable cache survives navigation.
// Streaming failures use a terminal error frame because HTTP headers are sent early.
func (s *server) streamStats(w http.ResponseWriter, r *http.Request, options catalogstate.StatusOptions) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	options.Context = ctx
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	encoder := json.NewEncoder(w)
	send := func(value any) {
		if err := encoder.Encode(value); err != nil {
			cancel()
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	var last time.Time
	options.Progress = func(p taskprogress.Snapshot) {
		if ctx.Err() != nil {
			return
		}
		if time.Since(last) < 250*time.Millisecond {
			return
		}
		last = time.Now()
		send(map[string]any{"progress": p})
	}
	status, err := catalogstate.BuildStatus(options)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		send(map[string]any{"error": err.Error()})
		return
	}
	send(map[string]any{"result": status})
}
