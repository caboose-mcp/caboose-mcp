import { useParams, Link, useNavigate } from 'react-router-dom'
import { ArrowLeft, Globe, Cpu, Tag, FlaskConical } from 'lucide-react'
import { TOOL_MAP, CATEGORIES } from '../data/tools'
import CodeBlock from '../components/CodeBlock'

export default function ToolDetail() {
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const tool = name ? TOOL_MAP.get(name) : undefined

  if (!tool) {
    return (
      <div className="max-w-7xl mx-auto px-4 sm:px-6 py-16 text-center animate-fade-in">
        <p className="text-text-secondary mb-4">Tool <code className="font-mono text-accent-green">{name}</code> not found.</p>
        <Link to="/tools" className="btn-ghost text-sm">
          <ArrowLeft className="w-4 h-4" /> Back to catalog
        </Link>
      </div>
    )
  }

  const catColor = CATEGORIES.find(c => c.id === tool.category)?.color ?? 'text-text-muted'

  const exampleCall = tool.example
    ? JSON.stringify({
        jsonrpc: '2.0',
        method: 'tools/call',
        params: { name: tool.name, arguments: tool.example.input },
      }, null, 2)
    : ''

  const exampleResponse = tool.example
    ? JSON.stringify({
        jsonrpc: '2.0',
        result: { content: [{ type: 'text', text: tool.example.output }] },
      }, null, 2)
    : ''

  return (
    <div className="max-w-4xl mx-auto px-4 sm:px-6 py-8 animate-fade-in">
      {/* Back */}
      <button
        onClick={() => navigate(-1)}
        className="flex items-center gap-1.5 text-sm text-text-muted hover:text-text-secondary mb-6 transition-colors"
      >
        <ArrowLeft className="w-4 h-4" /> Back
      </button>

      {/* Header */}
      <div className="mb-6">
        <div className="flex flex-wrap items-center gap-3 mb-3">
          <code className="text-xl font-mono font-semibold text-accent-green">{tool.name}</code>
          <span className={`tag border ${tool.tier === 'hosted' ? 'text-accent-blue bg-accent-blue/10 border-accent-blue/20' : 'text-accent-orange bg-accent-orange/10 border-accent-orange/20'}`}>
            {tool.tier === 'hosted'
              ? <><Globe className="w-2.5 h-2.5 mr-1" />hosted</>
              : <><Cpu className="w-2.5 h-2.5 mr-1" />local</>
            }
          </span>
          <span className={`text-xs font-medium ${catColor}`}>{tool.category}</span>
          {tool.sandboxable && (
            <Link to="/sandbox" className="flex items-center gap-1 tag bg-accent-green/10 text-accent-green border border-accent-green/20 hover:bg-accent-green/15 transition-colors">
              <FlaskConical className="w-2.5 h-2.5" /> try in sandbox
            </Link>
          )}
        </div>
        <p className="text-base text-text-secondary leading-relaxed">{tool.description}</p>

        {/* Tags */}
        {tool.tags && tool.tags.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mt-3">
            {tool.tags.map(tag => (
              <span key={tag} className="flex items-center gap-1 tag bg-bg-secondary text-text-muted border border-border">
                <Tag className="w-2.5 h-2.5" />{tag}
              </span>
            ))}
          </div>
        )}
      </div>

      {/* Parameters */}
      <section className="mb-6">
        <h2 className="text-sm font-semibold text-text-primary mb-3">Parameters</h2>
        {tool.params.length === 0 ? (
          <p className="text-sm text-text-muted italic">No parameters — call with an empty arguments object.</p>
        ) : (
          <div className="border border-border rounded-xl overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="bg-bg-secondary border-b border-border">
                  <th className="text-left px-4 py-2.5 text-xs text-text-muted font-medium">Name</th>
                  <th className="text-left px-4 py-2.5 text-xs text-text-muted font-medium">Type</th>
                  <th className="text-left px-4 py-2.5 text-xs text-text-muted font-medium">Required</th>
                  <th className="text-left px-4 py-2.5 text-xs text-text-muted font-medium">Description</th>
                </tr>
              </thead>
              <tbody>
                {tool.params.map((p, i) => (
                  <tr key={p.name} className={i < tool.params.length - 1 ? 'border-b border-border/50' : ''}>
                    <td className="px-4 py-3">
                      <code className="font-mono text-accent-green text-xs">{p.name}</code>
                    </td>
                    <td className="px-4 py-3">
                      <span className="tag bg-bg-secondary text-text-code border border-border text-xs">{p.type}</span>
                    </td>
                    <td className="px-4 py-3">
                      {p.required
                        ? <span className="text-xs text-accent-red">required</span>
                        : <span className="text-xs text-text-muted">optional{p.default ? ` (${p.default})` : ''}</span>
                      }
                    </td>
                    <td className="px-4 py-3 text-xs text-text-secondary">{p.description}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Example */}
      {tool.example && (
        <section className="mb-6">
          <h2 className="text-sm font-semibold text-text-primary mb-3">Example</h2>
          <div className="grid md:grid-cols-2 gap-3">
            <CodeBlock code={exampleCall} label="Request" />
            <div className="terminal">
              <div className="terminal-bar">
                <div className="flex gap-1.5">
                  <div className="terminal-dot bg-[#ff5f57]" />
                  <div className="terminal-dot bg-[#ffbd2e]" />
                  <div className="terminal-dot bg-[#28c840]" />
                </div>
                <span className="text-xs text-text-muted ml-3">Response</span>
              </div>
              <pre className="p-4 text-xs leading-relaxed overflow-x-auto whitespace-pre-wrap">
                <code className="text-text-code">{tool.example.output}</code>
              </pre>
            </div>
          </div>
        </section>
      )}

      {/* Claude Code usage */}
      <section className="mb-6">
        <h2 className="text-sm font-semibold text-text-primary mb-3">Usage in Claude</h2>
        <CodeBlock
          code={`${tool.name}(${tool.params.filter(p => p.required).map(p => `${p.name}="<value>"`).join(', ')})`}
          label="Claude prompt"
        />
      </section>

      {/* Tier note */}
      {tool.tier === 'local' && (
        <div className="rounded-lg border border-accent-orange/20 bg-accent-orange/5 p-4 text-xs text-text-secondary">
          <span className="font-medium text-accent-orange">Local tier:</span>{' '}
          This tool requires local hardware access (Docker, shell, Bambu printer, etc.).
          It is available with <code className="font-mono">--serve</code> or <code className="font-mono">--serve-local</code> on your own machine,
          but not on the hosted <code className="font-mono">mcp.chrismarasco.io</code> endpoint.
        </div>
      )}
    </div>
  )
}
