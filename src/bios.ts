import { toaster } from '@decky/api'
import { useEffect, useState, useSyncExternalStore } from 'react'

import { call, onEvent } from './backend'
import type { BIOSEvent, PlatformBIOS, Error as RpcError } from './rpc/schema.gen'
import { asRpcError } from './setup/common'

/** Goes up each time a BIOS download starts or ends, so views showing BIOS status reload. */
let changes = 0
const listeners = new Set<() => void>()

function files(n: number): string {
  return n === 1 ? '1 file' : `${n} files`
}

function apply(e: BIOSEvent) {
  changes++
  for (const fn of listeners) fn()
  if (!e.done) {
    // A download the user started from a button shows on the page they're on.
    if (e.auto) {
      toaster.toast({ title: 'Downloading BIOS', body: `${files(e.files)} for ${e.platformName}` })
    }
    return
  }
  if (e.problem) {
    toaster.toast({ title: `Couldn't download ${e.platformName} BIOS`, body: e.problem.message })
  } else {
    toaster.toast({ title: 'BIOS installed', body: `${files(e.downloaded)} for ${e.platformName}` })
  }
}

/** Toasts BIOS downloads starting and ending. Returns a function that stops listening. */
export function watchBios(): () => void {
  return onEvent('bios', apply)
}

function subscribe(fn: () => void) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/** Loads a platform's BIOS status, again each time a BIOS download starts or ends. */
export function usePlatformBios(platformId: number): {
  bios: PlatformBIOS | null
  error: RpcError | null
} {
  const version = useSyncExternalStore(subscribe, () => changes)
  const [bios, setBios] = useState<PlatformBIOS | null>(null)
  const [error, setError] = useState<RpcError | null>(null)
  useEffect(() => {
    let current = true
    call('platform_bios', { platformId }).then(
      (r) => {
        if (!current) return
        setBios(r)
        setError(null)
      },
      (err: unknown) => {
        if (current) setError(asRpcError(err))
      },
    )
    return () => {
      current = false
    }
  }, [platformId, version])
  return { bios, error }
}
