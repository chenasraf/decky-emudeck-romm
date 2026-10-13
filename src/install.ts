import { addEventListener, removeEventListener, useQuickAccessVisible } from '@decky/api'
import { useEffect, useState } from 'react'

import { BACKEND_STATUS_EVENT, type BackendStatus, call } from './backend'
import type { InstallState } from './rpc/schema.gen'

/** Path of the full-screen route that lists every install check and its fix. */
export const STATUS_ROUTE = '/emudeck-romm/status'

type Listener = (state: InstallState | null) => void

let current: InstallState | null = null
const listeners = new Set<Listener>()

function publish(state: InstallState) {
  current = state
  for (const fn of listeners) fn(state)
}

/**
 * Asks the backend what is installed. The backend looks at the disk at most once a minute unless
 * refresh is set, as Check again does.
 */
export async function loadInstallState(refresh = false): Promise<InstallState | null> {
  try {
    const state = await call('install_state', { refresh })
    publish(state)
    return state
  } catch (_e) {
    // The backend is down or restarting; the backend status section says so.
    return null
  }
}

/**
 * What the plugin found of EmuDeck and ES-DE, loaded each time the Quick Access Menu opens and each
 * time the backend becomes ready.
 */
export function useInstallState(): InstallState | null {
  const [state, setState] = useState(current)
  const visible = useQuickAccessVisible()

  useEffect(() => {
    listeners.add(setState)
    const listener = addEventListener<[BackendStatus]>(BACKEND_STATUS_EVENT, (backend) => {
      if (backend.state === 'ready') void loadInstallState()
    })
    return () => {
      listeners.delete(setState)
      removeEventListener(BACKEND_STATUS_EVENT, listener)
    }
  }, [])

  useEffect(() => {
    if (visible) void loadInstallState()
  }, [visible])

  return state
}
