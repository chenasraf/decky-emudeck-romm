import { toaster } from '@decky/api'
import { useEffect, useState, useSyncExternalStore } from 'react'

import { call, onEvent } from './backend'
import type { ESDENotice, ESDERescanEvent } from './rpc/schema.gen'

/** Goes up with each batch ES-DE needs a rescan to see, so the QAM note reloads. */
let changes = 0
const listeners = new Set<() => void>()

function games(n: number, kind: string): string {
  return n === 1 ? `1 ${kind} game` : `${n} ${kind} games`
}

/** What ES-DE needs a rescan for, as the end of a sentence: "see 2 new games". */
export function rescanFor(n: ESDENotice): string {
  const parts = []
  if (n.added > 0) parts.push(`see ${games(n.added, 'new')}`)
  if (n.removed > 0) parts.push(`drop ${games(n.removed, 'deleted')}`)
  return parts.join(' and ')
}

function apply(e: ESDERescanEvent) {
  changes++
  for (const fn of listeners) fn()
  toaster.toast({
    title: 'ES-DE is open',
    body: `Run Utilities → Rescan ROM directory to ${rescanFor(e.batch)}.`,
  })
}

/** Toasts each batch of changes ES-DE needs a rescan to see. Returns a function that stops listening. */
export function watchESDE(): () => void {
  return onEvent('esde_rescan', apply)
}

function subscribe(fn: () => void) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/** What ES-DE hasn't seen yet, null while there's nothing, asked again after each batch. */
export function useESDENotice(): ESDENotice | null {
  const version = useSyncExternalStore(subscribe, () => changes)
  const [notice, setNotice] = useState<ESDENotice | null>(null)
  useEffect(() => {
    let current = true
    call('esde_notice').then(
      (n) => {
        if (current) setNotice(n.added + n.removed > 0 ? n : null)
      },
      () => {
        if (current) setNotice(null)
      },
    )
    return () => {
      current = false
    }
  }, [version])
  return notice
}
