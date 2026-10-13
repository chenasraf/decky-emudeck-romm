import {
  ConfirmModal,
  DialogButton,
  Field,
  Focusable,
  Navigation,
  showModal,
  TextField,
} from '@decky/ui'
import { useState } from 'react'

import { call } from '../backend'
import { currentConnection, publishConnection } from '../connection'
import { disabledReason, featureLabel } from '../features'
import type {
  CheckServerResult,
  ConnectionStatus,
  Error as RpcError,
  Server,
} from '../rpc/schema.gen'
import { asRpcError, ErrorBox, mono, muted, page } from './common'
import { DevicePairingPanel } from './DevicePairingPanel'

type Step =
  | { name: 'server' }
  | { name: 'sign_in'; found: CheckServerResult }
  | { name: 'done'; status: ConnectionStatus }
  | { name: 'manage' }
  | { name: 'edit_url' }
  | { name: 'different'; found: CheckServerResult; why: string }

function ServerStep({
  onFound,
  onBack,
}: {
  onFound: (found: CheckServerResult) => Promise<void> | void
  onBack?: () => void
}) {
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<RpcError | null>(null)

  const check = async (trustFingerprint?: string) => {
    setBusy(true)
    setError(null)
    try {
      await onFound(await call('check_server', { url, trustFingerprint }))
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
        {onBack && (
          <DialogButton disabled={busy} onClick={onBack}>
            Back
          </DialogButton>
        )}
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
  onForgotten,
}: {
  found: CheckServerResult
  onBack: () => void
  onSignedIn: (status: ConnectionStatus) => void
  /** Set while signing in again, to offer forgetting the server instead. */
  onForgotten?: (status: ConnectionStatus) => void
}) {
  const [method, setMethod] = useState<Method>('qr')
  const [error, setError] = useState<RpcError | null>(null)

  return (
    <>
      <Field label="Server" description={found.version ? `RomM ${found.version}` : undefined}>
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
        {onForgotten && (
          <DialogButton onClick={() => confirmForget(onForgotten, setError)}>
            Forget server
          </DialogButton>
        )}
      </Focusable>
      {error && <ErrorBox>{error.message}</ErrorBox>}
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

/** Asks before forgetting the server, then forgets it. */
function confirmForget(
  onForgotten: (status: ConnectionStatus) => void,
  onError: (e: RpcError) => void,
) {
  showModal(
    <ConfirmModal
      strTitle="Forget this server?"
      strDescription="The plugin drops its sign-in, its copy of the library and its records of the games it downloaded, and cancels the downloads in the queue. Every file stays on this Deck, yours to manage: the plugin won't update or delete them."
      strOKButtonText="Forget server"
      bDestructiveWarning
      onOK={() => {
        call('forget_server').then(onForgotten, (err: unknown) => onError(asRpcError(err)))
      }}
    />,
  )
}

function ManageStep({
  status,
  onEditURL,
  onForgotten,
}: {
  status: ConnectionStatus | null
  onEditURL: () => void
  onForgotten: (status: ConnectionStatus) => void
}) {
  const [error, setError] = useState<RpcError | null>(null)
  return (
    <>
      <Field label="Server" description={status?.version ? `RomM ${status.version}` : undefined}>
        {status?.serverUrl}
      </Field>
      <div style={muted}>
        Moved RomM to a new address? Change it here: the plugin checks it&apos;s the same server and
        keeps everything. To use a different RomM server, forget this one first.
      </div>
      {error && <ErrorBox>{error.message}</ErrorBox>}
      <Focusable style={{ display: 'flex', gap: 12 }} flow-children="horizontal">
        <DialogButton onClick={onEditURL}>Change address</DialogButton>
        <DialogButton onClick={() => confirmForget(onForgotten, setError)}>
          Forget server
        </DialogButton>
        <DialogButton onClick={() => Navigation.NavigateBack()}>Done</DialogButton>
      </Focusable>
    </>
  )
}

function DifferentStep({
  found,
  why,
  onBack,
  onForgotten,
}: {
  found: CheckServerResult
  why: string
  onBack: () => void
  onForgotten: (status: ConnectionStatus) => void
}) {
  const [error, setError] = useState<RpcError | null>(null)
  return (
    <>
      <Field label="⚠ A different server?" description={why}>
        {found.server.url}
      </Field>
      <div style={muted}>
        Nothing changed. To use that server, forget this one and sign in there: files stay on this
        Deck, but the plugin no longer manages them.
      </div>
      {error && <ErrorBox>{error.message}</ErrorBox>}
      <Focusable style={{ display: 'flex', gap: 12 }} flow-children="horizontal">
        <DialogButton onClick={onBack}>Back</DialogButton>
        <DialogButton onClick={() => confirmForget(onForgotten, setError)}>
          Forget server and sign in there
        </DialogButton>
      </Focusable>
    </>
  )
}

/**
 * Signed out, the route signs in again on the same server, with its pinned certificate, so the
 * Device and every local record carry over. Set up, it manages the server.
 */
function firstStep(): Step {
  const status = currentConnection()
  if (status?.state === 'connected' || status?.state === 'offline') return { name: 'manage' }
  if (status?.state !== 'signed_out' || !status.serverUrl) return { name: 'server' }
  return {
    name: 'sign_in',
    found: {
      server: { url: status.serverUrl, certFingerprint: status.certFingerprint },
      version: status.version ?? '',
    },
  }
}

export function SetupPage() {
  const [step, setStep] = useState<Step>(firstStep)
  const [mode, setMode] = useState<'set_up' | 'sign_in_again' | 'manage'>(() =>
    step.name === 'sign_in' ? 'sign_in_again' : step.name === 'manage' ? 'manage' : 'set_up',
  )

  const signedIn = (status: ConnectionStatus) => {
    publishConnection(status)
    setStep({ name: 'done', status })
  }
  // Forgotten, the route starts over as setting up, at found's server when there is one.
  const forgotten = (status: ConnectionStatus, found?: CheckServerResult) => {
    publishConnection(status)
    setMode('set_up')
    setStep(found ? { name: 'sign_in', found } : { name: 'server' })
  }
  const move = async (found: CheckServerResult) => {
    const r = await call('move_server', { server: found.server })
    if (r.moved && r.status) {
      publishConnection(r.status)
      setStep({ name: 'done', status: r.status })
    } else {
      setStep({ name: 'different', found, why: r.why ?? '' })
    }
  }

  const titles = { set_up: 'Set up RomM', sign_in_again: 'Sign in again', manage: 'RomM server' }
  return (
    <div style={page}>
      <div style={{ fontSize: 24, fontWeight: 700, color: '#fff' }}>{titles[mode]}</div>
      {step.name === 'server' && (
        <ServerStep onFound={(found) => setStep({ name: 'sign_in', found })} />
      )}
      {step.name === 'sign_in' && (
        <SignInStep
          found={step.found}
          onBack={
            mode === 'sign_in_again'
              ? () => Navigation.NavigateBack()
              : () => setStep({ name: 'server' })
          }
          onSignedIn={signedIn}
          onForgotten={mode === 'sign_in_again' ? (status) => forgotten(status) : undefined}
        />
      )}
      {step.name === 'manage' && (
        <ManageStep
          status={currentConnection()}
          onEditURL={() => setStep({ name: 'edit_url' })}
          onForgotten={(status) => forgotten(status)}
        />
      )}
      {step.name === 'edit_url' && (
        <ServerStep onFound={move} onBack={() => setStep({ name: 'manage' })} />
      )}
      {step.name === 'different' && (
        <DifferentStep
          found={step.found}
          why={step.why}
          onBack={() => setStep({ name: 'manage' })}
          onForgotten={(status) => forgotten(status, step.found)}
        />
      )}
      {step.name === 'done' && <DoneStep status={step.status} />}
    </div>
  )
}
