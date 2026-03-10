package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/turintech/openvino-desktop/ui/internal/setup"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultOvmsURL = "https://github.com/openvinotoolkit/model_server/releases/download/v2026.0/ovms_windows_python_on.zip"
	defaultUvURL   = "https://github.com/turintech/openvino-desktop/releases/download/uv/uv.exe"
)

// Config holds user-configurable settings.
var defaultSearchTags = []string{"OpenVINO", "Qwen", "Embedding"}

type Config struct {
	InstallDir             string   `json:"install_dir"`
	OvmsURL                string   `json:"ovms_url"`
	UvURL                  string   `json:"uv_url"`
	StartupSet             bool     `json:"startup_set"`
	SearchTags             []string `json:"search_tags"`
	SearchLimit            int      `json:"search_limit"`
	TextGenTargetDevice    string   `json:"text_gen_target_device"`
	EmbeddingsTargetDevice string   `json:"embeddings_target_device"`
	APIPort                int      `json:"api_port"`
	OVMSRestPort           int      `json:"ovms_rest_port"`
}

// StatusResult reports whether each component is ready.
type StatusResult struct {
	DepsReady   bool   `json:"deps_ready"`
	OvmsReady   bool   `json:"ovms_ready"`
	OvmsVersion string `json:"ovms_version"`
}

// ovmsVersionFromURL extracts the version tag from an OVMS release URL.
// e.g. ".../download/v2026.0/ovms_windows..." → "2026.0"
func ovmsVersionFromURL(ovmsURL string) string {
	for _, part := range strings.Split(ovmsURL, "/") {
		if strings.HasPrefix(part, "v") && len(part) > 1 {
			return part[1:]
		}
	}
	return ""
}

// App is the Wails application struct.
type App struct {
	ctx      context.Context
	config   Config
	ovmsProc *exec.Cmd
	ovmsMu   sync.Mutex
	assets   embed.FS
}

func NewApp(assets embed.FS) *App {
	return &App{assets: assets}
}

// extractAssets copies embedded assets into installDir, preserving directory structure.
func (a *App) extractAssets() error {
	return fs.WalkDir(a.assets, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("assets", path)
		dest := filepath.Join(a.config.InstallDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		data, err := a.assets.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0755)
	})
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadConfig()
	// Kill any orphaned ovms.exe left by a previous process (e.g. wails dev hot-reload).
	tk := exec.Command("taskkill", "/F", "/T", "/IM", "ovms.exe")
	hideWindow(tk)
	tk.Run() //nolint: errcheck
	a.extractAssets() //nolint: errcheck — best-effort on startup
	a.startAPIServer(ctx)
	// On first run, register the app to start with Windows by default.
	if !a.config.StartupSet {
		a.SetStartup(true) //nolint: errcheck — best-effort on first run
		a.config.StartupSet = true
		a.SaveConfig(a.config) //nolint: errcheck
	}
	// Gracefully shut down child processes on OS signals (e.g. Task Manager, Ctrl+C).
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		a.shutdown()
		os.Exit(0)
	}()
}

// shutdown stops OVMS and all child processes before the application exits.
func (a *App) shutdown() {
	a.stopAndWait()
	// Belt-and-suspenders: kill any ovms.exe still running.
	tk := exec.Command("taskkill", "/F", "/T", "/IM", "ovms.exe")
	hideWindow(tk)
	tk.Run() //nolint: errcheck
}

func defaultInstallDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "openvino-desktop")
}

func configPath() string {
	return filepath.Join(defaultInstallDir(), "settings.json")
}

func defaultConfig() Config {
	return Config{
		InstallDir:             defaultInstallDir(),
		OvmsURL:                defaultOvmsURL,
		UvURL:                  defaultUvURL,
		SearchTags:             defaultSearchTags,
		SearchLimit:            30,
		TextGenTargetDevice:    "GPU",
		EmbeddingsTargetDevice: "GPU",
		APIPort:                3333,
		OVMSRestPort:           8080,
	}
}

