import { useState, useMemo } from 'react'
import { Search, Globe, Cpu, X } from 'lucide-react'
import ToolCard from '../components/ToolCard'
import { CATEGORIES, searchTools, getToolsByCategory, TOOL_COUNT } from '../data/tools'

type Tier = 'all' | 'hosted' | 'local'

export default function Tools() {
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState('all')
  const [tier, setTier] = useState<Tier>('all')

  const results = useMemo(() => {
    let list = query ? searchTools(query) : getToolsByCategory(category)
    if (tier !== 'all') {
      list = list.filter(t => t.tier === tier || t.tier === 'both')
    }
    return list
  }, [query, category, tier])

  const clearFilters = () => {
    setQuery('')
    setCategory('all')
    setTier('all')
  }

  const hasFilters = query || category !== 'all' || tier !== 'all'

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 py-8 animate-fade-in">
      {/* Header */}
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-text-primary mb-1">Tool Catalog</h1>
        <p className="text-sm text-text-secondary">
          {TOOL_COUNT} tools across {CATEGORIES.length - 1} categories. Click any tool for full docs and examples.
        </p>
      </div>

      {/* Search + filters */}
      <div className="flex flex-col sm:flex-row gap-3 mb-6">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted" />
          <input
            type="text"
            placeholder="Search tools, descriptions, tags..."
            value={query}
            onChange={e => setQuery(e.target.value)}
            className="input pl-9"
          />
          {query && (
            <button onClick={() => setQuery('')}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-text-muted hover:text-text-secondary">
              <X className="w-3.5 h-3.5" />
            </button>
          )}
        </div>

        {/* Tier filter */}
        <div className="flex rounded-lg border border-border overflow-hidden">
          {(['all', 'hosted', 'local'] as Tier[]).map(t => (
            <button
              key={t}
              onClick={() => setTier(t)}
              className={`flex items-center gap-1.5 px-3 py-2 text-xs transition-all duration-150 ${
                tier === t
                  ? 'bg-bg-hover text-text-primary'
                  : 'text-text-muted hover:text-text-secondary'
              }`}
            >
              {t === 'hosted' && <Globe className="w-3 h-3" />}
              {t === 'local' && <Cpu className="w-3 h-3" />}
              {t}
            </button>
          ))}
        </div>

        {hasFilters && (
          <button onClick={clearFilters} className="btn-ghost text-xs">
            <X className="w-3 h-3" />
            Clear
          </button>
        )}
      </div>

      {/* Category pills */}
      {!query && (
        <div className="flex flex-wrap gap-1.5 mb-6">
          {CATEGORIES.map(cat => (
            <button
              key={cat.id}
              onClick={() => setCategory(cat.id)}
              className={`text-xs px-2.5 py-1 rounded-full border transition-all duration-150 ${
                category === cat.id
                  ? `${cat.color} border-current bg-current/10`
                  : 'text-text-muted border-border hover:border-border-bright hover:text-text-secondary'
              }`}
            >
              {cat.label}
            </button>
          ))}
        </div>
      )}

      {/* Results */}
      <div className="mb-3 flex items-center justify-between">
        <p className="text-xs text-text-muted">
          {results.length} tool{results.length !== 1 ? 's' : ''}{query ? ` for "${query}"` : ''}
        </p>
      </div>

      {results.length === 0 ? (
        <div className="text-center py-16">
          <Search className="w-8 h-8 text-text-muted mx-auto mb-3" />
          <p className="text-text-secondary mb-1">No tools found</p>
          <p className="text-xs text-text-muted">Try adjusting your search or filters</p>
        </div>
      ) : (
        <div className="grid sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
          {results.map(tool => (
            <ToolCard key={tool.name} tool={tool} />
          ))}
        </div>
      )}
    </div>
  )
}
