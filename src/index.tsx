import { addEventListener, definePlugin, removeEventListener, routerHook } from '@decky/api'
import {
  ButtonItem,
  Field,
  Navigation,
  PanelSection,
  PanelSectionRow,
  staticClasses,
  ToggleField,
} from '@decky/ui'
import { useEffect, useState } from 'react'
import { FaCompactDisc } from 'react-icons/fa'

import {
  BACKEND_STATUS_EVENT,
  type BackendState,
  type BackendStatus,
  backendStatus,
  call,
  restartBackend,
} from './backend'
import { watchBios } from './bios'
import { LIBRARY_ROUTE, timeAgo } from './catalog'
import { SETUP_ROUTE, useConnection, watchConnection } from './connection'
import { disabledReason, featureLabel } from './features'
import { loadInstallState, STATUS_ROUTE, useInstallState } from './install'
import { watchDownloads } from './downloads'
import { rescanFor, useESDENotice, watchESDE } from './esde'
import {
  CollectionsView,
  DownloadsSection,
  type GameSource,
  GamesView,
  GameView,
  LibrarySection,
  PlatformsView,
  SearchView,
} from './library/Library'
import { LibraryPage } from './library/LibraryPage'
import type { GameFilter, Preferences } from './rpc/schema.gen'
import { useSaveSummary, watchSaves } from './saves'
import { asRpcError } from './setup/common'
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
      <PanelSectionRow>
        <ButtonItem layout="below" onClick={openSetup}>
          Server settings
        </ButtonItem>
      </PanelSectionRow>
    </PanelSection>
  )
}

function openStatus() {
  Navigation.CloseSideMenus()
  Navigation.Navigate(STATUS_ROUTE)
}