func (a *App) loadConfig() {
	data, err := os.ReadFile(configPath())
	if err != nil {
		a.config = defaultConfig()
		a.SaveConfig(a.config) //nolint: errcheck — create settings.json with defaults on first run
		return
	}
	if err := json.Unmarshal(data, &a.config); err != nil {
		a.config = defaultConfig()
		return
	}
	if a.config.OvmsURL == "" {
		a.config.OvmsURL = defaultOvmsURL
	}
	if a.config.UvURL == "" {
		a.config.UvURL = defaultUvURL
	}
	if len(a.config.SearchTags) == 0 {
		a.config.SearchTags = defaultSearchTags
	}
if a.config.SearchLimit == 0 {
		a.config.SearchLimit = 30
	}
	if a.config.TextGenTargetDevice == "" {
		a.config.TextGenTargetDevice = "GPU"
	}
	if a.config.EmbeddingsTargetDevice == "" {
		a.config.EmbeddingsTargetDevice = "GPU"
	}
	if a.config.APIPort == 0 {
		a.config.APIPort = 3333
	}
	if a.config.OVMSRestPort == 0 {
		a.config.OVMSRestPort = 8080
	}
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() Config {
	return a.config
}

// GetPipelineFilters returns the supported pipeline filter types.
func (a *App) GetPipelineFilters() []string {
	filters := make([]string, 0, len(pipelineDefs))
	for k := range pipelineDefs {
		filters = append(filters, k)
	}
	return filters
}

// SaveConfig persists the configuration to disk.
func (a *App) SaveConfig(config Config) error {
	a.config = config
	dir := filepath.Dir(configPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0644)
}

// GetAvailableDevices returns the OpenVINO devices visible to the bundled Python.
func (a *App) GetAvailableDevices() []string {
	ovmsDirPath := filepath.Join(a.config.InstallDir, "ovms")
	pythonExe := filepath.Join(ovmsDirPath, "python", "python.exe")
	if _, err := os.Stat(pythonExe); err != nil {
		return []string{"CPU", "GPU", "NPU", "AUTO"}
	}
	cmd := exec.Command(pythonExe, "-c",
		"from openvino import Core; print(','.join(Core().available_devices))")
	cmd.Env = buildOVMSEnv(ovmsDirPath)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return []string{"CPU", "GPU", "NPU", "AUTO"}
	}
	var devices []string
	for _, d := range strings.Split(strings.TrimSpace(string(out)), ",") {
		if d = strings.TrimSpace(d); d != "" {
			devices = append(devices, d)
		}
	}
	if len(devices) == 0 {
		return []string{"CPU", "GPU", "NPU", "AUTO"}
	}
	return devices
}

// CheckStatus reports whether the export deps and OVMS are present.
func (a *App) CheckStatus() StatusResult {
	marker := filepath.Join(a.config.InstallDir, ".deps-ready")
	ovmsDirPath := filepath.Join(a.config.InstallDir, "ovms")

	_, depsErr := os.Stat(marker)
	_, ovmsErr := os.Stat(ovmsDirPath)

	return StatusResult{
		DepsReady:   depsErr == nil,
		OvmsReady:   ovmsErr == nil,
		OvmsVersion: ovmsVersionFromURL(a.config.OvmsURL),
	}
}

func (a *App) emit(line string) {
	runtime.EventsEmit(a.ctx, "log", line)
}

// HFModel is a minimal representation of a Hugging Face model search result.
type HFModel struct {
	ID          string `json:"id"`
	PipelineTag string `json:"pipeline_tag"`
	Downloads   int    `json:"downloads"`
	Likes       int    `json:"likes"`
	LibraryName string `json:"library_name"`
}

func hfGet(endpoint string) ([]HFModel, error) {
	resp, err := http.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("huggingface request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("huggingface API: HTTP %d", resp.StatusCode)
	}
	var models []HFModel
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return models, nil
}

