import { useEffect, useRef, useState } from 'react'
import { login, redeemCode } from './session'

interface Props {
  /** A code or password was accepted; the caller resumes whatever was gated. */
  onUnlocked: () => void
  onClose: () => void
}

/**
 * The unlock prompt: an access code, or an account.
 *
 * A native `<dialog>` rather than a hand-rolled overlay: the browser supplies
 * the focus trap, the inert background and Escape-to-close, all of which are
 * easy to get subtly wrong by hand.
 *
 * The code field comes first because it is what almost every visitor has. The
 * account form is for the one admin, whose password is set — and rotated — by
 * `scripts/admin.sh rotate-admin`.
 */
export default function AccessCodeModal({ onUnlocked, onClose }: Props) {
  const ref = useRef<HTMLDialogElement>(null)
  const [code, setCode] = useState('')
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [showAccount, setShowAccount] = useState(false)
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

  /** One submit path for both credentials, so the busy and error states can
      never disagree about which attempt is in flight. */
  async function attempt() {
    if (submitting) return
    const trimmedCode = code.trim()
    const trimmedUser = username.trim()

    if (!showAccount && !trimmedCode) return
    if (showAccount && (!trimmedUser || !password)) return

    setSubmitting(true)
    setError(null)
    try {
      if (showAccount) await login(trimmedUser, password)
      else await redeemCode(trimmedCode)
      onUnlocked()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not check that.')
      setSubmitting(false)
    }
  }

  /**
   * Enter submits, explicitly.
   *
   * Implicit form submission does not fire for these inputs inside the modal
   * <dialog> — verified in the browser: without this, Enter does nothing at
   * all. Typing a credential and pressing Enter is the whole interaction, so
   * it cannot be left to a behaviour that is not actually happening.
   */
  function onEnter(event: React.KeyboardEvent) {
    if (event.key !== 'Enter') return
    event.preventDefault()
    void attempt()
  }

  const submitDisabled = submitting || (showAccount ? !username.trim() || !password : !code.trim())

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
          {showAccount ? 'Sign in' : 'Enter your access code'}
        </h2>
        <p className="code-intro">
          Reading the corpus is open to everyone. Importing papers and asking
          questions need a code, because they call out to PubTator and burn
          real compute.
        </p>

        {showAccount ? (
          <>
            <label className="code-label" htmlFor="account-username">
              Username
            </label>
            <input
              className="code-input"
              id="account-username"
              name="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              onKeyDown={onEnter}
              autoComplete="username"
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              disabled={submitting}
            />
            <label className="code-label code-label-spaced" htmlFor="account-password">
              Password
            </label>
            <input
              className="code-input"
              id="account-password"
              name="password"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              onKeyDown={onEnter}
              autoComplete="current-password"
              autoFocus
              disabled={submitting}
              aria-invalid={error !== null}
              aria-describedby={error ? 'code-error' : undefined}
            />
          </>
        ) : (
          <>
            <label className="code-label" htmlFor="access-code">
              Access code
            </label>
            <input
              className="code-input"
              id="access-code"
              name="access-code"
              value={code}
              onChange={(event) => setCode(event.target.value)}
              onKeyDown={onEnter}
              placeholder="paste your code"
              autoComplete="off"
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              // The modal is opened by an explicit click, so taking focus is
              // what the visitor is already expecting.
              autoFocus
              disabled={submitting}
              aria-invalid={error !== null}
              aria-describedby={error ? 'code-error' : undefined}
            />
          </>
        )}

        {error && (
          <p className="code-error" id="code-error" role="alert">
            {error}
          </p>
        )}

        <p className="code-note">
          {showAccount
            ? 'For the site owner. Everyone else uses an access code.'
            : 'A code lasts 48 hours from the first time it is used, on one device at a time.'}
        </p>

        <button
          className="code-switch"
          type="button"
          onClick={() => {
            // Errors belong to the form that produced them.
            setError(null)
            setShowAccount((previous) => !previous)
          }}
          disabled={submitting}
        >
          {showAccount ? 'Use an access code instead' : 'Sign in with a username and password'}
        </button>

        <div className="code-actions">
          <button
            className="code-cancel"
            type="button"
            onClick={onClose}
            disabled={submitting}
          >
            Not now
          </button>
          <button className="code-submit" type="submit" disabled={submitDisabled}>
            {submitting ? 'Checking…' : showAccount ? 'Sign in' : 'Unlock'}
          </button>
        </div>
      </form>
    </dialog>
  )
}
