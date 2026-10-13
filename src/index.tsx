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
import { SETUP_ROUTE, useConnection, watchConnection } from './connection'
import { disabledReason, featureLabel } from './features'
import { loadInstallState, STATUS_ROUTE, useInstallState } from './install'
import { watchDownloads } from './downloads'
import {
  DownloadsSection,
  GamesView,
  GameView,
  LibrarySection,
  PlatformsView,
  SearchView,
} from './library/Library'
import type { GameFilter, PlatformSummary } from './rpc/schema.gen'
import { SetupPage } from './setup/SetupPage'
import { StatusPage } from './status/StatusPage'

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

  if (status.state === 'signed_out') {
    return (
      <PanelSection title="RomM">
        <PanelSectionRow>
          <Field
            label="Signed out"
            description="RomM no longer accepts this Deck's sign-in. Work with RomM is paused until you sign in again."
          >
            {status.serverUrl}
          </Field>
        </PanelSectionRow>
        <PanelSectionRow>
          <ButtonItem layout="below" onClick={openSetup}>
            Sign in again
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

function openStatus() {
  Navigation.CloseSideMenus()
  Navigation.Navigate(STATUS_ROUTE)
}

function InstallSection() {
  const state = useInstallState()
  const [checking, setChecking] = useState(false)

  if (!state) return null

  const checkAgain = async () => {
    setChecking(true)
    await loadInstallState(true)
    setChecking(false)
  }

  return (
    <PanelSection title="EmuDeck">
      {state.emudeck.found ? (
        <PanelSectionRow>
          <Field label="EmuDeck" description={state.emudeck.root}>
            Found
          </Field>
        </PanelSectionRow>
      ) : (
        <>
          <PanelSectionRow>
            <Field
              label="⚠ EmuDeck not found"
              description="Downloads, BIOS and save sync stay off until the plugin finds EmuDeck's Emulation folder."
            />
          </PanelSectionRow>
          <PanelSectionRow>
            <ButtonItem layout="below" disabled={checking} onClick={() => void checkAgain()}>
              {checking ? 'Checking…' : 'Check again'}
            </ButtonItem>
          </PanelSectionRow>
        </>
      )}
      {state.esde.parseGamelistOnly && (
        <PanelSectionRow>
          <Field
            label="⚠ ES-DE hides new games"
            description="ES-DE only shows games from its gamelists, so downloaded games won't appear. Turn off Other Settings → Only show games from gamelist files in ES-DE."
          />
        </PanelSectionRow>
      )}
      <PanelSectionRow>
        <ButtonItem layout="below" onClick={openStatus}>
          Setup status
        </ButtonItem>
      </PanelSectionRow>
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

/** Where the QAM is in the library: the home sections, or a level of the drill-down. */
type View =
  | { name: 'home' }
  | { name: 'platforms' }
  | { name: 'games'; platform: PlatformSummary; filter: GameFilter; game?: number }
  | { name: 'search'; query: string; game?: number }
  | { name: 'game'; id: number }

function Content() {
  const [view, setView] = useState<View>({ name: 'home' })

  if (view.name === 'search') {
    return (
      <>
        {/* Kept mounted while a game is open, so Back returns to the same results. */}
        <div style={{ display: view.game === undefined ? undefined : 'none' }}>
          <SearchView
            query={view.query}
            onQuery={(query) => setView({ ...view, query })}
            onBack={() => setView({ name: 'home' })}
            onOpen={(game) => setView({ ...view, game: game.id })}
          />
        </div>
        {view.game !== undefined && (
          <GameView id={view.game} onBack={() => setView({ ...view, game: undefined })} />
        )}
      </>
    )
  }

  if (view.name === 'platforms') {
    return (
      <PlatformsView
        onBack={() => setView({ name: 'home' })}
        onOpen={(platform) => setView({ name: 'games', platform, filter: 'all' })}
      />
    )
  }
  if (view.name === 'games') {
    return (
      <>
        {/* Kept mounted while a game is open, so Back returns to the same place in a long list. */}
        <div style={{ display: view.game === undefined ? undefined : 'none' }}>
          <GamesView
            platform={view.platform}
            filter={view.filter}
            onFilter={(filter) => setView({ ...view, filter })}
            onBack={() => setView({ name: 'platforms' })}
            onOpen={(game) => setView({ ...view, game: game.id })}
          />
        </div>
        {view.game !== undefined && (
          <GameView id={view.game} onBack={() => setView({ ...view, game: undefined })} />
        )}
      </>
    )
  }
  if (view.name === 'game') {
    return <GameView id={view.id} onBack={() => setView({ name: 'home' })} />
  }
  return (
    <>
      <ServerSection />
      <DownloadsSection onOpen={(id) => setView({ name: 'game', id })} />
      <LibrarySection
        onBrowse={() => setView({ name: 'platforms' })}
        onSearch={() => setView({ name: 'search', query: '' })}
      />
      <InstallSection />
      <BackendSection />
    </>
  )
}

export default definePlugin(() => {
  routerHook.addRoute(SETUP_ROUTE, SetupPage, { exact: true })
  routerHook.addRoute(STATUS_ROUTE, StatusPage, { exact: true })
  const stopWatchingConnection = watchConnection()
  const stopWatchingDownloads = watchDownloads()

  return {
    name: 'EmuDeck RomM Sync',
    titleView: <div className={staticClasses.Title}>EmuDeck RomM Sync</div>,
    content: <Content />,
    icon: <FaCompactDisc />,
    onDismount() {
      routerHook.removeRoute(SETUP_ROUTE)
      routerHook.removeRoute(STATUS_ROUTE)
      stopWatchingConnection()
      stopWatchingDownloads()
    },
  }
})
