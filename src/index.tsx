import { addEventListener, definePlugin, removeEventListener, routerHook } from '@decky/api'
import {
  ButtonItem,
  Field,
  Navigation,
  PanelSection,
  PanelSectionRow,
  staticClasses,
} from '@decky/ui'
import { useEffect, useState } from 'react'
import { FaCompactDisc } from 'react-icons/fa'

import {
  BACKEND_STATUS_EVENT,
  type BackendState,
  type BackendStatus,
  backendStatus,
  restartBackend,
} from './backend'
import { SETUP_ROUTE, useConnection } from './connection'
import { disabledReason, featureLabel } from './features'
import { SetupPage } from './setup/SetupPage'

const STATE_LABELS: Record<BackendState, string> = {
  starting: 'Starting…',
  ready: 'Running',
  restarting: 'Restarting…',
  down: 'Stopped',
  refused: 'Incompatible',
  stopped: 'Stopped',
}

function openSetup() {
  Navigation.CloseSideMenus()
  Navigation.Navigate(SETUP_ROUTE)
}

function ServerSection() {
  const status = useConnection()

  if (!status) {
    return (
      <PanelSection title="RomM">
        <PanelSectionRow>
          <Field label="Status">…</Field>
        </PanelSectionRow>
      </PanelSection>
    )
  }

  if (status.state === 'not_set_up') {
    return (
      <PanelSection title="RomM">
        <PanelSectionRow>
          <ButtonItem layout="below" onClick={openSetup}>
            Set up RomM
          </ButtonItem>
        </PanelSectionRow>
      </PanelSection>
    )
  }

  const connected = status.state === 'connected'
  return (
    <PanelSection title="RomM">
      <PanelSectionRow>
        <Field label="Status" description={status.problem?.message}>
          {connected ? 'Connected' : 'Offline'}
        </Field>
      </PanelSectionRow>
      <PanelSectionRow>
        <Field label="Server" description={connected ? `RomM ${status.version}` : undefined}>
          {status.serverUrl}
        </Field>
      </PanelSectionRow>
      {status.disabledFeatures?.map((d) => (
        <PanelSectionRow key={d.feature}>
          <Field label={featureLabel(d.feature)} description={disabledReason(d)}>
            Off
          </Field>
        </PanelSectionRow>
      ))}
      {status.problem?.data?.kind === 'untrusted_cert' && (
        <PanelSectionRow>
          <ButtonItem layout="below" onClick={openSetup}>
            Set up RomM
          </ButtonItem>
        </PanelSectionRow>
      )}
    </PanelSection>
  )
}

function BackendSection() {
  const [status, setStatus] = useState<BackendStatus | null>(null)

  useEffect(() => {
    backendStatus().then(setStatus, () => setStatus(null))
    const listener = addEventListener<[BackendStatus]>(BACKEND_STATUS_EVENT, setStatus)
    return () => {
      removeEventListener(BACKEND_STATUS_EVENT, listener)
    }
  }, [])

  const canRestart = status?.state === 'down' || status?.state === 'refused'

  return (
    <PanelSection title="Backend">
      <PanelSectionRow>
        <Field label="Status" description={status?.reason ?? undefined}>
          {status ? STATE_LABELS[status.state] : '…'}
        </Field>
      </PanelSectionRow>
      <PanelSectionRow>
        <Field label="Version">{status?.version ?? '—'}</Field>
      </PanelSectionRow>
      {canRestart && (
        <PanelSectionRow>
          <ButtonItem layout="below" onClick={() => void restartBackend()}>
            Restart backend
          </ButtonItem>
        </PanelSectionRow>
      )}
    </PanelSection>
  )
}

function Content() {
  return (
    <>
      <ServerSection />
      <BackendSection />
    </>
  )
}

export default definePlugin(() => {
  routerHook.addRoute(SETUP_ROUTE, SetupPage, { exact: true })

  return {
    name: 'EmuDeck RomM Sync',
    titleView: <div className={staticClasses.Title}>EmuDeck RomM Sync</div>,
    content: <Content />,
    icon: <FaCompactDisc />,
    onDismount() {
      routerHook.removeRoute(SETUP_ROUTE)
    },
  }
})