function InstallSection() {
  const state = useInstallState()
  const esde = useESDENotice()
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
      {state.esde.settingsFound && state.emudeck.found && !state.esde.romDirectoryMatches && (
        <PanelSectionRow>
          <Field
            label="⚠ ES-DE looks elsewhere"
            description={`ES-DE reads games from ${state.esde.romDirectory}, not EmuDeck's ROMs folder, so downloaded games won't appear. See Setup status.`}
          />
        </PanelSectionRow>
      )}
      {esde && (
        <PanelSectionRow>
          <Field
            label="⚠ ES-DE needs a rescan"
            description={`ES-DE was open while games changed. Run Utilities → Rescan ROM directory, or restart ES-DE, to ${rescanFor(esde)}.`}
          />
        </PanelSectionRow>
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

function SaveSyncSection() {
  const connection = useConnection()
  const summary = useSaveSummary()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!summary || !connection || connection.state === 'not_set_up') return null

  const syncAll = async () => {
    setBusy(true)
    try {
      await call('sync_saves', {})
      setError(null)
    } catch (err) {
      setError(asRpcError(err).message)
    }
    setBusy(false)
  }
  const { conflicts, waiting, failed } = summary.counts
  const status = summary.syncing
    ? 'Syncing…'
    : summary.lastCheckedAt
      ? `Synced ${timeAgo(summary.lastCheckedAt)}`
      : 'Not synced yet'

  return (
    <PanelSection title="Saves">
      <PanelSectionRow>
        <Field label="RetroArch saves">{status}</Field>
      </PanelSectionRow>
      {conflicts > 0 && (
        <PanelSectionRow>
          <Field
            label="⚠ Save conflicts"
            description={`${conflicts === 1 ? '1 game' : `${conflicts} games`} changed both on this Deck and on RomM. Neither copy was overwritten.`}
          />
        </PanelSectionRow>
      )}
      {waiting > 0 && (
        <PanelSectionRow>
          <Field
            label="Waiting for RetroArch"
            description={`RomM has newer saves for ${waiting === 1 ? '1 game' : `${waiting} games`}. Close RetroArch and sync again.`}
          />
        </PanelSectionRow>
      )}
      {failed > 0 && (
        <PanelSectionRow>
          <Field
            label="⚠ Sync failed"
            description={`${failed === 1 ? "1 game's save" : `${failed} games' saves`} didn't sync. Its page says why.`}
          />
        </PanelSectionRow>
      )}
      {error && (
        <PanelSectionRow>
          <Field label="⚠ Couldn't sync" description={error} />
        </PanelSectionRow>
      )}
      <PanelSectionRow>
        <ButtonItem
          layout="below"
          disabled={busy || summary.syncing}
          onClick={() => void syncAll()}
        >
          {summary.syncing ? 'Syncing saves…' : 'Sync all saves'}
        </ButtonItem>
      </PanelSectionRow>
    </PanelSection>
  )
}

function SettingsSection() {
  const connection = useConnection()
  const [prefs, setPrefs] = useState<Preferences | null>(null)

  useEffect(() => {
    call('get_preferences').then(setPrefs, () => setPrefs(null))
  }, [])

  if (!prefs || !connection || connection.state === 'not_set_up') return null

  return (
    <PanelSection title="Settings">
      <PanelSectionRow>
        <ToggleField
          label="Download BIOS with games"
          description="A platform's first game download also fetches its missing BIOS files. Files already in the BIOS folder are never replaced."
          checked={prefs.autoBios}
          onChange={(autoBios) => {
            setPrefs({ ...prefs, autoBios })
            call('set_preferences', { autoBios }).then(setPrefs, () => setPrefs(prefs))
          }}
        />
      </PanelSectionRow>
      <PanelSectionRow>
        <ToggleField
          label="Wait for games to close"
          description="While an emulator is running, downloaded games wait to be extracted until it closes, so the game doesn't stutter. Downloads keep going."
          checked={prefs.waitForGames}
          onChange={(waitForGames) => {
            setPrefs({ ...prefs, waitForGames })
            call('set_preferences', { waitForGames }).then(setPrefs, () => setPrefs(prefs))
          }}
        />
      </PanelSectionRow>
      <PanelSectionRow>
        <ToggleField
          label="Add art to ES-DE"
          description="Downloaded games get their RomM cover and screenshot in ES-DE without scraping. Art already in ES-DE is never replaced, and art ES-DE's scraper replaced stays when a game is deleted."
          checked={prefs.esdeMedia}
          onChange={(esdeMedia) => {
            setPrefs({ ...prefs, esdeMedia })
            call('set_preferences', { esdeMedia }).then(setPrefs, () => setPrefs(prefs))
          }}
        />
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
  | { name: 'collections' }
  | { name: 'games'; source: GameSource; filter: GameFilter; game?: number }
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
        onOpen={(platform) =>
          setView({ name: 'games', source: { kind: 'platform', platform }, filter: 'all' })
        }
      />
    )
  }
  if (view.name === 'collections') {
    return (
      <CollectionsView
        onBack={() => setView({ name: 'home' })}
        onOpen={(collection) =>
          setView({ name: 'games', source: { kind: 'collection', collection }, filter: 'all' })
        }
      />
    )
  }
  if (view.name === 'games') {
    return (
      <>
        {/* Kept mounted while a game is open, so Back returns to the same place in a long list. */}
        <div style={{ display: view.game === undefined ? undefined : 'none' }}>
          <GamesView
            source={view.source}
            filter={view.filter}
            onFilter={(filter) => setView({ ...view, filter })}
            onBack={() =>
              setView(
                view.source.kind === 'platform' ? { name: 'platforms' } : { name: 'collections' },
              )
            }
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
        onCollections={() => setView({ name: 'collections' })}
        onSearch={() => setView({ name: 'search', query: '' })}
      />
      <SaveSyncSection />
      <SettingsSection />
      <InstallSection />
      <BackendSection />
    </>
  )
}

export default definePlugin(() => {
  routerHook.addRoute(SETUP_ROUTE, SetupPage, { exact: true })
  routerHook.addRoute(STATUS_ROUTE, StatusPage, { exact: true })
  routerHook.addRoute(LIBRARY_ROUTE, LibraryPage, { exact: true })
  const stopWatchingConnection = watchConnection()
  const stopWatchingDownloads = watchDownloads()
  const stopWatchingBios = watchBios()
  const stopWatchingESDE = watchESDE()
  const stopWatchingSaves = watchSaves()

  return {
    name: 'EmuDeck RomM Sync',
    titleView: <div className={staticClasses.Title}>EmuDeck RomM Sync</div>,
    content: <Content />,
    icon: <FaCompactDisc />,
    onDismount() {
      routerHook.removeRoute(SETUP_ROUTE)
      routerHook.removeRoute(STATUS_ROUTE)
      routerHook.removeRoute(LIBRARY_ROUTE)
      stopWatchingConnection()
      stopWatchingDownloads()
      stopWatchingBios()
      stopWatchingESDE()
      stopWatchingSaves()
    },
  }
})
