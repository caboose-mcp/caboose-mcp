import { Link } from 'react-router-dom'
import {
  BookOpen, FlaskConical, KeyRound, ArrowRight,
  Calendar, Focus, Database,
  Shield, Bot, Server
} from 'lucide-react'
import { TOOL_COUNT } from '../data/tools'

const STATS = [
  { value: `${TOOL_COUNT}+`, label: 'MCP Tools' },
  { value: '6', label: 'Categories' },
  { value: '2', label: 'Chat Bots' },
  { value: 'JWT', label: 'Auth Model' },
]

const FEATURE_GROUPS = [
  {
    icon: Calendar,
    label: 'Calendar & Notes',
    color: 'text-blue-400',
    bg: 'bg-blue-400/10',
    desc: 'Full Google Calendar OAuth2 — list, create, delete events. Quick notes with tags and Google Drive backup.',
    tools: ['calendar_list', 'calendar_create', 'note_add', 'notes_drive_backup'],
  },
  {
    icon: Focus,
    label: 'Focus & Learning',
    color: 'text-purple-400',
    bg: 'bg-purple-400/10',
    desc: 'ADHD-friendly focus sessions with parking lot, plus multi-language learning with streaks and exercises.',
    tools: ['focus_start', 'focus_park', 'learn_start', 'learn_submit'],
  },
  {
    icon: Bot,
    label: 'Discord & Slack Bots',
    color: 'text-indigo-400',
    bg: 'bg-indigo-400/10',
    desc: 'Full chat-bot agents on Discord and Slack. SSO via identity linking — one JWT covers all surfaces.',
    tools: ['discord_post_message', 'slack_post_message', 'auth_link_identity'],
  },
  {
    icon: Shield,
    label: 'JWT RBAC Auth',
    color: 'text-green-400',
    bg: 'bg-green-400/10',
    desc: 'Per-token tool allowlists, magic link exchange, identity SSO. Static admin token as superuser bypass.',
    tools: ['auth_create_token', 'auth_link_identity', 'auth_revoke_token'],
  },
  {
    icon: Database,
    label: 'Database & GitHub',
    color: 'text-orange-400',
    bg: 'bg-orange-400/10',
    desc: 'PostgreSQL + MongoDB queries, GitHub repo management and code search via gh CLI.',
    tools: ['postgres_query', 'mongodb_query', 'github_create_pr'],
  },
  {
    icon: Server,
    label: 'System & Docker',
    color: 'text-red-400',
    bg: 'bg-red-400/10',
    desc: 'Gated shell execution, Docker container management and logs, health reports. Local tier only.',
    tools: ['execute_command', 'docker_list_containers', 'health_report'],
  },
]

const REQUEST_EXAMPLE = `POST https://mcp.chrismarasco.io/mcp
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "calendar_list",
    "arguments": { "days": 7 }
  }
}`

const RESPONSE_EXAMPLE = `{
  "jsonrpc": "2.0",
  "result": {
    "content": [{
      "type": "text",
      "text": "Upcoming events (next 7 days):\\n\\n• Team standup..."
    }]
  }
}`

