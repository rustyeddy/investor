package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/rustyeddy/investor/internal/firm"
)

// Handler exposes the manager namespace reserved by Tulip. Runs are synchronous
// and request-scoped; this initial service does not retain run history.
func Handler(c firm.Config, engine *firm.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"ok": true, "mode": "dry-run"})
	})
	for name, value := range map[string]any{"agents": c.Agents, "skills": c.Skills, "tools": c.Tools, "workflows": c.Workflows} {
		mux.HandleFunc("GET /api/manager/"+name, func(w http.ResponseWriter, r *http.Request) { write(w, 200, value) })
	}
	mux.HandleFunc("POST /api/manager/runs", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Workflow  string `json:"workflow"`
			Objective string `json:"objective"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		d.DisallowUnknownFields()
		if err := d.Decode(&request); err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := d.Decode(new(any)); err != io.EOF {
			write(w, 400, map[string]string{"error": "expected one JSON request"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		run, err := engine.Run(ctx, request.Workflow, request.Objective)
		if errors.Is(err, firm.ErrUnknownWorkflow) {
			write(w, 404, map[string]string{"error": err.Error()})
			return
		}
		if err != nil {
			write(w, 500, run)
			return
		}
		write(w, 200, run)
	})
	return mux
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
