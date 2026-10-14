import { addEventListener, callable, removeEventListener } from '@decky/api'

import type { Error as RpcErrorObject, Events, Methods } from './rpc/schema.gen'

export type BackendState = 'starting' | 'ready' | 'restarting' | 'down' | 'refused' | 'stopped'

export interface BackendStatus {
  state: BackendState
  version: string | null
  reason: string | null
}

/** Event main.py emits whenever the backend's state changes. */
export const BACKEND_STATUS_EVENT = 'backend_status'

type Params<M extends keyof Methods> = Methods[M]['params']
type Result<M extends keyof Methods> = Methods[M]['result']

const relay = callable<[method: string, params?: unknown], unknown>('call')

export const backendStatus = callable<[], BackendStatus>('backend_status')
export const restartBackend = callable<[], void>('restart_backend')
/** Stops the backend, deletes everything the plugin knows (never games or saves), and restarts. */
export const resetPluginData = callable<[], void>('reset_plugin_data')

/** Calls a backend method through main.py's relay. */
export async function call<M extends keyof Methods>(
  method: M,
  params?: Params<M>,
): Promise<Result<M>> {
  return (await relay(method, params)) as Result<M>
}

/** Listens for a backend event; returns a function that stops listening. */
export function onEvent<E extends keyof Events>(
  event: E,
  fn: (params: Events[E]) => void,
): () => void {
  const listener = addEventListener<[Events[E]]>(event, fn)
  return () => {
    removeEventListener(event, listener)
  }
}

/**
 * Reads the JSON-RPC error object main.py puts in a rejected call's message, or null when the
 * call failed for another reason (backend down, timeout).
 */
export function rpcError(err: unknown): RpcErrorObject | null {
  if (!(err instanceof Error)) return null
  try {
    const parsed: unknown = JSON.parse(err.message)
    if (parsed && typeof parsed === 'object' && 'code' in parsed && 'message' in parsed) {
      return parsed as RpcErrorObject
    }
  } catch (_e) {
    // Not an RPC error.
  }
  return null
}
