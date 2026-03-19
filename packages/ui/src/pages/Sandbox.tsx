import { useState } from 'react'
import { Play, Loader2, AlertCircle, Terminal, FlaskConical, ChevronDown, ChevronRight } from 'lucide-react'
import { SANDBOX_TOOLS } from '../data/tools'
import { runSandboxTool } from '../lib/api'
import CodeBlock from '../components/CodeBlock'
import type { ToolDef } from '../data/tools'

type RunState = 'idle' | 'loading' | 'done' | 'error'

export default function Sandbox() {
  const [selected, setSelected] = useState<ToolDef>(SANDBOX_TOOLS[0])
  const [args, setArgs] = useState<Record<string, string>>({})
  const [output, setOutput] = useState('')
  const [error, setError] = useState('')
  const [duration, setDuration] = useState(0)
  const [state, setState] = useState<RunState>('idle')

  const run = async () => {
    setState('loading')
    setError('')
    setOutput('')
    try {
      const parsedArgs: Record<string, unknown> = {}
      for (const [k, v] of Object.entries(args)) {
        if (v !== '') parsedArgs[k] = v
      }
      const res = await runSandboxTool({ tool: selected.name, args: parsedArgs })
      setOutput(res.output)
      setDuration(res.duration_ms)
      setState('done')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
      setState('error')
    }
  }

  const selectTool = (t: ToolDef) => {
    setSelected(t)
    setArgs({})
    setOutput('')
    setError('')
    setState('idle')
  }

  const requestPreview = JSON.stringify({
    jsonrpc: '2.0',
    method: 'tools/call',
    params: {
      name: selected.name,
      arguments: Object.fromEntries(Object.entries(args).filter(([, v]) => v !== '')),
    },
  }, null, 2)

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 py-8 animate-fade-in">
      {/* Header */}
      <div className="mb-6">
        <div className="flex items-center gap-2 mb-1">
          <FlaskConical className="w-5 h-5 text-accent-green" />
          <h1 className="text-2xl font-bold text-text-primary">Live Sandbox</h1>
        </div>
        <p className="text-sm text-text-secondary">
          Try safe, read-only tools directly against the live endpoint — no authentication required.
          Results are real.
        </p>
        <div className="mt-2 inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md
                        bg-accent-orange/10 border border-accent-orange/20 text-xs text-accent-orange">
          <AlertCircle className="w-3 h-3" />
          Rate limited to 10 requests/minute per IP
        </div>
      </div>

      <div className="grid lg:grid-cols-[260px_1fr] gap-5">
        {/* Tool selector */}
        <div className="space-y-1.5">
          <p className="text-xs text-text-muted font-medium uppercase tracking-wider mb-2">Available tools</p>
          {SANDBOX_TOOLS.map(t => (
            <button
              key={t.name}
              onClick={() => selectTool(t)}
              className={`w-full text-left px-3 py-2.5 rounded-lg text-sm transition-all duration-150 ${
                selected.name === t.name
                  ? 'bg-accent-green/10 border border-accent-green/20 text-accent-green'
                  : 'text-text-secondary hover:bg-bg-hover hover:text-text-primary border border-transparent'
              }`}
            >
              <code className="font-mono text-xs">{t.name}</code>
              <p className="text-xs text-text-muted mt-0.5 line-clamp-1">{t.description.slice(0, 60)}…</p>
            </button>
          ))}

          {/* Info box */}
          <div className="mt-4 rounded-lg border border-border bg-bg-secondary p-3 text-xs text-text-muted leading-relaxed">
            Sandbox tools are read-only and don't require authentication.
            <br /><br />
            To access all {'{'}108{'}'} tools, <a href="/ui/auth" className="text-accent-green hover:underline">create a JWT token →</a>
          </div>
        </div>

        {/* Main panel */}
        <div className="space-y-4">
          {/* Selected tool header */}
          <div className="card">
            <div className="flex items-start justify-between gap-3">
              <div>
                <code className="text-lg font-mono font-semibold text-accent-green">{selected.name}</code>
                <p className="text-sm text-text-secondary mt-1">{selected.description}</p>
              </div>
            </div>

            {/* Params */}
            {selected.params.length > 0 && (
              <div className="mt-4 space-y-3">
                <p className="text-xs font-medium text-text-secondary">Parameters</p>
                {selected.params.map(p => (
                  <div key={p.name}>
                    <label className="block text-xs text-text-muted mb-1 font-mono">
                      {p.name}
                      {p.required && <span className="text-accent-red ml-1">*</span>}
                      {p.default && <span className="text-text-muted ml-1">(default: {p.default})</span>}
                    </label>
                    <input
                      type={p.type === 'number' ? 'number' : 'text'}
                      placeholder={p.description}
                      value={args[p.name] ?? ''}
                      onChange={e => setArgs(prev => ({ ...prev, [p.name]: e.target.value }))}
                      className="input text-xs font-mono"
                    />
                  </div>
                ))}
              </div>
            )}

            {/* Run button */}
            <div className="mt-4 flex items-center gap-3">
              <button
                onClick={run}
                disabled={state === 'loading'}
                className="btn-primary"
              >
                {state === 'loading'
                  ? <><Loader2 className="w-4 h-4 animate-spin" /> Running…</>
                  : <><Play className="w-4 h-4" /> Run tool</>
                }
              </button>
              {state === 'done' && (
                <span className="text-xs text-text-muted font-mono">{duration}ms</span>
              )}
            </div>
          </div>

          {/* Output */}
          {(state === 'done' || state === 'error') && (
            <div className="animate-slide-up">
              <div className="terminal">
                <div className="terminal-bar justify-between">
                  <div className="flex items-center gap-3">
                    <div className="flex gap-1.5">
                      <div className="terminal-dot bg-[#ff5f57]" />
                      <div className="terminal-dot bg-[#ffbd2e]" />
                      <div className="terminal-dot bg-[#28c840]" />
                    </div>
                    <span className="text-xs text-text-muted">output</span>
                  </div>
                  <span className={`text-xs font-mono ${state === 'done' ? 'text-accent-green' : 'text-accent-red'}`}>
                    {state === 'done' ? '✓ success' : '✗ error'}
                  </span>
                </div>
                <pre className="p-4 text-xs leading-relaxed overflow-x-auto whitespace-pre-wrap">
                  <code className={state === 'error' ? 'text-accent-red' : 'text-text-code'}>
                    {state === 'error' ? error : output}
                  </code>
                </pre>
              </div>
            </div>
          )}

          {/* Request preview */}
          <details className="group">
            <summary className="cursor-pointer flex items-center gap-1.5 text-xs text-text-muted
                                hover:text-text-secondary transition-colors list-none">
              <ChevronRight className="w-3.5 h-3.5 group-open:rotate-90 transition-transform" />
              Show JSON-RPC request
            </summary>
            <div className="mt-2">
              <CodeBlock code={requestPreview} label="Request preview" />
            </div>
          </details>
        </div>
      </div>
    </div>
  )
}
