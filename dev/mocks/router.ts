// Shared by the @decky/api and @decky/ui mocks: routes the plugin registers, and the one the dev
// shell shows beside the QAM.

import { type ComponentType, useSyncExternalStore } from 'react'

export const routes = new Map<string, ComponentType>()

let path: string | null = null
const listeners = new Set<() => void>()

function notify() {
  for (const fn of listeners) fn()
}

export function navigate(to: string | null) {
  path = to
  notify()
}

export function addRoute(route: string, component: ComponentType) {
  routes.set(route, component)
  notify()
}

export function removeRoute(route: string) {
  routes.delete(route)
  notify()
}

export function useRoute(): string | null {
  return useSyncExternalStore(
    (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    () => path,
  )
}
