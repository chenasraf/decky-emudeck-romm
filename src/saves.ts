import { toaster } from '@decky/api'
import { useEffect, useState, useSyncExternalStore } from 'react'

import { call, onEvent } from './backend'
import type {
  SaveActivityEntry,
  SaveBackup,
  SaveCounts,
  SaveStatus,
  SaveSummary,
  SaveSyncEvent,
} from './rpc/schema.gen'

/** Goes up each time a save sync starts or ends, so views showing save status reload. */
let changes = 0
const listeners = new Set<() => void>()

function games(n: number): string {
  return n === 1 ? '1 game' : `${n} games`
}

/** What a sync did, as a sentence, e.g. "1 uploaded, 2 downloaded." */
export function syncOutcome(c: SaveCounts): string {
  const parts = []
  if (c.uploaded > 0) parts.push(`${c.uploaded} uploaded`)
  if (c.downloaded > 0) parts.push(`${c.downloaded} downloaded`)
  if (c.waiting > 0) parts.push(`${c.waiting} waiting for an emulator to close`)
  if (c.failed > 0) parts.push(`${c.failed} failed`)
  return parts.length > 0 ? `${parts.join(', ')}.` : 'Everything is up to date.'
}

function apply(e: SaveSyncEvent) {
  changes++
  for (const fn of listeners) fn()
  if (e.syncing) return
  // Syncs that run on their own speak up only when they changed a save on this Deck or need the user.
  if (e.auto) {
    if (e.counts.conflicts > 0) {
      toaster.toast({
        title: 'Save conflict',
        body: `${games(e.counts.conflicts)} changed both on this Deck and on RomM. Nothing was overwritten.`,
      })
    } else if (e.counts.downloaded > 0) {
      toaster.toast({
        title: 'Saves downloaded',
        body: `Newer saves from RomM for ${games(e.counts.downloaded)}.`,
      })
    }
    return
  }
  if (e.problem) {
    toaster.toast({ title: "Couldn't sync saves", body: e.problem.message })
  } else if (e.counts.conflicts > 0) {
    toaster.toast({
      title: 'Save conflict',
      body: `${games(e.counts.conflicts)} changed both on this Deck and on RomM. Nothing was overwritten.`,
    })
  } else {
    toaster.toast({ title: 'Saves synced', body: syncOutcome(e.counts) })
  }
}

/** Toasts each save sync's outcome. Returns a function that stops listening. */
export function watchSaves(): () => void {
  return onEvent('save_sync', apply)
}

/** Reloads every view of save status. */
export function refreshSaves() {
  changes++
  for (const fn of listeners) fn()
}

function subscribe(fn: () => void) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/** Loads with fetch, again each time a save sync starts or ends. */
function useReloading<T>(fetch: () => Promise<T>, deps: unknown[]): T | null {
  const version = useSyncExternalStore(subscribe, () => changes)
  const [value, setValue] = useState<T | null>(null)
  useEffect(() => {
    let current = true
    fetch().then(
      (v) => {
        if (current) setValue(v)
      },
      () => {
        if (current) setValue(null)
      },
    )
    return () => {
      current = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [version, ...deps])
  return value
}

/** Where a game's save sync stands. */
export function useSaveStatus(id: number): SaveStatus | null {
  return useReloading(() => call('save_status', { id }), [id])
}

/** The newest save activity, of the game with id or of every game when id is 0. */
export function useSaveActivity(id: number, limit: number): SaveActivityEntry[] | null {
  return useReloading(
    () => call('save_activity', { id, limit }).then((r) => r.entries),
    [id, limit],
  )
}

/** A game's save backups, newest first. */
export function useSaveBackups(id: number): SaveBackup[] | null {
  return useReloading(() => call('save_backups', { id }).then((r) => r.backups), [id])
}

/** Where save sync stands across games. */
export function useSaveSummary(): SaveSummary | null {
  return useReloading(() => call('save_summary'), [])
}

/** A save's state as the line the game page shows. */
export function saveStateLabel(s: SaveStatus): string {
  if (s.syncing) return 'Syncing…'
  switch (s.state) {
    case 'synced':
      return 'Synced'
    case 'conflict':
      return '⚠ Conflict'
    case 'removed_on_server':
      return 'Removed on RomM'
    case 'waiting':
      return `Waiting for ${s.saver ?? 'the emulator'} to close`
    case 'failed':
      return '⚠ Sync failed'
    default:
      return 'Not synced yet'
  }
}

/** What a save's state means, for the game page. */
export function saveStateDetail(s: SaveStatus): string | undefined {
  switch (s.state) {
    case 'conflict':
      return s.conflict && s.conflict.serverSaveId > 0
        ? 'The save changed both on this Deck and on RomM since they last matched. Neither was overwritten: choose which to keep.'
        : 'The save changed both on this Deck and on RomM since they last matched. Neither was overwritten. Sync again to compare the two.'
    case 'removed_on_server':
      return "RomM's save of this game was deleted. The save on this Deck stays."
    case 'waiting':
      return `RomM has a newer save. It downloads once ${s.saver ?? 'the emulator'} closes: an emulator writes the save it holds when a game closes, which would undo the download.`
    case 'failed':
      return s.problem
    default:
      return undefined
  }
}
