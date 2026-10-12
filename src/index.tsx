import { addEventListener, definePlugin, removeEventListener } from '@decky/api'
import { ButtonItem, Field, PanelSection, PanelSectionRow, staticClasses } from '@decky/ui'
import { useEffect, useState } from 'react'
import { FaCompactDisc } from 'react-icons/fa'

import {
  BACKEND_STATUS_EVENT,
  type BackendState,
  type BackendStatus,
  backendStatus,
  restartBackend,
} from './backend'

const STATE_LABELS: Record<BackendState, string> = {
  starting: 'Starting…',
  ready: 'Running',
  restarting: 'Restarting…',
  down: 'Stopped',
  refused: 'Incompatible',
  stopped: 'Stopped',
}

function Content() {
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

export default definePlugin(() => ({
  name: 'EmuDeck RomM Sync',
  titleView: <div className={staticClasses.Title}>EmuDeck RomM Sync</div>,
  content: <Content />,
  icon: <FaCompactDisc />,
}))
