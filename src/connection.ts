import { useQuickAccessVisible } from '@decky/api'
import { useEffect, useState } from 'react'

import { call } from './backend'
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

async function refresh() {
  try {
    publishConnection(await call('connection_status'))
  } catch (_e) {
    // The backend is down or restarting; the backend status section says so.
  }
}

/** The connection status, re-checked each time the Quick Access Menu opens. */
export function useConnection(): ConnectionStatus | null {
  const [status, setStatus] = useState(current)
  const visible = useQuickAccessVisible()

  useEffect(() => {
    listeners.add(setStatus)
    return () => {
      listeners.delete(setStatus)
    }
  }, [])

  useEffect(() => {
    if (visible) void refresh()
  }, [visible])

  return status
}