// SearchModels queries the Hugging Face API with the given pipeline filters.
func (a *App) SearchModels(query string, filters []string) ([]HFModel, error) {
	base := fmt.Sprintf("https://huggingface.co/api/models?limit=%d&sort=downloads&direction=-1", a.config.SearchLimit)
	if query != "" {
		base += "&search=" + url.QueryEscape(query)
	}
	if len(filters) == 0 {
		return hfGet(base)
	}
	seen := map[string]bool{}
	var merged []HFModel
	for _, f := range filters {
		results, err := hfGet(base + "&pipeline_tag=" + url.QueryEscape(f))
		if err != nil {
			return nil, err
		}
		for _, m := range results {
			if !seen[m.ID] {
				seen[m.ID] = true
				merged = append(merged, m)
			}
		}
	}
	return merged, nil
}

// pipelineDef describes how each HuggingFace pipeline_tag maps to OVMS operations.
type pipelineDef struct {
	PullTask    string // --task arg for `ovms --pull`
	ExportTask  string // positional arg for export_model.py
	IsEmbedding bool   // true → use /v3/embeddings endpoint for testing
}

// pipelineDefs is the single source of truth for supported pipeline types.
var pipelineDefs = map[string]pipelineDef{
	"text-generation":     {PullTask: "text_generation", ExportTask: "text_generation", IsEmbedding: false},
	"image-text-to-text":  {PullTask: "text_generation", ExportTask: "text_generation", IsEmbedding: false},
	"feature-extraction":  {PullTask: "embeddings", ExportTask: "embeddings_ov", IsEmbedding: true},
}

// PullModel downloads an OpenVINO model from Hugging Face using OVMS --pull.
// pipelineTag is the HF pipeline_tag (e.g. "text-generation", "feature-extraction").
func (a *App) PullModel(modelID, targetDevice, pipelineTag string) error {
	if a.config.InstallDir == "" {
		return fmt.Errorf("install directory is not configured")
	}
	if modelID == "" {
		return fmt.Errorf("no model selected")
	}

	def, ok := pipelineDefs[pipelineTag]
	if !ok {
		return fmt.Errorf("unsupported pipeline tag %q", pipelineTag)
	}

	ovmsDirPath := filepath.Join(a.config.InstallDir, "ovms")
	ovmsExe := filepath.Join(ovmsDirPath, "ovms.exe")

	if _, err := os.Stat(ovmsExe); err != nil {
		return fmt.Errorf("ovms.exe not found at %s", ovmsExe)
	}

	modelsDir := filepath.Join(a.config.InstallDir, "models")
	os.MkdirAll(modelsDir, 0755) //nolint: errcheck

	args := []string{
		"--pull",
		"--source_model", modelID,
		"--model_repository_path", modelsDir,
		"--model_name", modelID,
		"--task", def.PullTask,
		"--target_device", targetDevice,
	}

	cmd := exec.Command(ovmsExe, args...)
	cmd.Dir = ovmsDirPath
	cmd.Env = buildOVMSEnv(ovmsDirPath)

	if err := a.streamCmd(cmd); err != nil {
		return err
	}
	return a.ovmsAddToConfig(modelID, modelsDir, targetDevice, pipelineTag)
}

// ExportTextGen exports a text-generation model using export_model.py.
func (a *App) ExportTextGen(modelID, targetDevice string, extraOpts map[string]any) error {
	return a.exportWithScript(modelID, targetDevice, "text-generation", extraOpts)
}

// ExportEmbeddings exports an embeddings model using export_model.py.
func (a *App) ExportEmbeddings(modelID, targetDevice string, extraOpts map[string]any) error {
	return a.exportWithScript(modelID, targetDevice, "feature-extraction", extraOpts)
}

