# ui/src/auth

The access-code gate. The app loads whole for everyone; this decides which
controls actually work.

## Files

**`session.ts`** — the token store, deliberately outside React. `api.ts` is a
plain module with no hooks, so the access token has to live somewhere both it
and the component tree can reach; the tree binds to it through
`useSyncExternalStore`. It also exports `authFetch`, which every call in
`api.ts` goes through: it attaches the bearer token and gives a 401 exactly one
repair attempt via `/auth/refresh` before clearing the session.

The access token is held **in memory only**. The refresh token is an HttpOnly
cookie the page cannot read, so a script injected into this app has nothing
durable to steal — and a reload recovers the session through `/auth/refresh`
anyway.

**`AuthProvider.tsx`** — context and `useAuth()`. The interesting member is
`requireAuth(action)`: it runs the action when unlocked, and otherwise opens
the modal, remembers it, and replays it once a code is accepted, so a click is
never simply lost.

**`AccessCodeModal.tsx`** — a native `<dialog>`, for the focus trap, the inert
background and Escape-to-close.

## Using the gate

Pass the gate the *work*, never the gated entry point itself:

```tsx
function send(text: string) { onSend(text) }        // the work
function submit(text: string) { requireAuth(() => send(text)) }
```

Handing `submit` to `requireAuth` recurses without end — unlocked,
`requireAuth` runs its action immediately, and that action gates itself again.
This was a real stack overflow, not a hypothetical one.

Locked controls are `readOnly`, not `disabled`: a disabled element fires no
click events, and the click is the whole point — it is what opens the modal.

## Dependencies

React only. `AuthProvider` wraps `<App />` in `main.tsx`.
