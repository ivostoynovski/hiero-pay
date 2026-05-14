import { useEffect, useState } from 'react'

function App() {
  const [health, setHealth] = useState<string>('checking…')

  useEffect(() => {
    fetch('/api/health')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(r.statusText))))
      .then((data) => setHealth(data.status ?? 'unknown'))
      .catch((err) => setHealth(`error: ${err.message}`))
  }, [])

  return (
    <main className="min-h-screen flex items-center justify-center bg-background text-foreground p-8">
      <div className="max-w-md text-center space-y-4">
        <h1 className="text-4xl font-semibold tracking-tight">hiero-pay</h1>
        <p className="text-muted-foreground">
          Chat-driven payments — Slice 1 scaffold.
        </p>
        <p className="text-sm">
          Backend status: <code className="font-mono">{health}</code>
        </p>
      </div>
    </main>
  )
}

export default App