func (a *App) exportWithScript(modelID, targetDevice, pipelineTag string, extraOpts map[string]any) error {
	if a.config.InstallDir == "" {
		return fmt.Errorf("install directory is not configured")
	}
	if modelID == "" {
		return fmt.Errorf("no model selected")
	}

	def, ok := pipelineDefs[pipelineTag]
	if !ok {
		return fmt.Errorf("unsupported pipeline tag %q", pipelineTag)
	}

	ovmsDirPath := filepath.Join(a.config.InstallDir, "ovms")
	pythonExe := filepath.Join(ovmsDirPath, "python", "python.exe")
	scriptPath := filepath.Join(a.config.InstallDir, "export-model-requirements", "export_model.py")

	if _, err := os.Stat(pythonExe); err != nil {
		return fmt.Errorf("python not found at %s — run Prepare OVMS first", pythonExe)
	}
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("export_model.py not found at %s", scriptPath)
	}

	modelsDir := filepath.Join(a.config.InstallDir, "models")
	os.MkdirAll(modelsDir, 0755) //nolint: errcheck

	args := []string{
		scriptPath,
		def.ExportTask,
		"--source_model", modelID,
		"--model_repository_path", modelsDir,
		"--model_name", modelID,
		"--target_device", targetDevice,
	}
	for k, v := range extraOpts {
		switch val := v.(type) {
		case bool:
			if val {
				args = append(args, "--"+k)
			}
		case string:
			if val != "" {
				args = append(args, "--"+k, val)
			}
		case float64:
			args = append(args, "--"+k, strconv.FormatFloat(val, 'f', -1, 64))
		}
	}

	a.emit("$ " + pythonExe + " " + strings.Join(args, " "))

	cmd := exec.Command(pythonExe, args...)
	cmd.Dir = ovmsDirPath
	cmd.Env = buildOVMSEnv(ovmsDirPath)

	if err := a.streamCmd(cmd); err != nil {
		return err
	}
	return a.ovmsAddToConfig(modelID, modelsDir, targetDevice, pipelineTag)
}

func (a *App) ovmsAddToConfig(modelID, modelsDir, targetDevice, task string) error {
	cfgPath := filepath.Join(a.config.InstallDir, "config.json")
	var cfg OVMSConfig
	if data, err := os.ReadFile(cfgPath); err == nil {
		json.Unmarshal(data, &cfg) //nolint: errcheck — start fresh on parse error
	}
	a.setModelTask(modelID, task)
	for i, e := range cfg.ModelConfigList {
		if e.Config.Name == modelID {
			if cfg.ModelConfigList[i].Config.TargetDevice != targetDevice {
				cfg.ModelConfigList[i].Config.TargetDevice = targetDevice
				a.emit("Updating target_device for " + modelID + " to " + targetDevice)
				if err := writeOVMSConfig(cfgPath, cfg); err != nil {
					return err
				}
				a.restartOVMS()
			}
			return nil
		}
	}
	cfg.ModelConfigList = append(cfg.ModelConfigList, OVMSConfigEntry{
		Config: OVMSModelConfig{
			Name:         modelID,
			BasePath:     filepath.Join(modelsDir, modelID),
			TargetDevice: targetDevice,
		},
	})
	a.emit("Adding " + modelID + " to config.json")
	if err := writeOVMSConfig(cfgPath, cfg); err != nil {
		return err
	}
	a.restartOVMS()
	return nil
}

