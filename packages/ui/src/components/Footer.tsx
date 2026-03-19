import { Github, ExternalLink } from 'lucide-react'
import { Link } from 'react-router-dom'

export default function Footer() {
  return (
    <footer className="border-t border-border bg-bg-secondary mt-16">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 py-8">
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-6 mb-6">
          {/* Brand */}
          <div>
            <p className="font-mono text-sm font-medium text-text-primary mb-1">
              caboose<span className="text-accent-green">-mcp</span>
            </p>
            <p className="text-xs text-text-muted leading-relaxed">
              108 MCP tools for AI assistants.<br />
              Calendar, notes, focus, GitHub, Docker, and more.
            </p>
          </div>

          {/* Links */}
          <div>
            <p className="text-xs font-medium text-text-secondary uppercase tracking-wider mb-2">Explore</p>
            <div className="flex flex-col gap-1">
              {[
                { to: '/tools', label: 'Tool Catalog' },
                { to: '/sandbox', label: 'Live Sandbox' },
                { to: '/auth', label: 'Auth Portal' },
                { to: '/changelog', label: 'Changelog' },
              ].map(({ to, label }) => (
                <Link key={to} to={to} className="text-xs text-text-muted hover:text-text-secondary transition-colors">
                  {label}
                </Link>
              ))}
            </div>
          </div>

          {/* External */}
          <div>
            <p className="text-xs font-medium text-text-secondary uppercase tracking-wider mb-2">Resources</p>
            <div className="flex flex-col gap-1">
              {[
                { href: 'https://github.com/cxm6467/caboose-mcp', label: 'GitHub' },
                { href: 'https://mcp.chrismarasco.io/mcp', label: 'Live Endpoint' },
                { href: 'https://chris.marasco.io', label: 'Chris Marasco' },
              ].map(({ href, label }) => (
                <a key={href} href={href} target="_blank" rel="noopener noreferrer"
                  className="flex items-center gap-1 text-xs text-text-muted hover:text-text-secondary transition-colors">
                  {label}
                  <ExternalLink className="w-2.5 h-2.5" />
                </a>
              ))}
            </div>
          </div>
        </div>

        <div className="border-t border-border/50 pt-4 flex flex-col sm:flex-row items-start sm:items-center
                        justify-between gap-2">
          <p className="text-xs text-text-muted">
            © 2025{' '}
            <a href="https://chris.marasco.io" className="hover:text-text-secondary transition-colors">
              Chris Marasco
            </a>{' '}
            · MIT License
          </p>
          <p className="text-xs text-text-muted">
            Built with{' '}
            <a href="https://claude.ai" className="hover:text-text-secondary transition-colors">Claude</a>
            ,{' '}
            <a href="https://claude.ai/claude-code" className="hover:text-text-secondary transition-colors">Claude Code</a>
            {' '}&{' '}
            <a href="https://github.com/features/copilot" className="hover:text-text-secondary transition-colors">GitHub Copilot</a>
          </p>
        </div>
      </div>
    </footer>
  )
}