export default function Home() {
  return (
    <div className="animate-fade-in">
      {/* Hero */}
      <section className="relative overflow-hidden">
        <div className="absolute inset-0 bg-grid-pattern bg-grid opacity-100" />
        <div className="absolute inset-0 bg-hero-gradient" />

        <div className="relative max-w-7xl mx-auto px-4 sm:px-6 pt-20 pb-16">
          <div className="max-w-3xl">
            {/* Badge */}
            <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full
                            border border-accent-green/30 bg-accent-green/5 mb-6">
              <span className="w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse-slow" />
              <span className="text-xs text-accent-green font-mono">
                mcp.chrismarasco.io/mcp · live
              </span>
            </div>

            <h1 className="text-4xl sm:text-5xl font-bold text-text-primary leading-tight mb-4">
              Your AI assistant,{' '}
              <span className="text-accent-green">supercharged</span>
            </h1>
            <p className="text-lg text-text-secondary leading-relaxed mb-8 max-w-2xl">
              caboose-mcp is a Go server exposing {TOOL_COUNT}+ MCP tools to Claude, VS Code, Discord, and Slack.
              Calendar, notes, focus sessions, GitHub, Docker, databases — all in one JSON-RPC endpoint.
            </p>

            <div className="flex flex-wrap gap-3 mb-12">
              <Link to="/tools" className="btn-primary">
                <BookOpen className="w-4 h-4" />
                Browse tools
                <ArrowRight className="w-4 h-4" />
              </Link>
              <Link to="/sandbox" className="btn-ghost">
                <FlaskConical className="w-4 h-4" />
                Live sandbox
              </Link>
            </div>

            {/* Stats row */}
            <div className="flex flex-wrap gap-6">
              {STATS.map(s => (
                <div key={s.label}>
                  <p className="text-2xl font-bold font-mono text-accent-green">{s.value}</p>
                  <p className="text-xs text-text-muted">{s.label}</p>
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* API example */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 py-12">
        <div className="grid md:grid-cols-2 gap-4">
          <div>
            <p className="text-xs text-text-muted font-mono mb-2 uppercase tracking-wider">Request</p>
            <div className="terminal h-full">
              <div className="terminal-bar">
                <div className="flex items-center gap-1.5">
                  <div className="terminal-dot bg-[#ff5f57]" />
                  <div className="terminal-dot bg-[#ffbd2e]" />
                  <div className="terminal-dot bg-[#28c840]" />
                </div>
                <span className="text-xs text-text-muted ml-3 font-mono">tools/call</span>
              </div>
              <pre className="p-4 text-xs leading-relaxed overflow-x-auto">
                <code className="text-text-code">{REQUEST_EXAMPLE}</code>
              </pre>
            </div>
          </div>
          <div>
            <p className="text-xs text-text-muted font-mono mb-2 uppercase tracking-wider">Response</p>
            <div className="terminal h-full">
              <div className="terminal-bar">
                <div className="flex items-center gap-1.5">
                  <div className="terminal-dot bg-[#ff5f57]" />
                  <div className="terminal-dot bg-[#ffbd2e]" />
                  <div className="terminal-dot bg-[#28c840]" />
                </div>
                <span className="text-xs text-accent-green ml-3 font-mono">200 OK</span>
              </div>
              <pre className="p-4 text-xs leading-relaxed overflow-x-auto">
                <code className="text-text-code">{RESPONSE_EXAMPLE}</code>
              </pre>
            </div>
          </div>
        </div>
      </section>

      {/* Feature groups */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 py-8">
        <h2 className="text-xl font-semibold text-text-primary mb-2">What can it do?</h2>
        <p className="text-sm text-text-secondary mb-8">
          Every tool is a JSON-RPC call. Connect via Claude, VS Code MCP extension, or HTTP client.
        </p>

        <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {FEATURE_GROUPS.map(g => {
            const Icon = g.icon
            return (
              <div key={g.label} className="card-glow group">
                <div className={`w-8 h-8 rounded-lg ${g.bg} flex items-center justify-center mb-3`}>
                  <Icon className={`w-4 h-4 ${g.color}`} />
                </div>
                <h3 className="text-sm font-semibold text-text-primary mb-1">{g.label}</h3>
                <p className="text-xs text-text-secondary leading-relaxed mb-3">{g.desc}</p>
                <div className="flex flex-wrap gap-1">
                  {g.tools.map(t => (
                    <Link key={t} to={`/tools/${t}`}
                      className="text-xs font-mono text-text-muted hover:text-accent-green
                                 bg-bg border border-border hover:border-accent-green/30
                                 px-1.5 py-0.5 rounded transition-all duration-150">
                      {t}
                    </Link>
                  ))}
                </div>
              </div>
            )
          })}
        </div>
      </section>

      {/* Auth callout */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 py-8">
        <div className="rounded-xl border border-accent-green/20 bg-accent-green/5 p-6">
          <div className="flex flex-col sm:flex-row gap-4 items-start sm:items-center justify-between">
            <div>
              <h3 className="text-base font-semibold text-text-primary mb-1 flex items-center gap-2">
                <Shield className="w-4 h-4 text-accent-green" />
                JWT RBAC — fine-grained access control
              </h3>
              <p className="text-sm text-text-secondary">
                Create tokens with per-tool allowlists. Link Discord, Slack, or Google identities for SSO.
                Superuser fallback via <code className="text-xs font-mono text-text-code">MCP_AUTH_TOKEN</code>.
              </p>
            </div>
            <div className="flex gap-2 shrink-0">
              <Link to="/auth" className="btn-primary text-xs px-3 py-1.5">
                <KeyRound className="w-3.5 h-3.5" />
                Auth portal
              </Link>
              <Link to="/tools/auth_create_token" className="btn-ghost text-xs px-3 py-1.5">
                Docs
              </Link>
            </div>
          </div>
        </div>
      </section>

      {/* Quick start */}
      <section className="max-w-7xl mx-auto px-4 sm:px-6 py-8 pb-16">
        <h2 className="text-xl font-semibold text-text-primary mb-6">Quick start</h2>
        <div className="grid sm:grid-cols-3 gap-4">
          {[
            {
              step: '01',
              title: 'Add to Claude Code',
              code: `# .mcp.json\n{\n  "mcpServers": {\n    "caboose": {\n      "url": "https://mcp.chrismarasco.io/mcp",\n      "headers": {\n        "Authorization": "Bearer <token>"\n      }\n    }\n  }\n}`,
            },
            {
              step: '02',
              title: 'Create a scoped token',
              code: `./caboose-mcp auth:create \\\n  --label "my-client" \\\n  --tools "calendar_list,note_add" \\\n  --expires 90\n# → magic link (15 min)`,
            },
            {
              step: '03',
              title: 'Exchange for JWT',
              code: `curl https://mcp.chrismarasco.io\\\n  /auth/verify?token=<magic>\n\n# → { "token": "eyJ...", "jti": "...", "expires_at": "..." }`,
            },
          ].map(({ step, title, code }) => (
            <div key={step} className="card">
              <div className="flex items-center gap-2 mb-3">
                <span className="text-xs font-mono text-accent-green/50">{step}</span>
                <h3 className="text-sm font-medium text-text-primary">{title}</h3>
              </div>
              <div className="terminal">
                <div className="terminal-bar">
                  <div className="flex gap-1.5">
                    <div className="terminal-dot bg-[#ff5f57]" />
                    <div className="terminal-dot bg-[#ffbd2e]" />
                    <div className="terminal-dot bg-[#28c840]" />
                  </div>
                </div>
                <pre className="p-3 text-xs overflow-x-auto"><code className="text-text-code">{code}</code></pre>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  )
}
