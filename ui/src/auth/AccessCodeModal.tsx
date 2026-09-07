import { useEffect, useRef, useState } from 'react'
import { redeemCode } from './session'

interface Props {
  /** A code was accepted; the caller resumes whatever was gated. */
  onUnlocked: () => void
  onClose: () => void
}

/**
 * The access-code prompt.
 *
 * A native `<dialog>` rather than a hand-rolled overlay: the browser supplies
 * the focus trap, the inert background and Escape-to-close, all of which are
 * easy to get subtly wrong by hand.
 */
export default function AccessCodeModal({ onUnlocked, onClose }: Props) {
  const ref = useRef<HTMLDialogElement>(null)
  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    // showModal(), not the `open` attribute: only the modal form gets the
    // top layer, the backdrop and the focus trap.
    if (!dialog.open) dialog.showModal()
    const handleCancel = (event: Event) => {
      event.preventDefault()
      onClose()
    }
    dialog.addEventListener('cancel', handleCancel)
    return () => dialog.removeEventListener('cancel', handleCancel)
  }, [onClose])

  async function attempt() {
    const trimmed = code.trim()
    if (!trimmed || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      await redeemCode(trimmed)
      onUnlocked()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not check that code.')
      setSubmitting(false)
    }
  }

  return (
    <dialog className="code-dialog" ref={ref} aria-labelledby="code-dialog-title">
      <form
        className="code-form"
        onSubmit={(event) => {
          event.preventDefault()
          void attempt()
        }}
      >
        <h2 className="code-title" id="code-dialog-title">
          Enter your access code
        </h2>
        <p className="code-intro">
          Reading the corpus is open to everyone. Importing papers and asking
          questions need a code, because they call out to PubTator and burn
          real compute.
        </p>

        <label className="code-label" htmlFor="access-code">
          Access code
        </label>
        <input
          className="code-input"
          id="access-code"
          name="access-code"
          value={code}
          onChange={(event) => setCode(event.target.value)}
          onKeyDown={(event) => {
            // Explicit, because implicit form submission does not fire for
            // this input inside the modal <dialog> — verified in the browser:
            // without this, Enter does nothing at all. Pasting a code and
            // pressing Enter is the whole interaction, so it cannot be left to
            // a behaviour that is not actually happening.
            if (event.key !== 'Enter') return
            event.preventDefault()
            void attempt()
          }}
          placeholder="paste your code"
          autoComplete="off"
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          // The modal is opened by an explicit click, so taking focus is what
          // the visitor is already expecting.
          autoFocus
          disabled={submitting}
          aria-invalid={error !== null}
          aria-describedby={error ? 'code-error' : undefined}
        />

        {error && (
          <p className="code-error" id="code-error" role="alert">
            {error}
          </p>
        )}

        <p className="code-note">
          A code lasts 48 hours from the first time it is used, on one device at
          a time.
        </p>

        <div className="code-actions">
          <button
            className="code-cancel"
            type="button"
            onClick={onClose}
            disabled={submitting}
          >
            Not now
          </button>
          <button className="code-submit" type="submit" disabled={!code.trim() || submitting}>
            {submitting ? 'Checking…' : 'Unlock'}
          </button>
        </div>
      </form>
    </dialog>
  )
}
