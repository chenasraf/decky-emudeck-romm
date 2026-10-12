import { DialogButton, Focusable } from '@decky/ui'
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useRef, useState } from 'react'

import { call, onEvent } from '../backend'
import type { ConnectionStatus, DevicePairing, Server } from '../rpc/schema.gen'
import { asRpcError, ErrorBox, mono, muted } from './common'

type State =
  | { name: 'starting' }
  | { name: 'waiting'; pairing: DevicePairing; deadline: number }
  | { name: 'denied' }
  | { name: 'expired' }
  | { name: 'failed'; message: string }

function formatCode(code: string): string {
  const c = code.replace('-', '')
  return c.length === 8 ? `${c.slice(0, 4)}-${c.slice(4)}` : code
}

function Countdown({ deadline }: { deadline: number }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])
  const left = Math.max(0, Math.round((deadline - now) / 1000))
  return (
    <>
      {Math.floor(left / 60)}:{String(left % 60).padStart(2, '0')}
    </>
  )
}

/**
 * Pairs this Deck as a RomM Device: shows a QR code for the user to scan and approve on another
 * device, and waits for RomM's answer.
 */
export function DevicePairingPanel({
  server,
  onSignedIn,
}: {
  server: Server
  onSignedIn: (status: ConnectionStatus) => void
}) {
  const [state, setState] = useState<State>({ name: 'starting' })
  const [attempt, setAttempt] = useState(0)
  // Read through a ref so a parent re-render doesn't restart the pairing.
  const signedIn = useRef(onSignedIn)
  useEffect(() => {
    signedIn.current = onSignedIn
  }, [onSignedIn])
  const { url, certFingerprint } = server

  useEffect(() => {
    let active = true
    let userCode: string | null = null

    const stop = onEvent('device_pairing', (event) => {
      if (!active) return
      if (event.state === 'approved' && event.status) {
        userCode = null
        signedIn.current(event.status)
      } else if (event.state === 'denied') {
        setState({ name: 'denied' })
      } else if (event.state === 'expired') {
        setState({ name: 'expired' })
      } else if (event.state === 'failed') {
        userCode = null
        setState({
          name: 'failed',
          message: `RomM approved this Deck, but the plugin couldn't save the sign-in: ${event.problem?.message ?? 'unknown error'}`,
        })
      }
    })

    call('start_device_pairing', { server: { url, certFingerprint } })
      .then((pairing) => {
        userCode = pairing.userCode
        if (!active) {
          // Left before the request came back; nobody will see this code.
          void call('cancel_device_pairing', { userCode })
          return
        }
        setState({
          name: 'waiting',
          pairing,
          deadline: Date.now() + pairing.expiresInSeconds * 1000,
        })
      })
      .catch((err: unknown) => {
        if (active) setState({ name: 'failed', message: asRpcError(err).message })
      })

    return () => {
      active = false
      stop()
      if (userCode) void call('cancel_device_pairing', { userCode })
    }
  }, [url, certFingerprint, attempt])

  const retry = (
    <Focusable style={{ display: 'flex', gap: 12 }}>
      <DialogButton
        onClick={() => {
          setState({ name: 'starting' })
          setAttempt((n) => n + 1)
        }}
      >
        Try again
      </DialogButton>
    </Focusable>
  )

  switch (state.name) {
    case 'starting':
      return <div style={muted}>Asking RomM for a pairing code…</div>
    case 'denied':
      return (
        <>
          <ErrorBox>The pairing request was denied in RomM.</ErrorBox>
          {retry}
        </>
      )
    case 'expired':
      return (
        <>
          <ErrorBox>The pairing code expired before it was approved.</ErrorBox>
          {retry}
        </>
      )
    case 'failed':
      return (
        <>
          <ErrorBox>{state.message}</ErrorBox>
          {retry}
        </>
      )
    case 'waiting':
      return (
        <div style={{ display: 'flex', gap: 24, alignItems: 'center' }}>
          <div style={{ background: '#fff', padding: 12, borderRadius: 4, lineHeight: 0 }}>
            <QRCodeSVG value={state.pairing.verificationUrl} size={176} />
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <div style={muted}>
              Scan the code with your phone, sign in to RomM if asked, and approve this Deck. Or
              open this address on any device:
            </div>
            <div style={mono}>{state.pairing.verificationUrl}</div>
            <div style={{ fontSize: 28, fontWeight: 700, letterSpacing: 2, color: '#fff' }}>
              {formatCode(state.pairing.userCode)}
            </div>
            <div style={muted}>
              Waiting for approval · expires in <Countdown deadline={state.deadline} />
            </div>
          </div>
        </div>
      )
  }
}
