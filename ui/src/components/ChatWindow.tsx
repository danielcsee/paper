import { useEffect, useRef, useState } from 'react'
import type { Message } from '../types'

const EXAMPLES = [
  'What is known about BRCA1 and DNA repair?',
  'Which findings about osteoporosis and quality of life conflict?',
  'Summarise the evidence linking calcium intake to fracture risk.',
]

interface Props {
  messages: Message[]
  onSend: (text: string) => void
}

export default function ChatWindow({ messages, onSend }: Props) {
  const [draft, setDraft] = useState('')
  const endRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  function submit(text: string) {
    const trimmed = text.trim()
    if (!trimmed) return
    onSend(trimmed)
    setDraft('')
  }

  return (
    <section className="chat" aria-label="Chat">
      <div className="chat-scroll">
        {messages.length === 0 ? (
          <div className="landing">
            <h1 className="landing-title">Ask the literature a question</h1>
            <p className="landing-sub">
              Answers are grounded in open-access papers and ranked by how the
              knowledge graph connects them.
            </p>
            <ul className="examples">
              {EXAMPLES.map((example) => (
                <li key={example}>
                  <button className="example" onClick={() => submit(example)}>
                    {example}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ) : (
          <ol className="messages">
            {messages.map((message) => (
              <li key={message.id} className={`message message-${message.role}`}>
                <div className="message-role">
                  {message.role === 'user' ? 'You' : 'litgraph'}
                </div>
                <div className="message-body">{message.text}</div>
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
          placeholder="Ask a question about the corpus…"
          aria-label="Message"
        />
        <button className="composer-send" type="submit" disabled={!draft.trim()}>
          Send
        </button>
      </form>
    </section>
  )
}
