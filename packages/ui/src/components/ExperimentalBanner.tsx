import { TriangleAlert } from 'lucide-react'

export default function ExperimentalBanner() {
  return (
    <div className="w-full bg-amber-950/60 border-b border-amber-700/50 text-amber-200">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 py-2 flex items-center gap-2.5">
        <TriangleAlert className="w-3.5 h-3.5 text-amber-400 shrink-0" />
        <p className="text-xs leading-relaxed">
          <span className="font-semibold text-amber-300">Experimental software — use at your own risk.</span>
          {' '}Not fully tested. Features may change or break without notice. No warranty provided.{' '}
          <a
            href="https://github.com/caboose-mcp/caboose-mcp#disclaimer"
            target="_blank"
            rel="noopener noreferrer"
            className="underline underline-offset-2 hover:text-amber-100 transition-colors"
          >
            Full disclaimer &#8594;
          </a>
        </p>
      </div>
    </div>
  )
}
