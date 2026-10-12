import { DialogButton, Field, Focusable, Navigation, TextField } from '@decky/ui'
import { type CSSProperties, useState } from 'react'

import { call } from '../backend'
import { publishConnection } from '../connection'
import { disabledReason, featureLabel } from '../features'
import type {
  CheckServerResult,
  ConnectionStatus,
  Error as RpcError,
  Server,
} from '../rpc/schema.gen'
import { asRpcError, ErrorBox, mono, muted } from './common'
import { DevicePairingPanel } from './DevicePairingPanel'

type Step =
  | { name: 'server' }
  | { name: 'sign_in'; found: CheckServerResult }
  | { name: 'done'; status: ConnectionStatus }

// Routes render under Steam's header bar.
const page: CSSProperties = {
  padding: '56px 32px 32px',
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
  maxWidth: 720,
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

function PairCodeForm({
  server,
  onSignedIn,
}: {
  server: Server
  onSignedIn: (status: ConnectionStatus) => void
}) {
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<RpcError | null>(null)

  const signIn = async () => {
    setBusy(true)
    setError(null)
    try {
      onSignedIn(await call('sign_in_with_pair_code', { server, code }))
    } catch (err) {
      setError(asRpcError(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
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
      <Focusable style={{ display: 'flex', gap: 12 }}>
        <DialogButton disabled={busy || code.trim() === ''} onClick={() => void signIn()}>
          {busy ? 'Signing in…' : 'Sign in'}
        </DialogButton>
      </Focusable>
    </>
  )
}

function PasswordForm({
  server,
  onSignedIn,
}: {
  server: Server
  onSignedIn: (status: ConnectionStatus) => void
}) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<RpcError | null>(null)

  const signIn = async () => {
    setBusy(true)
    setError(null)
    try {
      onSignedIn(await call('sign_in_with_password', { server, username, password }))
    } catch (err) {
      setError(asRpcError(err))
    } finally {
      // The password lives only as long as the form needs it.
      setPassword('')
      setBusy(false)
    }
  }

  return (
    <>
      <div style={muted}>
        Sign in with your RomM account. The plugin uses your password once to add this Deck as a
        device in RomM, then forgets it.
      </div>
      <TextField
        label="Username"
        value={username}
        disabled={busy}
        onChange={(e) => {
          setUsername(e.target.value)
          setError(null)
        }}
      />
      <TextField
        label="Password"
        bIsPassword
        value={password}
        disabled={busy}
        onChange={(e) => {
          setPassword(e.target.value)
          setError(null)
        }}
      />
      {error && <ErrorBox>{error.message}</ErrorBox>}
      <Focusable style={{ display: 'flex', gap: 12 }}>
        <DialogButton
          disabled={busy || username.trim() === '' || password === ''}
          onClick={() => void signIn()}
        >
          {busy ? 'Signing in…' : 'Sign in'}
        </DialogButton>
      </Focusable>
    </>
  )
}

type Method = 'qr' | 'code' | 'password'

const methodLabels: Record<Method, string> = {
  qr: 'Use a QR code',
  code: 'Use a pair code',
  password: 'Use a password',
}

function SignInStep({
  found,
  onBack,
  onSignedIn,
}: {
  found: CheckServerResult
  onBack: () => void
  onSignedIn: (status: ConnectionStatus) => void
}) {
  const [method, setMethod] = useState<Method>('qr')

  return (
    <>
      <Field label="Server" description={`RomM ${found.version}`}>
        {found.server.url}
      </Field>
      {method === 'qr' && <DevicePairingPanel server={found.server} onSignedIn={onSignedIn} />}
      {method === 'code' && <PairCodeForm server={found.server} onSignedIn={onSignedIn} />}
      {method === 'password' && <PasswordForm server={found.server} onSignedIn={onSignedIn} />}
      <Focusable style={{ display: 'flex', gap: 12 }} flow-children="horizontal">
        <DialogButton onClick={onBack}>Back</DialogButton>
        {(Object.keys(methodLabels) as Method[])
          .filter((m) => m !== method)
          .map((m) => (
            <DialogButton key={m} onClick={() => setMethod(m)}>
              {methodLabels[m]}
            </DialogButton>
          ))}
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
      {status.disabledFeatures?.map((d) => (
        <Field
          key={d.feature}
          label={`${featureLabel(d.feature)} is off`}
          description={disabledReason(d)}
        />
      ))}
      <Focusable style={{ display: 'flex', gap: 12 }}>
        <DialogButton onClick={() => Navigation.NavigateBack()}>Done</DialogButton>
      </Focusable>
    </>
  )
}

export function SetupPage() {
  const [step, setStep] = useState<Step>({ name: 'server' })

  const signedIn = (status: ConnectionStatus) => {
    publishConnection(status)
    setStep({ name: 'done', status })
  }

  return (
    <div style={page}>
      <div style={{ fontSize: 24, fontWeight: 700, color: '#fff' }}>Set up RomM</div>
      {step.name === 'server' && (
        <ServerStep onFound={(found) => setStep({ name: 'sign_in', found })} />
      )}
      {step.name === 'sign_in' && (
        <SignInStep
          found={step.found}
          onBack={() => setStep({ name: 'server' })}
          onSignedIn={signedIn}
        />
      )}
      {step.name === 'done' && <DoneStep status={step.status} />}
    </div>
  )
}
