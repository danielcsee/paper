import { useState } from 'react'
import ChatWindow from './components/ChatWindow'
import Sidebar from './components/Sidebar'
import type { Message, Source } from './types'

export default function App() {
  const [messages, setMessages] = useState<Message[]>([])
  // Populated by the retrieval layer once /query exists.
  const [sources] = useState<Source[]>([])

  function handleSend(text: string) {
    setMessages((prev) => [
      ...prev,
      { id: crypto.randomUUID(), role: 'user', text },
      {
        id: crypto.randomUUID(),
        role: 'assistant',
        text: 'The retrieval pipeline is not wired up yet — this is the UI shell only.',
      },
    ])
  }

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true" />
          <span className="brand-name">litgraph</span>
        </div>
        <span className="brand-tagline">GraphRAG over scientific literature</span>
      </header>

      <main className="layout">
        <ChatWindow messages={messages} onSend={handleSend} />
        <Sidebar sources={sources} />
      </main>
    </div>
  )
}
