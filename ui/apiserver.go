package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"
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
	mux.HandleFunc("GET /models/test", a.apiTestModels)
	mux.HandleFunc("POST /models/pull", a.apiPull)
	mux.HandleFunc("POST /models/export", a.apiExport)
	mux.HandleFunc("DELETE /models/{model_name}", a.apiDeleteModel)
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
			runtime.EventsEmit(a.ctx, "models-changed", nil)
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
		err := a.exportWithScript(req.ModelID, req.TargetDevice, req.Task, req.ExtraOpts)
		globalJob.mu.Lock()
		if err != nil {
			globalJob.lastErr = err.Error()
			globalJob.lastOK = false
		} else {
			globalJob.lastErr = ""
			globalJob.lastOK = true
			runtime.EventsEmit(a.ctx, "models-changed", nil)
		}
		globalJob.mu.Unlock()
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// ovmsPost sends a JSON request to OVMS and returns the decoded response body.
func ovmsPost(url string, payload any) (int, json.RawMessage, error) {
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body)) //nolint: noctx
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, json.RawMessage(raw), nil
}

// apiTestModels tests all installed models with a "hello" prompt.
// It auto-detects the model type: names containing "embed" use the embeddings
// endpoint; all others use chat completions.
func (a *App) apiTestModels(w http.ResponseWriter, _ *http.Request) {
	models, err := a.GetInstalledModels()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ovmsBase := "http://localhost:" + strconv.Itoa(a.config.OVMSRestPort)
	type result struct {
		Model    string          `json:"model"`
		Type     string          `json:"type"`
		Status   int             `json:"status"`
		Response json.RawMessage `json:"response,omitempty"`
		Error    string          `json:"error,omitempty"`
	}

	results := make([]result, 0, len(models))
	for _, m := range models {
		var (
			url     string
			payload any
			mtype   string
		)
		if pipelineDefs[m.Task].IsEmbedding {
			mtype = "embeddings"
			url = ovmsBase + "/v3/embeddings"
			payload = map[string]any{"model": m.Name, "input": "hello"}
		} else {
			mtype = "text-generation"
			url = ovmsBase + "/v3/chat/completions"
			payload = map[string]any{
				"model":      m.Name,
				"max_tokens": 50,
				"messages":   []map[string]string{{"role": "user", "content": "hello"}},
			}
		}
		code, raw, reqErr := ovmsPost(url, payload)
		r := result{Model: m.Name, Type: mtype, Status: code, Response: raw}
		if reqErr != nil {
			r.Error = reqErr.Error()
		}
		results = append(results, r)
	}

	writeJSON(w, http.StatusOK, results)
}


func (a *App) apiDeleteModel(w http.ResponseWriter, r *http.Request) {
	modelName := r.PathValue("model_name")
	if modelName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model_name is required"})
		return
	}
	if err := a.DeleteInstalledModel(modelName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	runtime.EventsEmit(a.ctx, "models-changed", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