// stopAndWait kills the OVMS process tree (if running) and blocks until it exits.
// taskkill /F /T kills the process and all its children so child processes
// (Python workers etc.) release their port bindings before we return.
func (a *App) stopAndWait() {
	a.ovmsMu.Lock()
	if a.ovmsProc == nil {
		a.ovmsMu.Unlock()
		return
	}
	pid := a.ovmsProc.Process.Pid
	a.ovmsMu.Unlock()

	tk := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	hideWindow(tk)
	tk.Run() //nolint: errcheck

	for {
		a.ovmsMu.Lock()
		done := a.ovmsProc == nil
		a.ovmsMu.Unlock()
		if done {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// restartOVMSIfRunning restarts the OVMS server if it is currently running.
func (a *App) restartOVMSIfRunning() {
	a.ovmsMu.Lock()
	running := a.ovmsProc != nil
	a.ovmsMu.Unlock()
	if !running {
		return
	}
	a.stopAndWait()
	a.StartOVMS() //nolint: errcheck
}

// restartOVMS stops OVMS if running then starts it unconditionally.
func (a *App) restartOVMS() {
	a.stopAndWait()
	a.StartOVMS() //nolint: errcheck
}

func (a *App) streamCmd(cmd *exec.Cmd) error {
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.streamLogReader(stdout) }()
	go func() { defer wg.Done(); a.streamLogReader(stderr) }()
	wg.Wait()
	return cmd.Wait()
}

// streamLogReader reads from r in small chunks, splitting on \n and \r so that
// carriage-return progress updates (e.g. huggingface_hub download bars) are
// emitted as individual log lines in real time instead of waiting for \n.
func (a *App) streamLogReader(r io.Reader) {
	buf := make([]byte, 4096)
	var partial string
	emit := func(line string) {
		if line = strings.TrimSpace(line); line != "" {
			runtime.EventsEmit(a.ctx, "log", line)
		}
	}
	for {
		n, err := r.Read(buf)
		if n > 0 {
			partial += string(buf[:n])
			for {
				idx := strings.IndexAny(partial, "\n\r")
				if idx < 0 {
					break
				}
				emit(partial[:idx])
				partial = partial[idx+1:]
				// skip the \n of a \r\n pair
				if len(partial) > 0 && partial[0] == '\n' {
					partial = partial[1:]
				}
			}
		}
		if err != nil {
			emit(partial)
			break
		}
	}
}

func writeOVMSConfig(cfgPath string, cfg OVMSConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config.json: %w", err)
	}
	return os.WriteFile(cfgPath, data, 0644)
}

// ResetModels removes the models folder and the OVMS JSON config files.
func (a *App) ResetModels() error {
	a.stopAndWait()
	modelsDir := filepath.Join(a.config.InstallDir, "models")
	if _, err := os.Stat(modelsDir); err == nil {
		rmCmd := exec.Command("cmd", "/c", "rd", "/s", "/q", modelsDir)
		hideWindow(rmCmd)
		if err := rmCmd.Run(); err != nil {
			return fmt.Errorf("remove models: %w", err)
		}
	}
	for _, name := range []string{"config.json", "model_meta.json"} {
		path := filepath.Join(a.config.InstallDir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}

// ResetOVMS removes the OVMS server directory and the deps-ready marker.
// Uses rd /s /q for fast native Windows deletion.
func (a *App) ResetOVMS() error {
	a.stopAndWait()
	ovmsDirPath := filepath.Join(a.config.InstallDir, "ovms")
	if _, err := os.Stat(ovmsDirPath); err == nil {
		rmCmd := exec.Command("cmd", "/c", "rd", "/s", "/q", ovmsDirPath)
		hideWindow(rmCmd)
		if err := rmCmd.Run(); err != nil {
			return fmt.Errorf("remove ovms: %w", err)
		}
	}
	for _, name := range []string{".deps-ready"} {
		path := filepath.Join(a.config.InstallDir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}

// PrepareOVMS downloads and extracts the OVMS server.
func (a *App) PrepareOVMS() error {
	if a.config.InstallDir == "" {
		return fmt.Errorf("install directory is not configured")
	}
	if a.config.OvmsURL == "" {
		return fmt.Errorf("OVMS download URL is not configured")
	}
	if err := setup.PrepareOVMS(a.config.InstallDir, a.config.OvmsURL, a.emit); err != nil {
		return err
	}
	return setup.PrepareExport(a.config.InstallDir, a.config.UvURL, a.emit)
}

// buildOVMSEnv constructs the process environment for running ovms.exe.
func buildOVMSEnv(ovmsDir string) []string {
	var prepend []string
	pythonDir := filepath.Join(ovmsDir, "python")
	if _, err := os.Stat(pythonDir); err == nil {
		prepend = []string{ovmsDir, pythonDir, filepath.Join(pythonDir, "Scripts")}
	} else {
		prepend = []string{ovmsDir}
	}

	base := os.Environ()
	result := make([]string, 0, len(base)+4)
	for _, e := range base {
		upper := strings.ToUpper(e)
		if strings.HasPrefix(upper, "PATH=") {
			result = append(result, "PATH="+strings.Join(prepend, ";")+";"+e[5:])
		} else if strings.HasPrefix(upper, "PYTHONPATH=") || strings.HasPrefix(upper, "PYTHONHOME=") {
			// strip system python env — we set our own below
		} else {
			result = append(result, e)
		}
	}
	result = append(result, "OVMS_DIR="+ovmsDir)
	if _, err := os.Stat(pythonDir); err == nil {
		sitePackages := filepath.Join(pythonDir, "Lib", "site-packages")
		result = append(result, "PYTHONHOME="+pythonDir)
		result = append(result, "PYTHONPATH="+sitePackages)
	}
	return result
}

func (a *App) emitServerLog(line string) {
	runtime.EventsEmit(a.ctx, "server-log", line)
}

func (a *App) streamReader(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			a.emitServerLog(line)
		}
	}
}

// StartOVMS starts the OVMS server process, streaming its output via "server-log" events.
func (a *App) StartOVMS() error {
	a.ovmsMu.Lock()
	defer a.ovmsMu.Unlock()

	if a.ovmsProc != nil {
		return fmt.Errorf("OVMS server is already running")
	}

	ovmsDirPath := filepath.Join(a.config.InstallDir, "ovms")
	ovmsExe := filepath.Join(ovmsDirPath, "ovms.exe")
	ovmsCfg := filepath.Join(a.config.InstallDir, "config.json")
	if _, err := os.Stat(ovmsExe); err != nil {
		return fmt.Errorf("ovms.exe not found at %s", ovmsExe)
	}

	modelsDir := filepath.Join(a.config.InstallDir, "models")
	os.MkdirAll(modelsDir, 0755) //nolint: errcheck

	args := []string{"--port", "9000", "--rest_port", strconv.Itoa(a.config.OVMSRestPort), "--config_path", ovmsCfg}
	a.emitServerLog("$ " + ovmsExe + " " + strings.Join(args, " "))
	cmd := exec.Command(ovmsExe, args...)
	cmd.Dir = ovmsDirPath
	cmd.Env = buildOVMSEnv(ovmsDirPath)
	hideWindow(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ovms: %w", err)
	}

	a.ovmsProc = cmd
	runtime.EventsEmit(a.ctx, "server-status", true)

	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); a.streamReader(stdout) }()
		go func() { defer wg.Done(); a.streamReader(stderr) }()
		wg.Wait()
		cmd.Wait() //nolint: errcheck

		a.ovmsMu.Lock()
		a.ovmsProc = nil
		a.ovmsMu.Unlock()
		runtime.EventsEmit(a.ctx, "server-status", false)
	}()

	return nil
}

