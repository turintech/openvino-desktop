package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/vaggeliskls/openvino-desk/ui/internal/setup"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed assets
var setupAssets embed.FS

const (
	defaultUvURL   = "https://github.com/astral-sh/uv/releases/download/0.10.8/uv-x86_64-pc-windows-msvc.zip"
	defaultOvmsURL = "https://github.com/openvinotoolkit/model_server/releases/download/v2026.0/ovms_windows_python_on.zip"
)

// Config holds user-configurable settings.
var defaultSearchTags = []string{"OpenVINO", "Qwen", "Embedding"}
var defaultPipelineFilters = []string{"text-generation", "feature-extraction"}

type Config struct {
	InstallDir      string   `json:"install_dir"`
	UvURL           string   `json:"uv_url"`
	OvmsURL         string   `json:"ovms_url"`
	StartupSet      bool     `json:"startup_set"` // true once the startup preference has been written
	SearchTags      []string `json:"search_tags"`
	PipelineFilters []string `json:"pipeline_filters"`
	SearchLimit     int      `json:"search_limit"`
}

// StatusResult reports whether each component is ready.
type StatusResult struct {
	UvReady   bool `json:"uv_ready"`
	DepsReady bool `json:"deps_ready"`
	OvmsReady bool `json:"ovms_ready"`
}

// App is the Wails application struct.
type App struct {
	ctx    context.Context
	config Config
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadConfig()
	// On first run, register the app to start with Windows by default.
	if !a.config.StartupSet {
		a.SetStartup(true) //nolint: errcheck — best-effort on first run
		a.config.StartupSet = true
		a.SaveConfig(a.config) //nolint: errcheck
	}
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".openvino-desk", "config.json")
}

func defaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		InstallDir:      filepath.Join(home, "openvino-desk"),
		UvURL:           defaultUvURL,
		OvmsURL:         defaultOvmsURL,
		SearchTags:      defaultSearchTags,
		PipelineFilters: defaultPipelineFilters,
		SearchLimit:     30,
	}
}

func (a *App) loadConfig() {
	data, err := os.ReadFile(configPath())
	if err != nil {
		a.config = defaultConfig()
		return
	}
	if err := json.Unmarshal(data, &a.config); err != nil {
		a.config = defaultConfig()
		return
	}
	// Fill in URL defaults for older configs that predate these fields.
	if a.config.UvURL == "" {
		a.config.UvURL = defaultUvURL
	}
	if a.config.OvmsURL == "" {
		a.config.OvmsURL = defaultOvmsURL
	}
	if len(a.config.SearchTags) == 0 {
		a.config.SearchTags = defaultSearchTags
	}
	if len(a.config.PipelineFilters) == 0 {
		a.config.PipelineFilters = defaultPipelineFilters
	}
	if a.config.SearchLimit == 0 {
		a.config.SearchLimit = 30
	}
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() Config {
	return a.config
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

// CheckStatus reports whether uv, the export venv, and OVMS are present.
func (a *App) CheckStatus() StatusResult {
	uvBin := filepath.Join(a.config.InstallDir, "uv.exe")
	venvPython := filepath.Join(a.config.InstallDir, "export", "Scripts", "python.exe")
	ovmsDir := filepath.Join(a.config.InstallDir, "ovms")

	_, uvErr := os.Stat(uvBin)
	_, depsErr := os.Stat(venvPython)
	_, ovmsErr := os.Stat(ovmsDir)

	return StatusResult{
		UvReady:   uvErr == nil,
		DepsReady: depsErr == nil,
		OvmsReady: ovmsErr == nil,
	}
}

// extractAssets writes embedded assets (requirements, scripts) to installDir,
// skipping uv.exe which is downloaded from the configured URL instead.
func (a *App) extractAssets() error {
	return fs.WalkDir(setupAssets, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Base(path) == "uv.exe" {
			return nil // uv is downloaded from URL, not embedded
		}
		rel, _ := filepath.Rel("assets", path)
		dest := filepath.Join(a.config.InstallDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		data, err := setupAssets.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0755)
	})
}

func (a *App) emit(line string) {
	runtime.EventsEmit(a.ctx, "log", line)
}

// PrepareExport extracts bundled assets, downloads uv, then sets up the Python environment.
func (a *App) PrepareExport() error {
	if a.config.InstallDir == "" {
		return fmt.Errorf("install directory is not configured")
	}
	if a.config.UvURL == "" {
		return fmt.Errorf("uv download URL is not configured")
	}
	a.emit("Extracting bundled assets...")
	if err := a.extractAssets(); err != nil {
		return fmt.Errorf("extract assets: %w", err)
	}
	return setup.PrepareExport(a.config.InstallDir, a.config.UvURL, a.emit)
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
// Pass an empty slice to search without any pipeline restriction.
// One request is made per filter and results are merged/deduplicated.
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

// PullModel downloads an OpenVINO model from Hugging Face using huggingface_hub.
func (a *App) PullModel(modelID string) error {
	if a.config.InstallDir == "" {
		return fmt.Errorf("install directory is not configured")
	}
	if modelID == "" {
		return fmt.Errorf("no model selected")
	}
	venvPython := filepath.Join(a.config.InstallDir, "export", "Scripts", "python.exe")
	destDir := filepath.Join(a.config.InstallDir, "models", modelID)
	script := fmt.Sprintf(
		"from huggingface_hub import snapshot_download; snapshot_download(%q, local_dir=%q)",
		modelID, destDir,
	)
	return setup.RunScript(a.config.InstallDir, a.emit, venvPython, "-c", script)
}

// ExportModel runs export_model.py with the given Hugging Face model ID.
func (a *App) ExportModel(modelID string) error {
	if a.config.InstallDir == "" {
		return fmt.Errorf("install directory is not configured")
	}
	if modelID == "" {
		return fmt.Errorf("no model selected")
	}
	venvPython := filepath.Join(a.config.InstallDir, "export", "Scripts", "python.exe")
	scriptPath := filepath.Join(a.config.InstallDir, "export-model-requirements", "export_model.py")
	return setup.RunScript(a.config.InstallDir, a.emit, venvPython, scriptPath, "--model_id", modelID)
}

// ResetExport removes the uv binary, Python installation and export venv.
func (a *App) ResetExport() error {
	dirs := []string{
		filepath.Join(a.config.InstallDir, "uv.exe"),
		filepath.Join(a.config.InstallDir, "python"),
		filepath.Join(a.config.InstallDir, "export"),
	}
	for _, d := range dirs {
		if err := os.RemoveAll(d); err != nil {
			return fmt.Errorf("remove %s: %w", d, err)
		}
	}
	return nil
}

// ResetOVMS removes the OVMS server directory.
func (a *App) ResetOVMS() error {
	ovmsDir := filepath.Join(a.config.InstallDir, "ovms")
	if err := os.RemoveAll(ovmsDir); err != nil {
		return fmt.Errorf("remove ovms: %w", err)
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
	return setup.PrepareOVMS(a.config.InstallDir, a.config.OvmsURL, a.emit)
}
