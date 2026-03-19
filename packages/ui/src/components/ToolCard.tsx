import { Link } from 'react-router-dom'
import { ArrowRight, Lock, Globe, Cpu } from 'lucide-react'
import type { ToolDef } from '../data/tools'
import { CATEGORIES } from '../data/tools'

type Props = {
  tool: ToolDef
  compact?: boolean
}

const TIER_BADGE = {
  hosted: { label: 'hosted', icon: Globe, color: 'text-accent-blue bg-accent-blue/10 border-accent-blue/20' },
  local: { label: 'local', icon: Cpu, color: 'text-accent-orange bg-accent-orange/10 border-accent-orange/20' },
  both: { label: 'hosted+local', icon: Globe, color: 'text-accent-purple bg-accent-purple/10 border-accent-purple/20' },
} as const

export default function ToolCard({ tool, compact }: Props) {
  const tier = TIER_BADGE[tool.tier]
  const TierIcon = tier.icon
  const catColor = CATEGORIES.find(c => c.id === tool.category)?.color ?? 'text-text-muted'

  return (
    <Link
      to={`/tools/${tool.name}`}
      className="group block card-glow cursor-pointer animate-fade-in"
    >
      <div className="flex items-start justify-between gap-2 mb-2">
        <code className="text-sm font-mono text-accent-green group-hover:text-accent-green/80 transition-colors leading-tight">
          {tool.name}
        </code>
        <span className={`tag border ${tier.color} shrink-0`}>
          <TierIcon className="w-2.5 h-2.5 mr-0.5" />
          {tier.label}
        </span>
      </div>

      {!compact && (
        <p className="text-xs text-text-secondary leading-relaxed mb-3 line-clamp-2">
          {tool.description}
        </p>
      )}

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <span className={`text-xs font-medium ${catColor}`}>{tool.category}</span>
          {tool.params.filter(p => p.required).length > 0 && (
            <span className="tag bg-bg text-text-muted border border-border">
              {tool.params.filter(p => p.required).length} required
            </span>
          )}
          {tool.sandboxable && (
            <span className="tag bg-accent-green/10 text-accent-green border border-accent-green/20">
              sandbox
            </span>
          )}
        </div>
        <ArrowRight className="w-3 h-3 text-text-muted group-hover:text-text-secondary
                                group-hover:translate-x-0.5 transition-all duration-150" />
      </div>
    </Link>
  )
}
