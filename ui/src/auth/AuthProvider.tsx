import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from 'react'
import AccessCodeModal from './AccessCodeModal'
import {
  getSnapshot,
  loadSession,
  logout as endSession,
  subscribe,
  type SessionState,
  type UserInfo,
} from './session'

interface AuthValue extends SessionState {
  /** True when the visitor may use the metered features. */
  unlocked: boolean
  /**
   * Gate an action. Runs it and returns true when unlocked; otherwise opens
   * the modal, remembers the action, and returns false. Whatever was gated
   * runs by itself once a code is accepted, so a click is never simply lost.
   */
  requireAuth: (action?: () => void) => boolean
  /** Open the modal with nothing pending — the toolbar button. */
  promptForCode: () => void
  logout: () => void
  user: UserInfo | null
}

const AuthContext = createContext<AuthValue | null>(null)

// Kicked off at module load rather than in an effect: the request is in flight
// while React is still mounting, so the toolbar settles a frame sooner.
void loadSession()

export function AuthProvider({ children }: { children: ReactNode }) {
  const state = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
  const [modalOpen, setModalOpen] = useState(false)
  const pending = useRef<(() => void) | null>(null)

  const unlocked = !state.authRequired || state.authenticated

  const requireAuth = useCallback(
    (action?: () => void) => {
      if (!state.authRequired || state.authenticated) {
        action?.()
        return true
      }
      pending.current = action ?? null
      setModalOpen(true)
      return false
    },
    [state.authRequired, state.authenticated],
  )

  const promptForCode = useCallback(() => {
    pending.current = null
    setModalOpen(true)
  }, [])

  const handleUnlocked = useCallback(() => {
    setModalOpen(false)
    const action = pending.current
    pending.current = null
    // Let the newly unlocked tree render before the resumed action fires into
    // it — the search panel and composer are still read-only this tick.
    if (action) window.setTimeout(action, 0)
  }, [])

  const handleClose = useCallback(() => {
    pending.current = null
    setModalOpen(false)
  }, [])

  const value = useMemo<AuthValue>(
    () => ({ ...state, unlocked, requireAuth, promptForCode, logout: () => void endSession() }),
    [state, unlocked, requireAuth, promptForCode],
  )

  return (
    <AuthContext.Provider value={value}>
      {children}
      {modalOpen && <AccessCodeModal onUnlocked={handleUnlocked} onClose={handleClose} />}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthValue {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used inside <AuthProvider>')
  return value
}
