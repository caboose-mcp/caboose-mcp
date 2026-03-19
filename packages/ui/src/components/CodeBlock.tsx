import { useState } from 'react'
import { Check, Copy } from 'lucide-react'

type Props = {
  code: string
  language?: string
  label?: string
  className?: string
}

export default function CodeBlock({ code, language = 'text', label, className = '' }: Props) {
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    await navigator.clipboard.writeText(code)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className={`terminal ${className}`}>
      <div className="terminal-bar justify-between">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5">
            <div className="terminal-dot bg-[#ff5f57]" />
            <div className="terminal-dot bg-[#ffbd2e]" />
            <div className="terminal-dot bg-[#28c840]" />
          </div>
          {label && <span className="text-xs text-text-muted">{label}</span>}
        </div>
        <button
          onClick={copy}
          className="flex items-center gap-1 text-xs text-text-muted hover:text-text-secondary transition-colors"
          aria-label="Copy code"
        >
          {copied
            ? <><Check className="w-3 h-3 text-accent-green" /> copied</>
            : <><Copy className="w-3 h-3" /> copy</>
          }
        </button>
      </div>
      <pre className="p-4 overflow-x-auto text-xs leading-relaxed">
        <code className="text-text-code">{code}</code>
      </pre>
    </div>
  )
}
