export type SandboxRequest = {
  tool: string
  args: Record<string, unknown>
}

export type SandboxResponse = {
  output: string
  error?: string
  duration_ms: number
}

// In production the UI is served from the same origin as the Go server.
// In dev (Vite proxy), /api is proxied to http://localhost:8080.
const API_BASE = ''

export async function runSandboxTool(req: SandboxRequest): Promise<SandboxResponse> {
  const res = await fetch(`${API_BASE}/api/sandbox`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })

  if (!res.ok) {
    const text = await res.text()
    throw new Error(`${res.status}: ${text}`)
  }

  return res.json() as Promise<SandboxResponse>
}

export async function exchangeMagicLink(token: string): Promise<{
  token: string
  jti: string
  expires_at: string
}> {
  const url = new URL('/auth/verify', window.location.origin)
  url.searchParams.set('token', token)

  const res = await fetch(url.toString())
  if (!res.ok) {
    const data = await res.json().catch(() => ({ error: res.statusText })) as { error?: string }
    throw new Error(data.error ?? res.statusText)
  }
  return res.json()
}
