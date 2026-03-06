package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
)

// job tracks an async pull/export operation.
type job struct {
	mu     sync.Mutex
	busy   atomic.Bool
	lastOK bool
	lastErr string
}

var globalJob job

func (a *App) startAPIServer(ctx context.Context) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /status", a.apiStatus)
	mux.HandleFunc("GET /models", a.apiModels)
	mux.HandleFunc("POST /models/pull", a.apiPull)
	mux.HandleFunc("POST /models/export", a.apiExport)
	mux.HandleFunc("GET /job", apiJobStatus)

	addr := ":" + strconv.Itoa(a.config.APIPort)
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background()) //nolint: errcheck
	}()
	go srv.ListenAndServe() //nolint: errcheck
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) //nolint: errcheck
}

func (a *App) apiStatus(w http.ResponseWriter, _ *http.Request) {
	s := a.CheckStatus()
	writeJSON(w, http.StatusOK, map[string]any{
		"running":           a.IsOVMSRunning(),
		"deps_ready":        s.DepsReady,
		"ovms_ready":        s.OvmsReady,
		"version":           s.OvmsVersion,
		"available_devices": a.GetAvailableDevices(),
	})
}

func (a *App) apiModels(w http.ResponseWriter, _ *http.Request) {
	models, err := a.GetInstalledModels()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, models)
}

type pullRequest struct {
	ModelID     string `json:"model_id"`
	TargetDevice string `json:"target_device"`
	PipelineTag  string `json:"pipeline_tag"`
}

func (a *App) apiPull(w http.ResponseWriter, r *http.Request) {
	var req pullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !globalJob.busy.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "another operation is in progress"})
		return
	}
	go func() {
		defer globalJob.busy.Store(false)
		err := a.PullModel(req.ModelID, req.TargetDevice, req.PipelineTag)
		globalJob.mu.Lock()
		if err != nil {
			globalJob.lastErr = err.Error()
			globalJob.lastOK = false
		} else {
			globalJob.lastErr = ""
			globalJob.lastOK = true
		}
		globalJob.mu.Unlock()
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

type exportRequest struct {
	ModelID      string         `json:"model_id"`
	TargetDevice string         `json:"target_device"`
	Task         string         `json:"task"` // "text-generation" or "feature-extraction"
	ExtraOpts    map[string]any `json:"extra_opts"`
}

func (a *App) apiExport(w http.ResponseWriter, r *http.Request) {
	var req exportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !globalJob.busy.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "another operation is in progress"})
		return
	}
	go func() {
		defer globalJob.busy.Store(false)
		var err error
		switch req.Task {
		case "text-generation":
			err = a.ExportTextGen(req.ModelID, req.TargetDevice, req.ExtraOpts)
		case "feature-extraction":
			err = a.ExportEmbeddings(req.ModelID, req.TargetDevice, req.ExtraOpts)
		default:
			err = fmt.Errorf("unsupported task %q", req.Task)
		}
		globalJob.mu.Lock()
		if err != nil {
			globalJob.lastErr = err.Error()
			globalJob.lastOK = false
		} else {
			globalJob.lastErr = ""
			globalJob.lastOK = true
		}
		globalJob.mu.Unlock()
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func apiJobStatus(w http.ResponseWriter, _ *http.Request) {
	busy := globalJob.busy.Load()
	globalJob.mu.Lock()
	lastOK := globalJob.lastOK
	lastErr := globalJob.lastErr
	globalJob.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"busy":    busy,
		"last_ok": lastOK,
		"last_error": lastErr,
	})
}
