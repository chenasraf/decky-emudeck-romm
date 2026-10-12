import type { CSSProperties, ReactNode } from 'react'

import { rpcError } from '../backend'
import type { Error as RpcError } from '../rpc/schema.gen'

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
