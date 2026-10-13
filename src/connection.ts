import { addEventListener, removeEventListener, toaster, useQuickAccessVisible } from '@decky/api'
import { useEffect, useState } from 'react'

import { BACKEND_STATUS_EVENT, type BackendStatus, call, onEvent } from './backend'
import type { ConnectionStatus } from './rpc/schema.gen'

/** Path of the full-screen route for setting up the server and signing in. */
export const SETUP_ROUTE = '/emudeck-romm/setup'

type Listener = (status: ConnectionStatus | null) => void

let current: ConnectionStatus | null = null
const listeners = new Set<Listener>()

/** Shares a status the setup route just got, so the QAM shows it without asking again. */
export function publishConnection(status: ConnectionStatus) {
  current = status
  for (const fn of listeners) fn(status)
}

/** The last connection status anyone fetched, or null before the first. */
export function currentConnection(): ConnectionStatus | null {
  return current
}

/**
 * Applies each connection change the backend announces, wherever the user is: a sign-in that
 * finished after its setup page closed, or a lapsed token, which toasts. The backend announces a
 * lapse once, not on every check after it. Returns a function that stops listening.
 */
export function watchConnection(): () => void {
  return onEvent('connection', (status) => {
    publishConnection(status)
    if (status.state !== 'signed_out') return
    toaster.toast({
      title: 'Signed out of RomM',
      body: 'Work with RomM is paused. Open the plugin to sign in again.',
    })
  })
}

async function refresh() {
  try {
    publishConnection(await call('connection_status'))
  } catch (_e) {
    // The backend is down or restarting; the backend status section says so.
  }
}

/**
 * How often an open Quick Access Menu re-checks the connection, so a token revoked or a server
 * gone down while it stays open shows without closing and reopening it.
 */
const REFRESH_INTERVAL_MS = 30_000

/**
 * The connection status, re-checked each time the Quick Access Menu opens, every
 * REFRESH_INTERVAL_MS while it stays open, and each time the backend becomes ready, since a check
 * made while it was starting got no answer.
 */
export function useConnection(): ConnectionStatus | null {
  const [status, setStatus] = useState(current)
  const visible = useQuickAccessVisible()

  useEffect(() => {
    listeners.add(setStatus)
    const listener = addEventListener<[BackendStatus]>(BACKEND_STATUS_EVENT, (backend) => {
      if (backend.state === 'ready') void refresh()
    })
    return () => {
      listeners.delete(setStatus)
      removeEventListener(BACKEND_STATUS_EVENT, listener)
    }
  }, [])

  useEffect(() => {
    if (!visible) return
    void refresh()
    const timer = setInterval(() => void refresh(), REFRESH_INTERVAL_MS)
    return () => clearInterval(timer)
  }, [visible])

  return status
}
