// Browser stand-in for @decky/api: callables go to the dev bridge, events come back over SSE.

import type { ComponentType } from 'react'

import { addRoute, removeRoute } from './router'

type Listener = (...args: never[]) => unknown

const listeners = new Map<string, Set<Listener>>()

new EventSource('/events').addEventListener('message', (e: MessageEvent<string>) => {
  const { event, args } = JSON.parse(e.data) as { event: string; args: never[] }
  for (const fn of listeners.get(event) ?? []) fn(...args)
})

export function callable<Args extends unknown[], Result>(
  name: string,
): (...args: Args) => Promise<Result> {
  return async (...args) => {
    const res = await fetch(`/api/${name}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ args }),
    })
    const data = (await res.json()) as { result?: Result; error?: string }
    if (data.error !== undefined) throw new Error(data.error)
    return data.result as Result
  }
}

export function addEventListener<Args extends unknown[]>(
  event: string,
  fn: (...args: Args) => unknown,
): (...args: Args) => unknown {
  if (!listeners.has(event)) listeners.set(event, new Set())
  listeners.get(event)!.add(fn as Listener)
  return fn
}

export function removeEventListener(event: string, fn: (...args: never[]) => unknown): void {
  listeners.get(event)?.delete(fn as Listener)
}

export function definePlugin<T>(fn: () => T): () => T {
  return fn
}

export const routerHook = {
  addRoute: (path: string, component: ComponentType) => addRoute(path, component),
  removeRoute: (path: string) => removeRoute(path),
}

/** The dev shell always shows the QAM. */
export function useQuickAccessVisible(): boolean {
  return true
}

export const toaster = {
  toast: (toast: { title?: unknown; body?: unknown }) => console.info('[toast]', toast),
}
