import { useState } from 'react'
import { Link, NavLink, useLocation } from 'react-router-dom'
import { Terminal, BookOpen, FlaskConical, KeyRound, Clock, Menu, X, Zap } from 'lucide-react'

const NAV = [
  { to: '/tools', label: 'Tools', icon: BookOpen },
  { to: '/sandbox', label: 'Sandbox', icon: FlaskConical },
  { to: '/auth', label: 'Auth', icon: KeyRound },
  { to: '/changelog', label: 'Changelog', icon: Clock },
]

export default function Navbar() {
  const [open, setOpen] = useState(false)
  const { pathname } = useLocation()

  return (
    <nav className="sticky top-0 z-50 border-b border-border bg-bg/95 backdrop-blur-sm">
      <div className="max-w-7xl mx-auto px-4 sm:px-6">
        <div className="flex items-center justify-between h-14">
          {/* Logo */}
          <Link to="/" className="flex items-center gap-2 group">
            <div className="w-7 h-7 rounded-md bg-accent-green/10 border border-accent-green/30
                            flex items-center justify-center group-hover:bg-accent-green/20 transition-colors">
              <Zap className="w-3.5 h-3.5 text-accent-green" />
            </div>
            <span className="font-mono font-medium text-text-primary text-sm">
              caboose<span className="text-accent-green">-mcp</span>
            </span>
          </Link>

          {/* Desktop nav */}
          <div className="hidden sm:flex items-center gap-1">
            {NAV.map(({ to, label, icon: Icon }) => (
              <NavLink
                key={to}
                to={to}
                className={({ isActive }) =>
                  `flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm transition-all duration-150 ${
                    isActive
                      ? 'text-accent-green bg-accent-green/10'
                      : 'text-text-secondary hover:text-text-primary hover:bg-bg-hover'
                  }`
                }
              >
                <Icon className="w-3.5 h-3.5" />
                {label}
              </NavLink>
            ))}
          </div>

          {/* Right: endpoint pill + hamburger */}
          <div className="flex items-center gap-3">
            <a
              href="https://mcp.chrismarasco.io/mcp"
              target="_blank"
              rel="noopener noreferrer"
              className="hidden md:flex items-center gap-1.5 px-3 py-1 rounded-full
                         bg-accent-green/10 border border-accent-green/20 text-accent-green
                         text-xs font-mono hover:bg-accent-green/15 transition-colors"
            >
              <span className="w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse-slow" />
              live endpoint
            </a>

            <button
              className="sm:hidden p-1.5 rounded-md text-text-secondary hover:text-text-primary"
              onClick={() => setOpen(!open)}
              aria-label="Toggle menu"
            >
              {open ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
            </button>
          </div>
        </div>
      </div>

      {/* Mobile menu */}
      {open && (
        <div className="sm:hidden border-t border-border bg-bg-secondary animate-fade-in">
          {NAV.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              onClick={() => setOpen(false)}
              className={({ isActive }) =>
                `flex items-center gap-2 px-4 py-3 text-sm border-b border-border/50 ${
                  isActive ? 'text-accent-green' : 'text-text-secondary'
                }`
              }
            >
              <Icon className="w-4 h-4" />
              {label}
            </NavLink>
          ))}
        </div>
      )}
    </nav>
  )
}
