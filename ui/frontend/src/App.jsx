import { useState, useEffect, useRef } from 'react'
import { GetConfig, SaveConfig, PrepareExport, PrepareOVMS, CheckStatus, GetStartupEnabled, SetStartup, SearchModels, ExportModel, PullModel } from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'

function StatusBadge({ ready, label }) {
  return (
    <div className={`status-badge ${ready ? 'ready' : 'missing'}`}>
      <span className="status-dot" />
      {label}
    </div>
  )
}

export default function App() {
  const [tab, setTab] = useState('export')
  const [config, setConfig] = useState({ install_dir: '', uv_url: '', ovms_url: '', search_tags: [], pipeline_filters: [] })
  const [newTag, setNewTag] = useState('')
  const [newFilter, setNewFilter] = useState('')
  const [saved, setSaved] = useState(false)
  const [startup, setStartup] = useState(false)
  const [status, setStatus] = useState({ uv_ready: false, deps_ready: false, ovms_ready: false })
  const [logs, setLogs] = useState([])
  const [running, setRunning] = useState(false)
  const [error, setError] = useState(null)

  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState([])
  const [searching, setSearching] = useState(false)
  const [selectedModel, setSelectedModel] = useState('')
  const [activeFilters, setActiveFilters] = useState(null) // null = not yet initialised

  const logsEndRef = useRef(null)

  useEffect(() => {
    GetConfig().then(cfg => { setConfig(cfg); setActiveFilters(cfg.pipeline_filters || []) })
    CheckStatus().then(setStatus)
    GetStartupEnabled().then(setStartup)
    EventsOn('log', (line) => setLogs(prev => [...prev, line]))
  }, [])

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  const handleSave = async () => {
    await SaveConfig(config)
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
  }

  const run = (action) => {
    setLogs([])
    setError(null)
    setRunning(true)
    action()
      .then(() => {
        setLogs(prev => [...prev, '--- Done ---'])
        CheckStatus().then(setStatus)
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
        if (list.length === 1) setSelectedModel(list[0].id)
      })
      .catch(err => setError(String(err)))
      .finally(() => setSearching(false))
  }

  const quickSearch = (tag) => { setSearchQuery(tag); doSearch(tag) }
  const handleSearch = () => { if (searchQuery.trim()) doSearch(searchQuery.trim()) }

  const toggleFilter = (f) =>
    setActiveFilters(prev => prev.includes(f) ? prev.filter(x => x !== f) : [...prev, f])

  const selectedModelInfo = searchResults.find(m => m.id === selectedModel)
  const isSelectedOV = selectedModelInfo?.library_name === 'openvino' ||
    selectedModel.toLowerCase().startsWith('openvino/')

  return (
    <div className="app">
      <header className="app-header">
        <span className="app-title">OpenVINO Desktop</span>
        <nav className="tabs">
          {['models', 'export', 'settings'].map(t => (
            <button
              key={t}
              className={`tab ${tab === t ? 'active' : ''}`}
              onClick={() => setTab(t)}
            >
              {t.charAt(0).toUpperCase() + t.slice(1)}
            </button>
          ))}
        </nav>
      </header>

      <main className="tab-content">
        {tab === 'models' && (
          <div className="panel">
            <p className="empty-state">No models configured yet.</p>
          </div>
        )}

        {tab === 'export' && (
          <div className="panel">
            <div className="status-row">
              <StatusBadge ready={status.uv_ready} label="uv" />
              <StatusBadge ready={status.deps_ready} label="Dependencies" />
              <StatusBadge ready={status.ovms_ready} label="OVMS" />
              <button className="btn-ghost" onClick={() => CheckStatus().then(setStatus)}>
                Refresh
              </button>
            </div>

            <div className="action-grid">
              <div className="action-card">
                <div className="action-card-body">
                  <h3>Export Environment</h3>
                  <p>Downloads uv, installs Python 3.12, creates a virtual environment and installs ML requirements.</p>
                </div>
                <button className="btn-primary" disabled={running} onClick={() => run(PrepareExport)}>
                  {running ? 'Running…' : 'Prepare Export'}
                </button>
              </div>

              <div className="action-card">
                <div className="action-card-body">
                  <h3>OVMS Server</h3>
                  <p>Downloads and extracts the OpenVINO Model Server for Windows.</p>
                </div>
                <button className="btn-primary" disabled={running} onClick={() => run(PrepareOVMS)}>
                  {running ? 'Running…' : 'Prepare OVMS'}
                </button>
              </div>
            </div>

            <div className="search-section">
              <h3>Export Model from Hugging Face</h3>
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
                <button className="btn-primary" disabled={searching || !searchQuery.trim()} onClick={handleSearch}>
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
                  <button
                    className="btn-primary"
                    disabled={running || !selectedModel}
                    onClick={() => run(() => isSelectedOV ? PullModel(selectedModel) : ExportModel(selectedModel))}
                  >
                    {running ? 'Running…' : isSelectedOV ? `Pull ${selectedModel || '…'}` : `Export ${selectedModel || '…'}`}
                  </button>
                </div>
              )}
            </div>

            {(logs.length > 0 || error) && (
              <div className="log-section">
                {error && <div className="error">{error}</div>}
                <div className="log-box">
                  {logs.map((line, i) => (
                    <div key={i} className={line.startsWith('---') ? 'log-done' : 'log-line'}>
                      {line}
                    </div>
                  ))}
                  <div ref={logsEndRef} />
                </div>
              </div>
            )}
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
                  placeholder="e.g. C:\Users\user\openvino-desk"
                />
                <small>Base directory where Python, venv and OVMS will be installed.</small>
              </div>

              <div className="field">
                <label>uv Download URL</label>
                <input
                  value={config.uv_url}
                  onChange={e => setConfig(c => ({ ...c, uv_url: e.target.value }))}
                  placeholder="https://github.com/astral-sh/uv/releases/download/…/uv-x86_64-pc-windows-msvc.zip"
                />
                <small>URL to the uv zip archive for Windows (x86_64).</small>
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

            <button className="btn-save" onClick={handleSave}>
              {saved ? 'Saved!' : 'Save Settings'}
            </button>
          </div>
        )}
      </main>
    </div>
  )
}
