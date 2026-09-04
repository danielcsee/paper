import { useState } from 'react'
import ChatWindow from './components/ChatWindow'
import CorpusView from './components/CorpusView'
import Sidebar from './components/Sidebar'
import type { Message } from './types'

type View = 'chat' | 'corpus'

export default function App() {
  const [messages, setMessages] = useState<Message[]>([])
  const [view, setView] = useState<View>('chat')

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
        <nav className="tabs">
          <button
            type="button"
            className={`tab${view === 'corpus' ? ' tab-active' : ''}`}
            aria-current={view === 'corpus' ? 'page' : undefined}
            onClick={() => setView('corpus')}
          >
            My Corpus
          </button>
        </nav>
        <span className="brand-tagline">GraphRAG over scientific literature</span>
      </header>

      <main className="layout">
        {/* The sidebar stays mounted across views: switching must not throw
            away a search, its scroll position, or a pending selection. */}
        {view === 'corpus' ? (
          <CorpusView onClose={() => setView('chat')} />
        ) : (
          <ChatWindow messages={messages} onSend={handleSend} />
        )}
        <Sidebar />
      </main>
    </div>
  )
}
