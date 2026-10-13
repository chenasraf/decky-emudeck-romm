import { addEventListener, removeEventListener, toaster } from '@decky/api'
import { useSyncExternalStore } from 'react'

import { BACKEND_STATUS_EVENT, type BackendStatus, call, onEvent } from './backend'
import type { DownloadEvent, DownloadItem, Error as RpcError } from './rpc/schema.gen'

export interface Downloads {
  /** The queued and running downloads, in queue order. */
  items: DownloadItem[]
  /** Why downloading is off, null when it works. */
  problem: RpcError | null
  /** Goes up each time a download ends, so views listing game states know to reload. */
  ended: number
  /** Why each game's last download failed, until it's queued again. */
  failures: Record<number, RpcError>
}

let current: Downloads = { items: [], problem: null, ended: 0, failures: {} }
const listeners = new Set<() => void>()

function publish(next: Downloads) {
  current = next
  for (const fn of listeners) fn()
}

async function load() {
  try {
    const r = await call('list_downloads')
    publish({ ...current, items: r.downloads, problem: r.problem ?? null })
  } catch (_e) {
    // The backend is down or restarting; the backend status section says so.
  }
}

function apply(e: DownloadEvent) {
  const { problem, ...item } = e
  const failures = { ...current.failures }
  if (e.state === 'queued' || e.state === 'downloading') {
    delete failures[e.id]
    const items = current.items.some((d) => d.id === e.id)
      ? current.items.map((d) => (d.id === e.id ? item : d))
      : [...current.items, item]
    publish({ ...current, items, failures })
    return
  }
  if (problem) failures[e.id] = problem
  publish({
    ...current,
    items: current.items.filter((d) => d.id !== e.id),
    ended: current.ended + 1,
    failures,
  })
  // A download can end long after the QAM closed.
  if (e.state === 'installed') {
    toaster.toast({ title: 'Installed', body: e.name })
  } else if (problem) {
    toaster.toast({ title: `Couldn't download ${e.name}`, body: problem.message })
  }
}

/**
 * Keeps the download queue current from the backend's download events, and reloads it each time
 * the backend becomes ready. Returns a function that stops listening.
 */
export function watchDownloads(): () => void {
  void load()
  const stop = onEvent('download', apply)
  const listener = addEventListener<[BackendStatus]>(BACKEND_STATUS_EVENT, (backend) => {
    if (backend.state === 'ready') void load()
  })
  return () => {
    stop()
    removeEventListener(BACKEND_STATUS_EVENT, listener)
  }
}

function subscribe(fn: () => void) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export function useDownloads(): Downloads {
  return useSyncExternalStore(subscribe, () => current)
}

/** Progress of a download as a whole percentage. */
export function percent(d: { receivedBytes: number; totalBytes: number }): number {
  if (d.totalBytes <= 0) return 0
  return Math.min(100, Math.floor((d.receivedBytes / d.totalBytes) * 100))
}
