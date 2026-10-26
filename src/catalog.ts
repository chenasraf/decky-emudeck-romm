import { addEventListener, removeEventListener, useQuickAccessVisible } from '@decky/api'
import { Navigation } from '@decky/ui'
import { useEffect, useState } from 'react'

import { BACKEND_STATUS_EVENT, type BackendStatus, call, onEvent } from './backend'
import type { CatalogStatus } from './rpc/schema.gen'

/** Path of the full-screen library: shelves of games to browse with the gamepad. */
export const LIBRARY_ROUTE = '/emudeck-romm/library'

/** A game the library is asked to open, kept until the library mounts when none is open yet. */
let pendingGame: number | null = null
const gameListeners = new Set<(id: number) => void>()

/** Opens a game's page in the full-screen library. */
export function openGamePage(id: number) {
  if (gameListeners.size > 0) {
    for (const fn of gameListeners) fn(id)
  } else {
    pendingGame = id
  }
  Navigation.CloseSideMenus()
  Navigation.Navigate(LIBRARY_ROUTE)
}

/** Takes the game openGamePage asked a library not yet open to show. */
export function takePendingGame(): number | null {
  const id = pendingGame
  pendingGame = null
  return id
}

/** Hears openGamePage while the library is open. */
export function onOpenGame(fn: (id: number) => void): () => void {
  gameListeners.add(fn)
  return () => gameListeners.delete(fn)
}

type Listener = (status: CatalogStatus | null) => void

let current: CatalogStatus | null = null
const listeners = new Set<Listener>()

function publish(status: CatalogStatus) {
  current = status
  for (const fn of listeners) fn(status)
}

/**
 * Refreshes the library. Without force, the backend refreshes only when the library is over five
 * minutes old, so surfaces call this each time they open.
 */
export async function refreshLibrary(force = false) {
  try {
    publish(await call('refresh_catalog', { force }))
  } catch (_e) {
    // The backend is down or restarting; the backend status section says so.
  }
}

/**
 * The library's status: refreshed when it's stale each time the Quick Access Menu opens and each
 * time the backend becomes ready, and kept current by the backend's catalog events.
 */
export function useCatalog(): CatalogStatus | null {
  const [status, setStatus] = useState(current)
  const visible = useQuickAccessVisible()

  useEffect(() => {
    listeners.add(setStatus)
    const stop = onEvent('catalog', publish)
    const listener = addEventListener<[BackendStatus]>(BACKEND_STATUS_EVENT, (backend) => {
      if (backend.state === 'ready') void refreshLibrary()
    })
    return () => {
      listeners.delete(setStatus)
      stop()
      removeEventListener(BACKEND_STATUS_EVENT, listener)
    }
  }, [])

  useEffect(() => {
    if (visible) void refreshLibrary()
  }, [visible])

  return status
}

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB']

/** A size in bytes as a short label, e.g. "1.4 GB". */
export function formatSize(bytes: number): string {
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024
    unit++
  }
  return `${value < 10 && unit > 0 ? value.toFixed(1) : Math.round(value)} ${UNITS[unit]}`
}

/** How long ago an RFC 3339 time was, e.g. "5 min ago". */
export function timeAgo(iso: string, now = Date.now()): string {
  const seconds = Math.max(0, Math.round((now - Date.parse(iso)) / 1000))
  if (seconds < 60) return 'just now'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours} h ago`
  return `${Math.round(hours / 24)} d ago`
}
