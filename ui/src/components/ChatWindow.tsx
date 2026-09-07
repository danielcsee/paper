import { useEffect, useRef, useState } from 'react'
import RagResults from './RagResults'
import type { Message } from '../types'

const EXAMPLES = [
  'What is known about BRCA1 and DNA repair?',
  'Which findings about osteoporosis and quality of life conflict?',
  'Summarise the evidence linking calcium intake to fracture risk.',
]

/** Must match the .landing-exit transition in styles.css. */
const LANDING_EXIT_MS = 320

interface Props {
  messages: Message[]
  onSend: (text: string) => void
  onOpenPaper: (paperId: number, title: string | null) => void
}

export default function ChatWindow({ messages, onSend, onOpenPaper }: Props) {
  const [draft, setDraft] = useState('')
  // The landing block outlives the first question: it has to animate away
  // before the answer appears, rather than vanishing the instant state changes.
  const [landingVisible, setLandingVisible] = useState(messages.length === 0)
  const [landingLeaving, setLandingLeaving] = useState(false)
  const endRef = useRef<HTMLDivElement>(null)

  const asked = messages.length > 0

  useEffect(() => {
    if (!asked || !landingVisible) return
    setLandingLeaving(true)
    const timer = window.setTimeout(() => {
      setLandingVisible(false)
      setLandingLeaving(false)
    }, LANDING_EXIT_MS)
    return () => window.clearTimeout(timer)
  }, [asked, landingVisible])

  useEffect(() => {
    if (landingVisible) return
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, landingVisible])

  function submit(text: string) {
    const trimmed = text.trim()
    if (!trimmed) return
    onSend(trimmed)
    setDraft('')
  }

  return (
    <section className="chat" aria-label="Chat">
      <div className="chat-scroll">
        {landingVisible && (
          <div className={`landing${landingLeaving ? ' landing-exit' : ''}`}>
            <h1 className="landing-title">Ask the literature a question</h1>
            <p className="landing-sub">
              Answers are grounded in the papers you have imported, ranked by
              how strongly their passages match your question.
            </p>
            <ul className="examples">
              {EXAMPLES.map((example) => (
                <li key={example}>
                  <button
                    className="example"
                    onClick={() => submit(example)}
                    disabled={landingLeaving}
                  >
                    {example}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}

        {/* Held back until the landing block has finished leaving, so the two
            never occupy the same space. */}
        {!landingVisible && (
          <ol className="messages">
            {messages.map((message) => (
              <li key={message.id} className={`message message-${message.role}`}>
                <div className="message-role">
                  {message.role === 'user' ? 'You' : 'sciterm'}
                </div>
                <div className="message-body">
                  {message.status === 'pending' ? (
                    <span className="rag-pending" role="status">
                      <span className="spinner" aria-hidden="true" />
                      <span>{message.text}</span>
                    </span>
                  ) : (
                    <>
                      {message.text && <p className="rag-text">{message.text}</p>}
                      {message.results && (
                        <RagResults
                          papers={message.results}
                          chunksConsidered={message.chunksConsidered ?? 0}
                          onOpenPaper={onOpenPaper}
                        />
                      )}
                    </>
                  )}
                </div>
              </li>
            ))}
            <div ref={endRef} />
          </ol>
        )}
      </div>

      <form
        className="composer"
        onSubmit={(event) => {
          event.preventDefault()
          submit(draft)
        }}
      >
        <textarea
          className="composer-input"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault()
              submit(draft)
            }
          }}
          rows={1}
          placeholder="Ask a question about your corpus…"
          aria-label="Message"
        />
        <button className="composer-send" type="submit" disabled={!draft.trim()}>
          Send
        </button>
      </form>
    </section>
  )
}
