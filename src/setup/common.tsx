import type { CSSProperties, ReactNode } from 'react'

import { rpcError } from '../backend'
import type { Error as RpcError } from '../rpc/schema.gen'

/**
 * A full-screen route's scroll area: the screen below Steam's 40px header bar, scrolling inside
 * itself. A route that grows past the screen instead scrolls Steam's whole page, which carries the
 * button-hint footer up with the content.
 */
export const routeScroll: CSSProperties = {
  marginTop: 40,
  height: 'calc(100% - 40px)',
  overflowY: 'auto',
  scrollbarWidth: 'none',
  boxSizing: 'border-box',
}

/** A full-screen route's layout. */
export const page: CSSProperties = {
  ...routeScroll,
  padding: '16px 32px 48px',
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
  maxWidth: 720,
}

export const muted: CSSProperties = { color: '#8b929a', fontSize: 14, lineHeight: 1.5 }

export const mono: CSSProperties = {
  fontFamily: 'monospace',
  fontSize: 12,
  wordBreak: 'break-all',
}

const errorBox: CSSProperties = {
  background: 'rgba(209, 28, 28, 0.15)',
  borderLeft: '3px solid #d11c1c',
  padding: '10px 14px',
  fontSize: 14,
  lineHeight: 1.5,
}

export function ErrorBox({ children }: { children: ReactNode }) {
  return <div style={errorBox}>{children}</div>
}

/** Turns a rejected call into an RPC error the page can show. */
export function asRpcError(err: unknown): RpcError {
  return (
    rpcError(err) ?? {
      code: 0,
      message: err instanceof Error ? err.message : 'Something went wrong.',
    }
  )
}