// StopOVMS kills the running OVMS server process.
func (a *App) StopOVMS() error {
	a.ovmsMu.Lock()
	defer a.ovmsMu.Unlock()

	if a.ovmsProc == nil {
		return nil
	}
	if err := a.ovmsProc.Process.Kill(); err != nil {
		return fmt.Errorf("kill ovms: %w", err)
	}
	return nil
}

// IsOVMSRunning reports whether the OVMS server process is active.
func (a *App) IsOVMSRunning() bool {
	a.ovmsMu.Lock()
	defer a.ovmsMu.Unlock()
	return a.ovmsProc != nil
}

// ModelInfo represents an installed model with its configuration.
type ModelInfo struct {
	Name         string `json:"name"`
	BasePath     string `json:"base_path"`
	TargetDevice string `json:"target_device"`
	Task         string `json:"task,omitempty"`
}

// modelMeta holds per-model metadata that is not part of the OVMS config.json.
type modelMeta struct {
	Task string `json:"task,omitempty"`
}

func (a *App) metaPath() string {
	return filepath.Join(a.config.InstallDir, "model_meta.json")
}

func (a *App) readModelMeta() map[string]modelMeta {
	data, err := os.ReadFile(a.metaPath())
	if err != nil {
		return map[string]modelMeta{}
	}
	var m map[string]modelMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]modelMeta{}
	}
	return m
}

func (a *App) writeModelMeta(m map[string]modelMeta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.metaPath(), data, 0644)
}

func (a *App) setModelTask(modelID, task string) {
	m := a.readModelMeta()
	m[modelID] = modelMeta{Task: task}
	a.writeModelMeta(m) //nolint: errcheck
}

