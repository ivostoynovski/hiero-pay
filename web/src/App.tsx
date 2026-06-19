import { useEffect, useRef, useState, type KeyboardEvent } from 'react'

type Role = 'user' | 'assistant'

type Message = {
  role: Role
  content: string
}

function App() {
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const scrollRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight })
  }, [messages, loading])

  async function send() {
    const trimmed = input.trim()
    if (!trimmed || loading) return

    const userMsg: Message = { role: 'user', content: trimmed }
    const next = [...messages, userMsg]
    setMessages(next)
    setInput('')
    setLoading(true)
    setError(null)

    try {
      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ messages: next }),
      })
      const data = await res.json().catch(() => null)
      if (!res.ok) {
        const reason = data?.error ?? `${res.status} ${res.statusText}`
        throw new Error(reason)
      }
      const reply = (data?.message as string | undefined) ?? ''
      setMessages([...next, { role: 'assistant', content: reply }])
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void send()
    }
  }

  return (
    <main className="min-h-screen flex flex-col bg-background text-foreground">
      <header className="border-b px-6 py-4">
        <h1 className="text-lg font-semibold tracking-tight">hiero-pay</h1>
        <p className="text-xs text-muted-foreground">
          Chat with your local payment assistant. Page reload resets the
          conversation.
        </p>
      </header>

      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto px-6 py-6 space-y-4"
      >
        {messages.length === 0 && !loading && !error ? (
          <p className="text-sm text-muted-foreground">
            Say hi to get started. (Payments and history land in a follow-up
            slice — for now the assistant only chats.)
          </p>
        ) : null}

        {messages.map((m, i) => (
          <MessageBubble key={i} message={m} />
        ))}

        {loading ? (
          <div className="text-sm text-muted-foreground italic">
            Thinking…
          </div>
        ) : null}

        {error ? (
          <div className="rounded-md border border-destructive/40 bg-destructive/10 text-destructive px-3 py-2 text-sm">
            {error}
          </div>
        ) : null}
      </div>

      <form
        className="border-t px-6 py-4"
        onSubmit={(e) => {
          e.preventDefault()
          void send()
        }}
      >
        <div className="flex gap-2 items-end">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={onKeyDown}
            disabled={loading}
            rows={1}
            placeholder="Type a message — Enter to send, Shift+Enter for newline"
            className="flex-1 resize-none rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
          />
          <button
            type="submit"
            disabled={loading || !input.trim()}
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90 disabled:opacity-40"
          >
            Send
          </button>
        </div>
      </form>
    </main>
  )
}

function MessageBubble({ message }: { message: Message }) {
  const isUser = message.role === 'user'
  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div
        className={`max-w-[80%] whitespace-pre-wrap rounded-lg px-3 py-2 text-sm ${
          isUser
            ? 'bg-primary text-primary-foreground'
            : 'bg-muted text-foreground'
        }`}
      >
        {message.content}
      </div>
    </div>
  )
}

export default App
