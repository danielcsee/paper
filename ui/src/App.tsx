import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, ragSearch, type PaperDetail } from './api'
import ChatWindow from './components/ChatWindow'
import CorpusView from './components/CorpusView'
import PaperTabs from './components/PaperTabs'
import PaperView from './components/PaperView'
import PaperExplorer, { type ReferenceTarget } from './components/PaperExplorer'
import {
  CHAT,
  CORPUS,
  loadTabs,
  pathToView,
  sameView,
  saveTabs,
  TAB_FLASH_MS,
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

  // Tabs briefly showing the selected styling after being opened in the
  // background, so the reader gets feedback for a tab they are not looking at.
  const [flashing, setFlashing] = useState<ReadonlySet<number>>(new Set())
  const flashTimers = useRef<Map<number, number>>(new Map())

  // Papers that 404ed this session. Their tab stays so the reader sees why,
  // but it must not come back after a reload.
  const [missing, setMissing] = useState<ReadonlySet<number>>(new Set())
  const [view, setView] = useState<View>(() => pathToView(window.location.pathname))
  // Which paper's references the side panel is showing, if any.
  const [referencesFor, setReferencesFor] = useState<ReferenceTarget | null>(null)

  useEffect(() => {
    saveTabs(tabs.filter((tab) => !missing.has(tab.paperId)))
  }, [tabs, missing])

  // Pending flashes must not fire into an unmounted tree.
  useEffect(() => {
    const timers = flashTimers.current
    return () => {
      timers.forEach((timer) => window.clearTimeout(timer))
      timers.clear()
    }
  }, [])

  const flashTab = useCallback((paperId: number) => {
    const timers = flashTimers.current
    // Clicking the same icon again restarts the flash rather than letting the
    // first timer cut the second one short.
    const running = timers.get(paperId)
    if (running !== undefined) window.clearTimeout(running)

    setFlashing((prev) => (prev.has(paperId) ? prev : new Set(prev).add(paperId)))
    timers.set(
      paperId,
      window.setTimeout(() => {
        timers.delete(paperId)
        setFlashing((prev) => {
          if (!prev.has(paperId)) return prev
          const next = new Set(prev)
          next.delete(paperId)
          return next
        })
      }, TAB_FLASH_MS),
    )
  }, [])

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
    flashTab(paperId)
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

  /**
   * Ask the corpus. Retrieval only — the backend runs no LLM, so the answer is
   * the ranked evidence rather than prose.
   *
   * The assistant message is appended immediately in a pending state and then
   * filled in, so the question and a spinner appear at once instead of the
   * user staring at their own message alone.
   */
  async function handleSend(text: string) {
    const answerId = crypto.randomUUID()
    setMessages((prev) => [
      ...prev,
      { id: crypto.randomUUID(), role: 'user', text },
      { id: answerId, role: 'assistant', text: 'Searching your corpus…', status: 'pending' },
    ])

    const replace = (patch: Partial<Message>) =>
      setMessages((prev) =>
        prev.map((message) =>
          message.id === answerId ? { ...message, ...patch } : message,
        ),
      )

    try {
      const response = await ragSearch(text)
      replace({
        status: 'done',
        text: '',
        results: response.papers,
        chunksConsidered: response.chunks_considered,
      })
    } catch (err) {
      replace({
        status: 'error',
        text:
          err instanceof ApiError
            ? err.message
            : 'Could not reach the search service.',
        results: undefined,
      })
    }
  }

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true" />
          <span className="brand-name">sciterm</span>
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
          flashing={flashing}
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
            onViewReferences={(paperId, title) => setReferencesFor({ paperId, title })}
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
          <ChatWindow
            messages={messages}
            onSend={(text) => void handleSend(text)}
            onOpenPaper={(paperId, title) => openPaper(paperId, truncateTitle(title, 200))}
          />
        )}
        <PaperExplorer
          referencesFor={referencesFor}
          onCloseReferences={() => setReferencesFor(null)}
        />
      </main>
    </div>
  )
}
