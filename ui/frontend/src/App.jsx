import { useState, useEffect, useRef } from 'react'
import { GetConfig, SaveConfig, PrepareOVMS, ResetOVMS, CheckStatus, GetStartupEnabled, SetStartup, SearchModels, ExportTextGen, ExportEmbeddings, PullModel, StartOVMS, StopOVMS, IsOVMSRunning, GetInstalledModels, DeleteInstalledModel, GetAvailableDevices } from '../wailsjs/go/main/App'
import { EventsOn, BrowserOpenURL } from '../wailsjs/runtime/runtime'

const PROGRESS_MAP = {
  'Downloading OVMS': 15,
  'Extracting OVMS': 25,
  'OVMS ready': 30,
  'Downloading export bundle': 60,
  'Installing export bundle': 85,
  'Setup complete': 100,
}

function StatusBadge({ ready, label }) {
  return (
    <div className={`status-badge ${ready ? 'ready' : 'missing'}`}>
      <span className="status-dot" />
      {label}
    </div>
  )
}

export default function App() {
  const [tab, setTab] = useState('server')
  const [config, setConfig] = useState({
    install_dir: '',
    ovms_url: '',
    uv_url: '',
    api_port: 3333,
    search_tags: [],
    pipeline_filters: [],
    search_limit: 30,
    text_gen_target_device: 'GPU',
    embeddings_target_device: 'CPU',
  })
  const [newTag, setNewTag] = useState('')
  const [newFilter, setNewFilter] = useState('')
  const [saved, setSaved] = useState(false)
  const [startup, setStartup] = useState(false)
  const [status, setStatus] = useState(null)
  const [logs, setLogs] = useState([])
  const [running, setRunning] = useState(false)
  const [error, setError] = useState(null)

  const [initStep, setInitStep] = useState('Checking setup…')
  const [initError, setInitError] = useState(null)
  const [progress, setProgress] = useState(0)

  const [serverRunning, setServerRunning] = useState(false)
  const [serverLogs, setServerLogs] = useState([])

  const [targetDevice, setTargetDevice] = useState('GPU')
  const [availableDevices, setAvailableDevices] = useState(['CPU', 'GPU', 'NPU', 'AUTO'])
  const [extraOptsText, setExtraOptsText] = useState('{\n  "weight-format": "int8"\n}')
  const [extraOptsError, setExtraOptsError] = useState(false)

  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState([])
  const [searching, setSearching] = useState(false)
  const [selectedModel, setSelectedModel] = useState('')
  const [activeFilters, setActiveFilters] = useState(null)
  const [installedModels, setInstalledModels] = useState([])
  const [deleteConfirm, setDeleteConfirm] = useState(null)

  const logsEndRef = useRef(null)
  const initLogsEndRef = useRef(null)
  const serverLogsEndRef = useRef(null)
  const startupRan = useRef(false)

  useEffect(() => {
    const offLog = EventsOn('log', line => {
      setLogs(prev => [...prev, line])
      setInitStep(line)
      const match = Object.entries(PROGRESS_MAP).find(([k]) => line.startsWith(k))
      if (match) setProgress(match[1])
    })
    return () => { if (offLog) offLog() }
  }, [])

  useEffect(() => {
    const offServerLog = EventsOn('server-log', line => {
      setServerLogs(prev => [...prev, line])
    })
    const offServerStatus = EventsOn('server-status', running => {
      setServerRunning(running)
    })
    IsOVMSRunning().then(setServerRunning)
    return () => {
      if (offServerLog) offServerLog()
      if (offServerStatus) offServerStatus()
    }
  }, [])

  useEffect(() => {
    if (startupRan.current) return
    startupRan.current = true

    Promise.all([GetConfig(), GetStartupEnabled()]).then(([cfg, su]) => {
      setConfig(cfg)
      setActiveFilters(cfg.pipeline_filters || [])
      setStartup(su)
    })

    const refreshDevices = () =>
      GetAvailableDevices().then(devices => {
        if (devices && devices.length > 0) setAvailableDevices(devices)
      })

    const autoStart = async () => {
      for (let i = 0; i < 3; i++) {
        try { await StartOVMS(); return } catch {}
        await new Promise(r => setTimeout(r, 1000))
      }
    }

    CheckStatus().then(async s => {
      setStatus(s)
      if (s.deps_ready && s.ovms_ready) {
        refreshDevices()
        autoStart()
        return
      }
      setRunning(true)
      try {
        setInitStep('Setting up OVMS…')
        await PrepareOVMS()
        const s2 = await CheckStatus()
        setStatus(s2)
        setLogs([])
        refreshDevices()
        autoStart()
      } catch (err) {
        setInitError(String(err))
      } finally {
        setRunning(false)
      }
    })
  }, [])

  useEffect(() => {
    if (tab === 'models' && status?.deps_ready && status?.ovms_ready) {
      loadInstalledModels()
    }
  }, [tab, status])

  // Set default target device and extra opts based on selected model pipeline
  useEffect(() => {
    if (!selectedModel) return
    const info = searchResults.find(m => m.id === selectedModel)
    const tag = info?.pipeline_tag
    const clamp = (preferred) => {
      if (availableDevices.length === 0) return preferred
      return availableDevices.includes(preferred) ? preferred : availableDevices[0]
    }
    if (tag === 'text-generation') setTargetDevice(clamp(config.text_gen_target_device || 'GPU'))
    else if (tag === 'feature-extraction') setTargetDevice(clamp(config.embeddings_target_device || 'GPU'))
    setExtraOptsText('{\n  "weight-format": "int8"\n}')
    setExtraOptsError(false)
  }, [selectedModel, searchResults, availableDevices])

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
    initLogsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  useEffect(() => {
    serverLogsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [serverLogs])

  const handleSave = async () => {
    await SaveConfig(config)
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
  }

  const run = (action) => {
    setLogs([])
    setError(null)
    setProgress(0)
    setRunning(true)
    action()
      .then(() => {
        setLogs(prev => [...prev, '--- Done ---'])
        CheckStatus().then(setStatus)
        loadInstalledModels()
      })
      .catch(err => setError(String(err)))
      .finally(() => setRunning(false))
  }

  const runSetup = async () => {
    setInitError(null)
    setProgress(0)
    setInitStep('Setting up OVMS…')
    setRunning(true)
    try {
      await PrepareOVMS()
      const s2 = await CheckStatus()
      setStatus(s2)
      setLogs([])
    } catch (err) {
      setInitError(String(err))
    } finally {
      setRunning(false)
    }
  }

  const handleReset = async () => {
    if (!window.confirm('This will delete the OVMS installation and re-download it. Continue?')) return
    setStatus(null)
    setRunning(true)
    try {
      await ResetOVMS()
    } catch (err) {
      setInitError(String(err))
      setRunning(false)
      return
    }
    await runSetup()
  }

  const loadInstalledModels = () => {
    GetInstalledModels()
      .then(models => setInstalledModels(models || []))
      .catch(() => setInstalledModels([]))
  }

  const handleDeleteModel = (modelName) => {
    setDeleteConfirm(modelName)
  }

  const confirmDelete = () => {
    const modelName = deleteConfirm
    setDeleteConfirm(null)
    setRunning(true)
    setLogs([`Deleting model ${modelName}...`])
    DeleteInstalledModel(modelName)
      .then(() => {
        setLogs(prev => [...prev, `Model ${modelName} deleted successfully`, '--- Done ---'])
        loadInstalledModels()
      })
      .catch(err => setError(String(err)))
      .finally(() => setRunning(false))
  }

  const doSearch = (query) => {
    setSearching(true)
    setSearchResults([])
    setSelectedModel('')
    SearchModels(query, activeFilters || [])
      .then(results => {
        const list = results || []
        setSearchResults(list)
        if (list.length > 0) setSelectedModel(list[0].id)
      })
      .catch(err => setError(String(err)))
      .finally(() => setSearching(false))
  }

  const quickSearch = (tag) => { setSearchQuery(tag); doSearch(tag) }
  const handleSearch = () => { doSearch(searchQuery.trim()) }

  const toggleFilter = (f) =>
    setActiveFilters(prev => prev.includes(f) ? prev.filter(x => x !== f) : [...prev, f])

  const selectedModelInfo = searchResults.find(m => m.id === selectedModel)
  const isSelectedOV = selectedModelInfo?.library_name === 'openvino' ||
    selectedModel.toLowerCase().startsWith('openvino/')

  const allReady = status?.deps_ready && status?.ovms_ready

  if (!status || !allReady) {
    return (
      <div className="loading-screen">
        <div className="loading-content">
          <div className="loading-title">OpenVINO Desktop</div>
          <div className="loading-step">{initStep}</div>
          <div className="progress-bar">
            <div className="progress-fill" style={{ width: `${progress}%` }} />
          </div>
          <div className="progress-label">{progress > 0 ? `${progress}%` : ''}</div>
          {initError && (
            <>
              <div className="error">{initError}</div>
              <button className="btn-primary" style={{ marginTop: 16 }} onClick={runSetup}>
                Retry
              </button>
            </>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="app">
      <header className="app-header">
        <span className="app-title">OpenVINO Desktop</span>
        <nav className="tabs">
          {['server', 'models', 'settings'].map(t => {
            const label = t === 'server' ? 'Models Server' : t.charAt(0).toUpperCase() + t.slice(1)
            return (
              <button
                key={t}
                className={`tab ${tab === t ? 'active' : ''}`}
                onClick={() => setTab(t)}
              >
                {label}
              </button>
            )
          })}
        </nav>
        <div className="status-row header-status">
          <StatusBadge ready={status.ovms_ready && status.deps_ready} label={status.ovms_version ? `OVMS ${status.ovms_version}` : 'OVMS'} />
        </div>
      </header>

      <main className="tab-content">
        {tab === 'server' && (
          <div className="panel">
            <div className="devices-info">
              <small>Available OpenVINO devices: <strong>{availableDevices.join(', ')}</strong></small>
            </div>
            <div className="action-card">
                    <div className="action-card-body">
                      <h3>OVMS Server</h3>
                      <p>Start the OpenVINO Model Server on port 9000 (REST 8080).</p>
                    </div>
                    <div className="server-controls">
                      {!serverRunning
                        ? (
                          <button className="btn-primary" onClick={() => {
                            setServerLogs([])
                            StartOVMS().catch(err => setServerLogs(prev => [...prev, '--- Error: ' + String(err) + ' ---']))
                          }}>
                            Start Server
                          </button>
                        )
                        : (
                          <button className="btn-reset" onClick={() => StopOVMS().catch(() => {})}>
                            Stop Server
                          </button>
                        )
                      }
                    </div>
                  </div>

                  {serverLogs.length > 0 && (
                    <div className="log-section">
                      <div className="log-box">
                        {serverLogs.map((line, i) => (
                          <div key={i} className={line.startsWith('---') ? 'log-done' : 'log-line'}>{line}</div>
                        ))}
                        <div ref={serverLogsEndRef} />
                      </div>
                    </div>
                  )}
          </div>
        )}

        {tab === 'models' && (
          <div className="panel">
            <div className="devices-info">
              <small>Available OpenVINO devices: <strong>{availableDevices.join(', ')}</strong></small>
            </div>
            <>
              {installedModels.length > 0 && (
                    <div className="installed-models-section">
                      <h3>Available Models</h3>
                      <div className="installed-models-list">
                        {installedModels.map(model => (
                          <div key={model.name} className="installed-model-card">
                            <div className="installed-model-info">
                              <div className="installed-model-name">{model.name}</div>
                              <div className="installed-model-device">
                                <span className="device-label">Target Device:</span>
                                <span className="device-value">{model.target_device}</span>
                              </div>
                            </div>
                            <button
                              className="btn-delete-model"
                              disabled={running}
                              onClick={() => handleDeleteModel(model.name)}
                              title="Delete model"
                            >
                              🗑️
                            </button>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  <div className="search-section">
                    <h3>Hugging face models</h3>
                    <div className="search-tags">
                      {(config.search_tags || []).map(tag => (
                        <button key={tag} className="search-tag" onClick={() => quickSearch(tag)}>{tag}</button>
                      ))}
                    </div>
                    {(config.pipeline_filters || []).length > 0 && (
                      <div className="filter-chips">
                        {(config.pipeline_filters || []).map(f => {
                          const active = (activeFilters || []).includes(f)
                          return (
                            <button
                              key={f}
                              className={`filter-chip ${active ? 'active' : ''}`}
                              onClick={() => toggleFilter(f)}
                            >
                              {f}
                            </button>
                          )
                        })}
                      </div>
                    )}
                    <div className="search-row">
                      <input
                        className="search-input"
                        value={searchQuery}
                        onChange={e => setSearchQuery(e.target.value)}
                        onKeyDown={e => e.key === 'Enter' && handleSearch()}
                        placeholder="Search Hugging Face models…"
                      />
                      <button className="btn-primary" disabled={searching} onClick={handleSearch}>
                        {searching ? 'Searching…' : 'Search'}
                      </button>
                    </div>

                    {searchResults.length > 0 && (
                      <div className="search-results">
                        <select
                          size={Math.min(Math.max(searchResults.length, 2), 8)}
                          value={selectedModel}
                          onChange={e => setSelectedModel(e.target.value)}
                        >
                          {searchResults.map(m => (
                            <option key={m.id} value={m.id}>
                              {m.id}{m.pipeline_tag ? ` · ${m.pipeline_tag}` : ''} · ↓{m.downloads.toLocaleString()}
                            </option>
                          ))}
                        </select>

                        {selectedModel && (
                          <div className="export-opts">
                            <label>Target Device
                              <select value={targetDevice} onChange={e => setTargetDevice(e.target.value)}>
                                {availableDevices.map(d => <option key={d}>{d}</option>)}
                              </select>
                            </label>
                            {!isSelectedOV && (
                              <label style={{marginTop: 10}}>Extra Options
                                <textarea
                                  className={`opts-raw-editor${extraOptsError ? ' opts-editor-error' : ''}`}
                                  style={{minHeight: 80, maxHeight: 180}}
                                  value={extraOptsText}
                                  spellCheck={false}
                                  onChange={e => {
                                    setExtraOptsText(e.target.value)
                                    try { JSON.parse(e.target.value); setExtraOptsError(false) }
                                    catch { setExtraOptsError(true) }
                                  }}
                                />
                                {extraOptsError && <div className="opts-editor-error-msg">Invalid JSON</div>}
                              </label>
                            )}
                          </div>
                        )}

                        <div className="search-actions">
                          <button
                            className="btn-primary"
                            disabled={running || !selectedModel || extraOptsError}
                            onClick={() => {
                              if (isSelectedOV) {
                                run(() => PullModel(selectedModel, targetDevice, selectedModelInfo?.pipeline_tag ?? ''))
                              } else {
                                const extraOpts = (() => { try { return JSON.parse(extraOptsText) } catch { return {} } })()
                                const tag = selectedModelInfo?.pipeline_tag
                                if (tag === 'text-generation') run(() => ExportTextGen(selectedModel, targetDevice, extraOpts))
                                else if (tag === 'feature-extraction') run(() => ExportEmbeddings(selectedModel, targetDevice, extraOpts))
                              }
                            }}
                          >
                            {running ? 'Running…' : isSelectedOV ? 'Pull' : 'Export'}
                          </button>
                          {selectedModel && (
                            <button
                              className="btn-hf-link"
                              onClick={() => BrowserOpenURL(`https://huggingface.co/${selectedModel}`)}
                            >
                              🤗 View on Hugging Face ↗
                            </button>
                          )}
                        </div>
                      </div>
                    )}
                  </div>

                  <div className="log-section">
                    {error && <div className="error">{error}</div>}
                    <div className="log-box">
                      {logs.length === 0 && !error
                        ? <div className="log-line log-empty">Export output will appear here…</div>
                        : logs.map((line, i) => (
                          <div key={i} className={line.startsWith('---') ? 'log-done' : 'log-line'}>{line}</div>
                        ))
                      }
                      <div ref={logsEndRef} />
                    </div>
                  </div>
            </>
          </div>
        )}

        {tab === 'settings' && (
          <div className="panel">
            <div className="fields">
              <div className="field">
                <label>Setup Folder</label>
                <input
                  value={config.install_dir}
                  onChange={e => setConfig(c => ({ ...c, install_dir: e.target.value }))}
                  placeholder="e.g. C:\Users\user\openvino-desktop"
                />
                <small>Base directory where OVMS will be installed.</small>
              </div>

              <div className="field">
                <label>OVMS Download URL</label>
                <input
                  value={config.ovms_url}
                  onChange={e => setConfig(c => ({ ...c, ovms_url: e.target.value }))}
                  placeholder="https://github.com/openvinotoolkit/model_server/releases/download/…/ovms_windows_python_on.zip"
                />
                <small>URL to the OVMS zip archive for Windows.</small>
              </div>

              <div className="field">
                <label>uv Download URL</label>
                <input
                  value={config.uv_url}
                  onChange={e => setConfig(c => ({ ...c, uv_url: e.target.value }))}
                  placeholder="https://github.com/turintech/openvino-desktop/releases/download/uv/uv.exe"
                />
                <small>URL to download uv.exe used for setting up the export environment.</small>
              </div>

              <div className="field">
                <label>REST API Port</label>
                <input
                  type="number"
                  min="1024"
                  max="65535"
                  value={config.api_port}
                  onChange={e => setConfig(c => ({ ...c, api_port: parseInt(e.target.value) || 3333 }))}
                />
                <small>Port for the local REST API (default: 3333). Requires restart to take effect.</small>
              </div>

            </div>

            <div className="field">
              <label>Search Limit</label>
              <input
                type="number"
                min="1"
                max="200"
                value={config.search_limit || 30}
                onChange={e => setConfig(c => ({ ...c, search_limit: parseInt(e.target.value) || 30 }))}
              />
              <small>Max number of models returned per search (default 30).</small>
            </div>

            <div className="field">
              <label>Search Tags</label>
              <div className="tag-editor">
                {(config.search_tags || []).map(tag => (
                  <span key={tag} className="tag-pill">
                    {tag}
                    <button onClick={() => setConfig(c => ({ ...c, search_tags: c.search_tags.filter(t => t !== tag) }))}>×</button>
                  </span>
                ))}
                <input
                  className="tag-input"
                  value={newTag}
                  onChange={e => setNewTag(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter' && newTag.trim()) {
                      setConfig(c => ({ ...c, search_tags: [...(c.search_tags || []), newTag.trim()] }))
                      setNewTag('')
                    }
                  }}
                  placeholder="Add tag…"
                />
              </div>
              <small>Clickable shortcuts on the Export search. Press Enter to add.</small>
            </div>

            <div className="field">
              <label>Pipeline Filters</label>
              <div className="tag-editor">
                {(config.pipeline_filters || []).map(f => (
                  <span key={f} className="tag-pill">
                    {f}
                    <button onClick={() => setConfig(c => ({ ...c, pipeline_filters: c.pipeline_filters.filter(x => x !== f) }))}>×</button>
                  </span>
                ))}
                <input
                  className="tag-input"
                  value={newFilter}
                  onChange={e => setNewFilter(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter' && newFilter.trim()) {
                      setConfig(c => ({ ...c, pipeline_filters: [...(c.pipeline_filters || []), newFilter.trim()] }))
                      setNewFilter('')
                    }
                  }}
                  placeholder="Add filter…"
                />
              </div>
              <small>Restrict searches to these Hugging Face pipeline types. Press Enter to add.</small>
            </div>

            <label className="toggle-row">
              <span className="toggle-label">
                Run on startup
                <small>Launch automatically when Windows starts.</small>
              </span>
              <input
                type="checkbox"
                checked={startup}
                onChange={e => {
                  const next = e.target.checked
                  SetStartup(next).then(() => setStartup(next))
                }}
              />
            </label>

            <div className="reset-row">
              <button className="btn-reset" disabled={running} onClick={handleReset}>
                Reset OVMS
              </button>
            </div>

            <button className="btn-save" onClick={handleSave}>
              {saved ? 'Saved!' : 'Save Settings'}
            </button>
          </div>
        )}
      </main>

      {deleteConfirm && (
        <div className="modal-overlay" onClick={() => setDeleteConfirm(null)}>
          <div className="modal-content modal-confirm" onClick={e => e.stopPropagation()}>
            <h3>Delete Model</h3>
            <div className="modal-body">
              <p className="modal-confirm-text">
                Are you sure you want to delete <strong>{deleteConfirm}</strong>?
              </p>
              <p className="modal-confirm-warning">
                This will remove the model from config.json and delete its files.
              </p>
            </div>
            <div className="modal-actions">
              <button className="btn-ghost" onClick={() => setDeleteConfirm(null)}>Cancel</button>
              <button className="btn-delete" onClick={confirmDelete}>Delete</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
