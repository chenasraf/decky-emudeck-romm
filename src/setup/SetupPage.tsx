import { DialogButton, Field, Focusable, Navigation, TextField } from '@decky/ui'
import { type CSSProperties, type ReactNode, useState } from 'react'

import { call, rpcError } from '../backend'
import { publishConnection } from '../connection'
import type { CheckServerResult, ConnectionStatus, Error as RpcError } from '../rpc/schema.gen'

type Step =
  | { name: 'server' }
  | { name: 'pair'; found: CheckServerResult }
  | { name: 'done'; status: ConnectionStatus }

// Routes render under Steam's header bar.
const page: CSSProperties = {
  padding: '56px 32px 32px',
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
  maxWidth: 720,
}

const muted: CSSProperties = { color: '#8b929a', fontSize: 14, lineHeight: 1.5 }

const errorBox: CSSProperties = {
  background: 'rgba(209, 28, 28, 0.15)',
  borderLeft: '3px solid #d11c1c',
  padding: '10px 14px',
  fontSize: 14,
  lineHeight: 1.5,
}

const mono: CSSProperties = { fontFamily: 'monospace', fontSize: 12, wordBreak: 'break-all' }

/** Turns a rejected call into an RPC error the page can show. */
function asRpcError(err: unknown): RpcError {
  return (
    rpcError(err) ?? {
      code: 0,
      message: err instanceof Error ? err.message : 'Something went wrong.',
    }
  )
}

function ErrorBox({ children }: { children: ReactNode }) {
  return <div style={errorBox}>{children}</div>
}

function ServerStep({ onFound }: { onFound: (found: CheckServerResult) => void }) {
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<RpcError | null>(null)

  const check = async (trustFingerprint?: string) => {
    setBusy(true)
    setError(null)
    try {
      onFound(await call('check_server', { url, trustFingerprint }))
    } catch (err) {
      setError(asRpcError(err))
    } finally {
      setBusy(false)
    }
  }

  const fingerprint = error?.data?.kind === 'untrusted_cert' ? error.data.fingerprint : undefined

  return (
    <>
      <div style={muted}>
        Enter the address you open RomM at in a browser. Without http:// or https://, the plugin
        tries HTTPS first, then HTTP.
      </div>
      <TextField
        label="Server address"
        value={url}
        disabled={busy}
        onChange={(e) => {
          setUrl(e.target.value)
          setError(null)
        }}
      />
      {error && (
        <ErrorBox>
          {error.message}
          {fingerprint && (
            <>
              <div style={{ marginTop: 8 }}>
                Trust it only if this matches the certificate your server uses:
              </div>
              <div style={{ ...mono, marginTop: 4 }}>SHA-256 {fingerprint}</div>
            </>
          )}
        </ErrorBox>
      )}
      <Focusable style={{ display: 'flex', gap: 12 }} flow-children="horizontal">
        {fingerprint ? (
          <DialogButton disabled={busy} onClick={() => void check(fingerprint)}>
            Trust this certificate
          </DialogButton>
        ) : (
          <DialogButton disabled={busy || url.trim() === ''} onClick={() => void check()}>
            {busy ? 'Checking…' : 'Continue'}
          </DialogButton>
        )}
      </Focusable>
    </>
  )
}

function PairStep({
  found,
  onBack,
  onSignedIn,
}: {
  found: CheckServerResult
  onBack: () => void
  onSignedIn: (status: ConnectionStatus) => void
}) {
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<RpcError | null>(null)

  const signIn = async () => {
    setBusy(true)
    setError(null)
    try {
      onSignedIn(await call('sign_in_with_pair_code', { server: found.server, code }))
    } catch (err) {
      setError(asRpcError(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Field label="Server" description={`RomM ${found.version}`}>
        {found.server.url}
      </Field>
      <div style={muted}>
        In RomM, open your profile and go to Client API Tokens. Create a token (or Regenerate an
        existing one) and choose Pair Device. Enter the 8-character code it shows before its
        countdown runs out.
      </div>
      <TextField
        label="Pair code"
        value={code}
        disabled={busy}
        onChange={(e) => {
          setCode(e.target.value)
          setError(null)
        }}
      />
      {error && <ErrorBox>{error.message}</ErrorBox>}
      <Focusable style={{ display: 'flex', gap: 12 }} flow-children="horizontal">
        <DialogButton disabled={busy} onClick={onBack}>
          Back
        </DialogButton>
        <DialogButton disabled={busy || code.trim() === ''} onClick={() => void signIn()}>
          {busy ? 'Signing in…' : 'Sign in'}
        </DialogButton>
      </Focusable>
    </>
  )
}

function DoneStep({ status }: { status: ConnectionStatus }) {
  return (
    <>
      <Field label="Signed in" description={status.version && `RomM ${status.version}`}>
        {status.serverUrl}
      </Field>
      <Focusable style={{ display: 'flex', gap: 12 }}>
        <DialogButton onClick={() => Navigation.NavigateBack()}>Done</DialogButton>
      </Focusable>
    </>
  )
}

export function SetupPage() {
  const [step, setStep] = useState<Step>({ name: 'server' })

  return (
    <div style={page}>
      <div style={{ fontSize: 24, fontWeight: 700, color: '#fff' }}>Set up RomM</div>
      {step.name === 'server' && (
        <ServerStep onFound={(found) => setStep({ name: 'pair', found })} />
      )}
      {step.name === 'pair' && (
        <PairStep
          found={step.found}
          onBack={() => setStep({ name: 'server' })}
          onSignedIn={(status) => {
            publishConnection(status)
            setStep({ name: 'done', status })
          }}
        />
      )}
      {step.name === 'done' && <DoneStep status={step.status} />}
    </div>
  )
}
