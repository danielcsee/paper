import { useCallback, useEffect, useRef, useState } from 'react'
import type { PaperDetail } from './api'
import ChatWindow from './components/ChatWindow'
import CorpusView from './components/CorpusView'
import PaperTabs from './components/PaperTabs'
import PaperView from './components/PaperView'
import Sidebar from './components/Sidebar'
import {
  CHAT,
  CORPUS,
  loadTabs,
  pathToView,
  sameView,
  saveTabs,
  truncateTitle,
  viewToPath,
  type PaperTab,
  type View,
} from './navigation'
import type { Message } from './types'

export default function App() {
  const [messages, setMessages] = useState<Message[]>([])
  const [tabs, setTabs] = useState<PaperTab[]>(() => {
    // Tabs survive a reload; the URL still decides which one is showing. A
    // shared /paper/12 link opens that tab too, with a placeholder label until
    // PaperView reports the real title.
    const stored = loadTabs()
    const initial = pathToView(window.location.pathname)
    if (initial.kind !== 'paper') return stored
    return stored.some((tab) => tab.paperId === initial.paperId)
      ? stored
      : [...stored, { paperId: initial.paperId, title: `Paper ${initial.paperId}` }]
  })

  // Papers that 404ed this session. Their tab stays so the reader sees why,
  // but it must not come back after a reload.
  const [missing, setMissing] = useState<ReadonlySet<number>>(new Set())
  const [view, setView] = useState<View>(() => pathToView(window.location.pathname))

  useEffect(() => {
    saveTabs(tabs.filter((tab) => !missing.has(tab.paperId)))
  }, [tabs, missing])

  // Where the user has been, oldest first. Closing a paper tab pops back
  // through this, which a single "current view" could not answer.
  const historyRef = useRef<View[]>([])

  const navigate = useCallback((next: View, { push = true } = {}) => {
    setView((current) => {
      if (sameView(current, next)) return current
      historyRef.current = [...historyRef.current.slice(-19), current]
      if (push) {
        const path = viewToPath(next)
        if (path !== window.location.pathname) window.history.pushState({}, '', path)
      }
      return next
    })
  }, [])

  // Browser back/forward.
  useEffect(() => {
    const onPop = () => setView(pathToView(window.location.pathname))
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  function addTab(paperId: number, title: string | null) {
    setTabs((prev) =>
      prev.some((tab) => tab.paperId === paperId)
        ? prev
        : [...prev, { paperId, title: title ?? `Paper ${paperId}` }],
    )
  }

  function openPaper(paperId: number, title: string | null) {
    addTab(paperId, title)
    navigate({ kind: 'paper', paperId })
  }

  /**
   * Queue a paper up without leaving the current view — no navigate, so the
   * visit stack and the URL are untouched and the reader keeps their place.
   */
  function openPaperInBackground(paperId: number, title: string | null) {
    addTab(paperId, title)
  }

  function closePaper(paperId: number) {
    const remaining = tabs.filter((tab) => tab.paperId !== paperId)
    setTabs(remaining)

    // Closing a background tab must not move the user.
    if (view.kind !== 'paper' || view.paperId !== paperId) {
      historyRef.current = historyRef.current.filter(
        (entry) => entry.kind !== 'paper' || entry.paperId !== paperId,
      )
      return
    }

    // Fall back to the most recent view that still exists.
    const open = new Set(remaining.map((tab) => tab.paperId))
    const stack = historyRef.current.filter(
      (entry) => entry.kind !== 'paper' || open.has(entry.paperId),
    )
    const previous = stack.pop() ?? CHAT
    historyRef.current = stack
    setView(previous)
    const path = viewToPath(previous)
    if (path !== window.location.pathname) window.history.pushState({}, '', path)
  }

  const handleMissing = useCallback((paperId: number) => {
    setMissing((prev) => (prev.has(paperId) ? prev : new Set(prev).add(paperId)))
  }, [])

  const handleLoaded = useCallback((paper: PaperDetail) => {
    setTabs((prev) =>
      prev.map((tab) =>
        tab.paperId === paper.paper_id && paper.title
          ? { ...tab, title: paper.title }
          : tab,
      ),
    )
  }, [])

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
        {/* The fixed tab sits outside PaperTabs so it never scrolls with them. */}
        <nav className="tabs">
          <button
            type="button"
            className={`tab${view.kind === 'corpus' ? ' tab-active' : ''}`}
            aria-current={view.kind === 'corpus' ? 'page' : undefined}
            onClick={() => navigate(CORPUS)}
          >
            My Corpus
          </button>
        </nav>
        <PaperTabs
          tabs={tabs}
          active={view}
          onSelect={(paperId) => navigate({ kind: 'paper', paperId })}
          onClose={closePaper}
        />
        <span className="brand-tagline">GraphRAG over scientific literature</span>
      </header>

      <main className="layout">
        {/* The sidebar stays mounted across views: switching must not throw
            away a search, its scroll position, or a pending selection. */}
        {view.kind === 'paper' ? (
          <PaperView
            key={view.paperId}
            paperId={view.paperId}
            onLoaded={handleLoaded}
            onMissing={handleMissing}
          />
        ) : view.kind === 'corpus' ? (
          <CorpusView
            onClose={() => navigate(CHAT)}
            onOpenPaper={(paperId, title) => openPaper(paperId, truncateTitle(title, 200))}
            onOpenPaperInBackground={(paperId, title) =>
              openPaperInBackground(paperId, truncateTitle(title, 200))
            }
          />
        ) : (
          <ChatWindow messages={messages} onSend={handleSend} />
        )}
        <Sidebar />
      </main>
    </div>
  )
}