func (a *App) deleteModelMeta(modelID string) {
	m := a.readModelMeta()
	delete(m, modelID)
	a.writeModelMeta(m) //nolint: errcheck
}

// OVMSModelConfig is a single model entry in config.json.
type OVMSModelConfig struct {
	Name         string         `json:"name"`
	BasePath     string         `json:"base_path"`
	TargetDevice string         `json:"target_device,omitempty"`
	PluginConfig map[string]any `json:"plugin_config,omitempty"`
	Nireq        int            `json:"nireq,omitempty"`
}

// OVMSConfigEntry wraps OVMSModelConfig in the config list.
type OVMSConfigEntry struct {
	Config OVMSModelConfig `json:"config"`
}

// OVMSConfig matches the structure of config.json used by OVMS.
type OVMSConfig struct {
	ModelConfigList []OVMSConfigEntry `json:"model_config_list"`
}

// GetInstalledModels returns the list of models from config.json.
func (a *App) GetInstalledModels() ([]ModelInfo, error) {
	cfgPath := filepath.Join(a.config.InstallDir, "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []ModelInfo{}, nil
		}
		return nil, fmt.Errorf("read config.json: %w", err)
	}

	var ovmsConfig OVMSConfig
	if err := json.Unmarshal(data, &ovmsConfig); err != nil {
		return nil, fmt.Errorf("parse config.json: %w", err)
	}

	meta := a.readModelMeta()
	models := make([]ModelInfo, 0, len(ovmsConfig.ModelConfigList))
	for _, item := range ovmsConfig.ModelConfigList {
		models = append(models, ModelInfo{
			Name:         item.Config.Name,
			BasePath:     item.Config.BasePath,
			TargetDevice: item.Config.TargetDevice,
			Task:         meta[item.Config.Name].Task,
		})
	}
	return models, nil
}

// DeleteInstalledModel removes a model from config.json and deletes its files.
// If the OVMS server is running it is stopped first and restarted after.
func (a *App) DeleteInstalledModel(modelName string) error {
	if modelName == "" {
		return fmt.Errorf("model name is required")
	}

	a.stopAndWait()

	cfgPath := filepath.Join(a.config.InstallDir, "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read config.json: %w", err)
	}

	var ovmsConfig OVMSConfig
	if err := json.Unmarshal(data, &ovmsConfig); err != nil {
		return fmt.Errorf("parse config.json: %w", err)
	}

	var modelPath string
	newList := make([]OVMSConfigEntry, 0)

	for _, item := range ovmsConfig.ModelConfigList {
		if item.Config.Name == modelName {
			modelPath = item.Config.BasePath
		} else {
			newList = append(newList, item)
		}
	}

	if modelPath == "" {
		return fmt.Errorf("model %q not found in config.json", modelName)
	}

	ovmsConfig.ModelConfigList = newList
	if err := writeOVMSConfig(cfgPath, ovmsConfig); err != nil {
		return err
	}
	a.deleteModelMeta(modelName)

	if !filepath.IsAbs(modelPath) {
		modelPath = filepath.Join(a.config.InstallDir, modelPath)
	}
	var removeErr error
	for range 5 {
		if removeErr = os.RemoveAll(modelPath); removeErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if removeErr != nil {
		a.emit("Warning: could not fully remove model directory: " + removeErr.Error())
	}

	if len(newList) > 0 {
		a.StartOVMS() //nolint: errcheck
	}
	return nil
}

// Chat sends a conversation to the specified text-generation model via OVMS
// and returns the assistant's reply.
func (a *App) Chat(modelName string, messages []map[string]string) (string, error) {
	if modelName == "" {
		return "", fmt.Errorf("no model selected")
	}
	url := fmt.Sprintf("http://localhost:%d/v3/chat/completions", a.config.OVMSRestPort)
	code, raw, err := ovmsPost(url, map[string]any{
		"model":      modelName,
		"max_tokens": 512,
		"messages":   messages,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parse response (HTTP %d): %w", code, err)
	}
	if result.Error.Message != "" {
		return "", fmt.Errorf("%s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from model")
	}
	return result.Choices[0].Message.Content, nil
}
