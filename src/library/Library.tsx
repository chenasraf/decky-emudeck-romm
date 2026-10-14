import { toaster } from '@decky/api'
import {
  ButtonItem,
  ConfirmModal,
  DialogButton,
  DropdownItem,
  Field,
  Focusable,
  Navigation,
  PanelSection,
  PanelSectionRow,
  ProgressBarWithInfo,
  showModal,
  TextField,
  ToggleField,
} from '@decky/ui'
import {
  type CSSProperties,
  Fragment,
  type ReactNode,
  useCallback,
  useEffect,
  useState,
} from 'react'
import {
  FaBookmark,
  FaBookOpen,
  FaCheckCircle,
  FaCloudDownloadAlt,
  FaCloudUploadAlt,
  FaExchangeAlt,
  FaExclamationTriangle,
  FaHistory,
  FaLayerGroup,
  FaListUl,
  FaSearch,
  FaSyncAlt,
  FaTimesCircle,
  FaTrashAlt,
} from 'react-icons/fa'

import { call, restartBackend } from '../backend'
import { usePlatformBios } from '../bios'
import { formatSize, LIBRARY_ROUTE, refreshLibrary, timeAgo, useCatalog } from '../catalog'
import { useConnection } from '../connection'
import { Cover } from '../covers'
import { type Downloads, percent, useDownloads } from '../downloads'
import { disabledReason } from '../features'
import type {
  CollectionRef,
  CollectionSummary,
  DeleteGamesParams,
  DownloadItem,
  Game,
  GameFilter,
  GameState,
  GameSummary,
  ListGamesResult,
  OnDeckCheck,
  PlatformSummary,
  Error as RpcError,
  SaveChoice,
  SaveDeletion,
  SearchGamesResult,
  SearchHit,
} from '../rpc/schema.gen'
import {
  refreshSaves,
  saveStateDetail,
  saveStateLabel,
  useSaveActivity,
  useSaveBackups,
  useSaveStatus,
} from '../saves'
import { asRpcError } from '../setup/common'
import { showFindGames } from './FindGames'
import { showSaveSlots } from './SaveSlots'

/** How many games each step of a list loads; "Show more" loads the next. */
const PAGE_SIZE = 50

const row: CSSProperties = {
  display: 'flex',
  justifyContent: 'space-between',
  gap: 8,
  width: '100%',
}
const count: CSSProperties = { color: '#8b929a', flexShrink: 0 }
const name: CSSProperties = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }

const gameName: CSSProperties = { ...name, flex: '1 1 auto', textAlign: 'left' }
const stacked: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  flex: '1 1 auto',
  minWidth: 0,
  textAlign: 'left',
}
const subline: CSSProperties = { ...name, color: '#8b929a', fontSize: 12, lineHeight: '16px' }

const iconLabel: CSSProperties = { display: 'flex', alignItems: 'center', gap: 10 }

/** A button's label led by its icon. */
export function IconLabel({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <span style={iconLabel}>
      {icon}
      {children}
    </span>
  )
}

function Row({
  label,
  value,
  cover,
  icon,
  onClick,
}: {
  label: string
  value?: ReactNode
  /** The game whose small cover leads the row. */
  cover?: { id: number; hasCover: boolean }
  icon?: ReactNode
  onClick: () => void
}) {
  const lead = cover !== undefined || icon !== undefined
  // A game's state goes under its name: beside it, it leaves a QAM row little room for the name.
  const below = cover !== undefined
  return (
    <PanelSectionRow>
      <ButtonItem layout="below" onClick={onClick}>
        <span style={lead ? { ...row, alignItems: 'center' } : row}>
          {cover && <Cover id={cover.id} size="small" hasCover={cover.hasCover} name={label} />}
          {icon}
          {below ? (
            <span style={stacked}>
              <span style={name}>{label}</span>
              {value !== undefined && value !== null && <span style={subline}>{value}</span>}
            </span>
          ) : (
            <span style={lead ? gameName : name}>{label}</span>
          )}
          {!below && value !== undefined && <span style={count}>{value}</span>}
        </span>
      </ButtonItem>
    </PanelSectionRow>
  )
}

export function Back({ onBack }: { onBack: () => void }) {
  return (
    <PanelSectionRow>
      <ButtonItem layout="below" onClick={onBack}>
        ← Back
      </ButtonItem>
    </PanelSectionRow>
  )
}

function Problem({ error }: { error: RpcError | null }) {
  if (!error) return null
  return (
    <PanelSectionRow>
      <Field label="⚠ Couldn't load" description={error.message} />
    </PanelSectionRow>
  )
}

/** A download's short state: Queued, Converting, Waiting, its progress, or Failed. */
export function downloadLabel(d: DownloadItem): string {
  if (d.state === 'queued') return 'Queued'
  if (d.state === 'failed') return 'Failed'
  if (d.converting) return 'Converting'
  if (d.waitingForGame) return 'Waiting'
  return `${percent(d)}%`
}

/**
 * A game's state for its row: its download's state while in the queue, "Installed" once on this
 * Deck, and null while it's only on RomM.
 */
export function stateLabel(
  game: { id: number; state: GameState; removed?: boolean },
  downloads: Downloads,
): string | null {
  const d = downloads.items.find((i) => i.id === game.id)
  if (d) return downloadLabel(d)
  if (game.removed) return 'Removed from RomM'
  if (game.state === 'failed') return 'Failed'
  if (game.state === 'installed') return 'Installed'
  if (game.state === 'update_available') return '⟳ Update'
  if (game.state === 'deleting') return 'Deleting…'
  if (game.state === 'on_deck') return 'On Deck'
  return null
}

/** Says the server is down while the library shows from the local copy. */
export function OfflineBanner() {
  const connection = useConnection()
  const catalog = useCatalog()
  const offline = connection?.state === 'offline' || catalog?.problem?.data?.kind === 'unreachable'
  if (!offline) return null
  return (
    <PanelSectionRow>
      <Field label="⚠ Offline" description="Server unreachable, showing cached library." />
    </PanelSectionRow>
  )
}

/** The library's line on the QAM home: what it holds, and the way into browsing it. */
export function LibrarySection({
  onBrowse,
  onCollections,
  onSearch,
}: {
  onBrowse: () => void
  onCollections: () => void
  onSearch: () => void
}) {
  const catalog = useCatalog()
  const connection = useConnection()
  if (!catalog || !connection || connection.state === 'not_set_up') return null

  let status: string
  if (catalog.refreshing) {
    status = catalog.fetched > 0 ? `Refreshing… ${catalog.fetched} games` : 'Refreshing…'
  } else if (catalog.syncedAt) {
    status = `Updated ${timeAgo(catalog.syncedAt)}`
  } else {
    status = 'Not loaded yet'
  }
  const failed = catalog.problem && catalog.problem.data?.kind !== 'unreachable'

  return (
    <PanelSection title="Library">
      <OfflineBanner />
      <PanelSectionRow>
        <Field
          label={`${catalog.games} games`}
          description={failed ? `Refresh failed: ${catalog.problem?.message}` : status}
        >
          {catalog.platforms} platforms
        </Field>
      </PanelSectionRow>
      {catalog.games > 0 && (
        <PanelSectionRow>
          <ButtonItem
            layout="below"
            onClick={() => {
              Navigation.CloseSideMenus()
              Navigation.Navigate(LIBRARY_ROUTE)
            }}
          >
            <IconLabel icon={<FaBookOpen />}>Open RomM library</IconLabel>
          </ButtonItem>
        </PanelSectionRow>
      )}
      <PanelSectionRow>
        <ButtonItem
          layout="below"
          disabled={catalog.refreshing || connection.state === 'signed_out'}
          onClick={() => void refreshLibrary(true)}
        >
          <IconLabel icon={<FaSyncAlt />}>Refresh library</IconLabel>
        </ButtonItem>
      </PanelSectionRow>
      {catalog.games > 0 && (
        <>
          <Row label="Search" icon={<FaSearch />} onClick={onSearch} />
          <Row label="Quick browse" icon={<FaListUl />} onClick={onBrowse} />
          <Row label="Collections" icon={<FaLayerGroup />} onClick={onCollections} />
        </>
      )}
    </PanelSection>
  )
}

export function PlatformsView({
  onBack,
  onOpen,
}: {
  onBack: () => void
  onOpen: (platform: PlatformSummary) => void
}) {
  const catalog = useCatalog()
  const [platforms, setPlatforms] = useState<PlatformSummary[] | null>(null)
  const [error, setError] = useState<RpcError | null>(null)

  // Reloads whenever a refresh ends, so new platforms and counts show without leaving.
  const syncedAt = catalog?.syncedAt
  useEffect(() => {
    call('list_platforms').then(
      (r) => {
        setPlatforms(r.platforms)
        setError(null)
      },
      (err: unknown) => setError(asRpcError(err)),
    )
  }, [syncedAt])

  return (
    <PanelSection title="Platforms">
      <Back onBack={onBack} />
      <OfflineBanner />
      <Problem error={error} />
      {platforms?.map((p) => (
        <Row
          key={p.id}
          label={p.name}
          value={`${p.installed}/${p.games}`}
          onClick={() => onOpen(p)}
        />
      ))}
      {platforms?.length === 0 && (
        <PanelSectionRow>
          <Field label="No games yet" description="Refresh the library to load it from RomM." />
        </PanelSectionRow>
      )}
    </PanelSection>
  )
}

export function CollectionsView({
  onBack,
  onOpen,
}: {
  onBack: () => void
  onOpen: (collection: CollectionSummary) => void
}) {
  const catalog = useCatalog()
  const connection = useConnection()
  const [collections, setCollections] = useState<CollectionSummary[] | null>(null)
  const [error, setError] = useState<RpcError | null>(null)
  const off = connection?.disabledFeatures?.find((d) => d.feature === 'collections')

  // Reloads whenever a refresh ends, so new collections and counts show without leaving.
  const syncedAt = catalog?.syncedAt
  useEffect(() => {
    call('list_collections').then(
      (r) => {
        setCollections(r.collections)
        setError(null)
      },
      (err: unknown) => setError(asRpcError(err)),
    )
  }, [syncedAt])

  return (
    <PanelSection title="Collections">
      <Back onBack={onBack} />
      <OfflineBanner />
      <Problem error={error} />
      {collections?.map((c) => (
        <Row
          key={`${c.kind}-${c.id}`}
          label={c.favorite ? `★ ${c.name}` : c.name}
          value={`${c.installed}/${c.games}`}
          onClick={() => onOpen(c)}
        />
      ))}
      {collections?.length === 0 && (
        <PanelSectionRow>
          {off ? (
            <Field label="Collections are off" description={disabledReason(off)} />
          ) : (
            <Field
              label="No collections"
              description="Collections you make in RomM, and smart collections, show here after the next refresh."
            />
          )}
        </PanelSectionRow>
      )}
    </PanelSection>
  )
}

export const FILTERS: { filter: GameFilter; label: string }[] = [
  { filter: 'all', label: 'All' },
  { filter: 'installed', label: 'Installed' },
  { filter: 'not_installed', label: 'Not installed' },
  { filter: 'updates', label: 'Updates' },
]

const chip: CSSProperties = { minWidth: 0, padding: '4px 8px', fontSize: 12, flex: '1 1 auto' }

function FilterChips({
  value,
  onChange,
}: {
  value: GameFilter
  onChange: (f: GameFilter) => void
}) {
  return (
    <PanelSectionRow>
      <Focusable style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }} flow-children="horizontal">
        {FILTERS.map(({ filter, label }) => (
          <DialogButton
            key={filter}
            style={{
              ...chip,
              background: filter === value ? '#1a9fff' : undefined,
              color: filter === value ? '#fff' : undefined,
            }}
            onClick={() => onChange(filter)}
          >
            {label}
          </DialogButton>
        ))}
      </Focusable>
    </PanelSectionRow>
  )
}

/**
 * The Keep archives toggle of a platform whose emulators open archives. Other platforms always or
 * never extract, so they show nothing.
 */
function KeepArchivesToggle({
  platformId,
  result,
  onError,
}: {
  platformId: number
  result: ListGamesResult | null
  onError: (e: RpcError) => void
}) {
  // Holds the switch's position from the moment it's flipped, before list_games says so again.
  const [keep, setKeep] = useState<boolean | null>(null)
  if (result?.archives !== 'optional') return null
  const checked = keep ?? result.keepArchives
  return (
    <PanelSectionRow>
      <ToggleField
        label="Keep archives"
        description={
          checked
            ? 'Zips and 7z holding one file stay as they are. Applies to new downloads.'
            : 'Every zip and 7z is extracted. Applies to new downloads.'
        }
        checked={checked}
        onChange={(next) => {
          setKeep(next)
          call('set_keep_archives', { platformId, keep: next }).catch((err: unknown) => {
            setKeep(null)
            onError(asRpcError(err))
          })
        }}
      />
    </PanelSectionRow>
  )
}

/**
 * A platform's BIOS files: what's missing, with a way to download it, and files of the same name
 * that differ from RomM's, which the plugin leaves alone. With nudge, only what needs the user's
 * attention shows.
 */
export function BiosSection({ platformId, nudge }: { platformId: number; nudge?: boolean }) {
  const { bios } = usePlatformBios(platformId)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<RpcError | null>(null)
  // A platform RomM has no BIOS for, or a status that didn't load (offline, no permission), shows
  // nothing: the QAM home says why the server or the feature is off.
  if (!bios || bios.files.length === 0) return null

  if (bios.inEmulator) {
    if (nudge) return null
    return (
      <PanelSectionRow>
        <Field
          label="BIOS"
          description="EmuDeck keeps this platform's firmware inside its emulator. Install it from there."
        >
          {bios.files.length} on RomM
        </Field>
      </PanelSectionRow>
    )
  }

  const missing = bios.files.filter((f) => f.status === 'missing')
  const different = bios.files.filter((f) => f.status === 'different')
  const names = (fs: typeof missing) => fs.map((f) => f.fileName).join(', ')
  const download = async () => {
    setBusy(true)
    try {
      await call('download_bios', { platformId })
      setError(null)
    } catch (err) {
      setError(asRpcError(err))
    }
    setBusy(false)
  }

  return (
    <>
      <Problem error={error} />
      {missing.length > 0 && (
        <>
          <PanelSectionRow>
            <Field label="⚠ BIOS missing" description={names(missing)} />
          </PanelSectionRow>
          <PanelSectionRow>
            <ButtonItem
              layout="below"
              disabled={busy || bios.downloading}
              onClick={() => void download()}
            >
              {bios.downloading
                ? 'Downloading BIOS…'
                : `Download BIOS (${formatSize(missing.reduce((n, f) => n + f.sizeBytes, 0))})`}
            </ButtonItem>
          </PanelSectionRow>
        </>
      )}
      {different.length > 0 && (
        <PanelSectionRow>
          <Field
            label="⚠ Different BIOS"
            description={`A different file is already there: ${names(different)}. The plugin leaves it as it is.`}
          />
        </PanelSectionRow>
      )}
      {!nudge && missing.length === 0 && different.length === 0 && (
        <PanelSectionRow>
          <Field label="BIOS">All {bios.files.length} present</Field>
        </PanelSectionRow>
      )}
    </>
  )
}

/** Where a downloaded game's save sync stands, with Sync now. */
export function SavesSection({ id }: { id: number }) {
  const status = useSaveStatus(id)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<RpcError | null>(null)
  if (!status) return null

  if (!status.syncs) {
    return (
      <PanelSectionRow>
        <Field label="Saves" description={status.why}>
          Not synced
        </Field>
      </PanelSectionRow>
    )
  }

  const sync = async () => {
    setBusy(true)
    try {
      await call('sync_saves', { ids: [id] })
      setError(null)
    } catch (err) {
      setError(asRpcError(err))
    }
    setBusy(false)
  }
  const checked = status.checkedAt ? ` · checked ${timeAgo(status.checkedAt)}` : ''
  const conflict = status.conflict
  const keep = (choice: SaveChoice) => {
    if (!conflict) return
    const deck = choice === 'deck'
    showModal(
      <ConfirmModal
        strTitle={deck ? "Keep this Deck's save?" : "Use RomM's save?"}
        strDescription={
          deck
            ? "This Deck's save becomes RomM's newest, so other devices get it. RomM keeps its other versions."
            : "RomM's save replaces the one on this Deck. The plugin keeps a backup of this Deck's save first."
        }
        strOKButtonText={deck ? "Keep this Deck's" : "Use RomM's"}
        onOK={() => {
          setBusy(true)
          call('resolve_save_conflict', { id, keep: choice, serverSaveId: conflict.serverSaveId })
            .then(
              () => setError(null),
              (err: unknown) => {
                setError(asRpcError(err))
                // A stale choice shows RomM's newer save.
                refreshSaves()
              },
            )
            .finally(() => setBusy(false))
        }}
      />,
    )
  }

  return (
    <>
      <Problem error={error} />
      <PanelSectionRow>
        <Field label="Saves" description={saveStateDetail(status)}>
          {saveStateLabel(status)}
          {!status.syncing && checked}
        </Field>
      </PanelSectionRow>
      {conflict && conflict.serverSaveId > 0 && (
        <>
          <PanelSectionRow>
            <Field
              label="This Deck's save"
              description={
                conflict.parked ? 'A copy is also kept on RomM, outside its save slots.' : undefined
              }
            >
              {conflict.deckAt ? `Written ${timeAgo(conflict.deckAt)}` : 'Written'}
            </Field>
          </PanelSectionRow>
          <PanelSectionRow>
            <Field label="RomM's save">
              {conflict.serverAt ? `Written ${timeAgo(conflict.serverAt)}` : 'Written'}
            </Field>
          </PanelSectionRow>
          <PanelSectionRow>
            <ButtonItem
              layout="below"
              disabled={busy || status.syncing}
              onClick={() => keep('deck')}
            >
              {"Keep this Deck's save"}
            </ButtonItem>
          </PanelSectionRow>
          <PanelSectionRow>
            <ButtonItem
              layout="below"
              disabled={busy || status.syncing}
              onClick={() => keep('server')}
            >
              {"Use RomM's save"}
            </ButtonItem>
          </PanelSectionRow>
        </>
      )}
      <PanelSectionRow>
        <ButtonItem layout="below" disabled={busy || status.syncing} onClick={() => void sync()}>
          <IconLabel icon={<FaExchangeAlt />}>
            {status.syncing ? 'Syncing saves…' : 'Sync saves now'}
          </IconLabel>
        </ButtonItem>
      </PanelSectionRow>
      <PanelSectionRow>
        <Field
          childrenLayout="below"
          label="Save slot"
          description="The RomM slot this game's save syncs in. Others keep separate lines of saves."
        >
          <DialogButton
            style={{ minWidth: 0, width: '100%' }}
            disabled={busy || status.syncing}
            onClick={() => showSaveSlots(id)}
          >
            {status.slot ?? 'autosave'}
          </DialogButton>
        </Field>
      </PanelSectionRow>
      <PanelSectionRow>
        <Field
          childrenLayout="below"
          label="Save file"
          description={`Written by ${status.saver} · tagged ${status.emulator}`}
        >
          <span style={{ wordBreak: 'break-all' }}>{status.path}</span>
        </Field>
      </PanelSectionRow>
      <SaveHistory id={id} />
      <SaveBackups id={id} onError={setError} />
    </>
  )
}

const warn = { color: '#e0a030', flexShrink: 0 }
const bad = { color: '#d94141', flexShrink: 0 }
const plain = { flexShrink: 0 }

/** What a save activity entry did, as an icon; trouble is colored to stand out in a list. */
export function ActivityIcon({ kind }: { kind: string }) {
  switch (kind) {
    case 'uploaded':
      return <FaCloudUploadAlt style={plain} />
    case 'downloaded':
      return <FaCloudDownloadAlt style={plain} />
    case 'conflict':
      return <FaExclamationTriangle style={warn} />
    case 'resolved':
      return <FaCheckCircle style={plain} />
    case 'failed':
      return <FaTimesCircle style={bad} />
    case 'removed_on_server':
      return <FaTrashAlt style={warn} />
    case 'restored':
      return <FaHistory style={plain} />
    case 'slot':
      return <FaBookmark style={plain} />
    case 'deleted':
      return <FaTrashAlt style={plain} />
    default:
      return <FaExchangeAlt style={plain} />
  }
}

/** What save sync last did to a game's save. */
function SaveHistory({ id }: { id: number }) {
  const entries = useSaveActivity(id, 5)
  if (!entries || entries.length === 0) return null
  return (
    <>
      <PanelSectionRow>
        <Field label="Save history" bottomSeparator="none" />
      </PanelSectionRow>
      {/* No value beside the label, so the message under it gets the row's whole width. */}
      {entries.map((e) => (
        <PanelSectionRow key={e.id}>
          <Field
            icon={<ActivityIcon kind={e.kind} />}
            label={timeAgo(e.at)}
            description={e.message}
          />
        </PanelSectionRow>
      ))}
    </>
  )
}

/** The copies of a game's save the plugin set aside, each with Restore. */
function SaveBackups({ id, onError }: { id: number; onError: (e: RpcError | null) => void }) {
  const backups = useSaveBackups(id)
  if (!backups || backups.length === 0) return null

  const restore = (name: string, at: string) =>
    showModal(
      <ConfirmModal
        strTitle="Restore this backup?"
        strDescription={`The save from ${timeAgo(at)} replaces the one on this Deck, then syncs to RomM like any change. The save it replaces is backed up first.`}
        strOKButtonText="Restore"
        onOK={() => {
          call('restore_save_backup', { id, name }).then(
            () => onError(null),
            (err: unknown) => onError(asRpcError(err)),
          )
        }}
      />,
    )

  return (
    <>
      {backups.map((b, i) => (
        <PanelSectionRow key={b.name}>
          <Field label={i === 0 ? 'Save backups' : undefined} description={formatSize(b.sizeBytes)}>
            <DialogButton style={{ minWidth: 0 }} onClick={() => restore(b.name, b.at)}>
              {`Restore · ${timeAgo(b.at)}`}
            </DialogButton>
          </Field>
        </PanelSectionRow>
      ))}
    </>
  )
}

/**
 * The Install to picker of a platform EmuDeck has other ROM folders for: the system's own folder,
 * or an alt folder for another emulator, frontend variant or region.
 */
function InstallToPicker({
  platformId,
  result,
  onError,
}: {
  platformId: number
  result: ListGamesResult | null
  onError: (e: RpcError) => void
}) {
  // Holds the choice from the moment it's made, before list_games says so again.
  const [picked, setPicked] = useState<string | null>(null)
  const folders = result?.installFolders ?? []
  if (folders.length < 2) return null
  const current = picked ?? result?.installTo ?? folders[0].folder
  const chosen = folders.find((f) => f.folder === current)
  return (
    <PanelSectionRow>
      <DropdownItem
        label="Install to"
        description={
          chosen?.srmSkips
            ? `${chosen.path}. Steam ROM Manager doesn't add games from this folder to Steam. Applies to new downloads.`
            : `${chosen?.path ?? ''}. Applies to new downloads; games already installed stay where they are.`
        }
        rgOptions={folders.map((f) => ({
          data: f.folder,
          label: `${f.folder}${f.primary ? ' (default)' : ''}${f.srmSkips ? ' · not in Steam ROM Manager' : ''}`,
        }))}
        selectedOption={current}
        onChange={(o) => {
          const folder = o.data as string
          setPicked(folder)
          call('set_install_to', { platformId, folder }).catch((err: unknown) => {
            setPicked(null)
            onError(asRpcError(err))
          })
        }}
      />
    </PanelSectionRow>
  )
}

/** Whether a game in state belongs under filter, for narrowing search results like the list. */
function matchesFilter(state: GameState, filter: GameFilter): boolean {
  switch (filter) {
    case 'installed':
      return state === 'installed' || state === 'update_available'
    case 'not_installed':
      return state !== 'installed' && state !== 'update_available'
    case 'updates':
      return state === 'update_available'
    default:
      return true
  }
}

/** Where a game list comes from: a platform, or a collection, whose games span platforms. */
export type GameSource =
  | { kind: 'platform'; platform: PlatformSummary }
  | { kind: 'collection'; collection: CollectionSummary }

/** The list_games and download-all params that name source. */
function sourceParams(source: GameSource): { platformId?: number; collection?: CollectionRef } {
  if (source.kind === 'platform') return { platformId: source.platform.id }
  return { collection: { kind: source.collection.kind, id: source.collection.id } }
}

export function GamesView({
  source,
  filter,
  onFilter,
  onBack,
  onOpen,
}: {
  source: GameSource
  filter: GameFilter
  onFilter: (filter: GameFilter) => void
  onBack: () => void
  onOpen: (game: GameSummary) => void
}) {
  const catalog = useCatalog()
  const downloads = useDownloads()
  const [games, setGames] = useState<GameSummary[]>([])
  const [total, setTotal] = useState<number | null>(null)
  const [listing, setListing] = useState<ListGamesResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<RpcError | null>(null)
  const [query, setQuery] = useState('')
  const platform = source.kind === 'platform' ? source.platform : null
  const title = platform?.name ?? (source.kind === 'collection' ? source.collection.name : '')
  // Search narrows to a platform; a collection's list has no search, so its query stays blank.
  const search = useSearch(query, platform?.id)
  const hits = search.result?.games.filter((g) => matchesFilter(g.state, filter)) ?? []
  const params = sourceParams(source)
  // The params object is new each render; these name the same source while it stays.
  const { platformId, collection } = params
  const collectionKind = collection?.kind
  const collectionId = collection?.id

  const load = useCallback(
    (offset: number) =>
      call('list_games', {
        platformId,
        collection:
          collectionKind && collectionId !== undefined
            ? { kind: collectionKind, id: collectionId }
            : undefined,
        filter,
        offset,
        limit: PAGE_SIZE,
      }).then(
        (r) => {
          setGames((prev) => (offset === 0 ? r.games : [...prev, ...r.games]))
          setTotal(r.total)
          setListing(r)
          setError(null)
          setLoading(false)
        },
        (err: unknown) => {
          setError(asRpcError(err))
          setLoading(false)
        },
      ),
    [platformId, collectionKind, collectionId, filter],
  )
  const value = (g: GameSummary) => {
    const state = stateLabel(g, downloads) ?? formatSize(g.sizeBytes)
    return g.platformName ? `${g.platformName} · ${state}` : state
  }

  // Starts over when the filter changes, a refresh ends, or a download ends and moves a game
  // between filters.
  const syncedAt = catalog?.syncedAt
  const ended = downloads.ended
  useEffect(() => {
    void load(0)
  }, [load, syncedAt, ended])

  const more = () => {
    setLoading(true)
    void load(games.length)
  }

  return (
    <PanelSection title={title}>
      <Back onBack={onBack} />
      <OfflineBanner />
      <FilterChips
        value={filter}
        onChange={(f) => {
          setLoading(true)
          onFilter(f)
        }}
      />
      {platform && (
        <>
          <InstallToPicker platformId={platform.id} result={listing} onError={setError} />
          <KeepArchivesToggle platformId={platform.id} result={listing} onError={setError} />
          <BiosSection platformId={platform.id} />
          <PanelSectionRow>
            <ButtonItem
              layout="below"
              disabled={downloads.problem !== null}
              onClick={() => showFindGames(platform)}
            >
              Find my games
            </ButtonItem>
          </PanelSectionRow>
          <PanelSectionRow>
            <TextField
              label={`Search ${platform.name}`}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </PanelSectionRow>
        </>
      )}
      {search.blank ? (
        <>
          <Problem error={error} />
          {total !== null && (
            <PanelSectionRow>
              <Field label={`${total} games`} />
            </PanelSectionRow>
          )}
          {filter !== 'installed' && total !== null && total > 0 && (
            <PanelSectionRow>
              <ButtonItem
                layout="below"
                disabled={downloads.problem !== null}
                onClick={() => void confirmDownloadAll(params, setError)}
              >
                Download all missing
              </ButtonItem>
            </PanelSectionRow>
          )}
          {platform && filter === 'installed' && total !== null && total > 0 && (
            <PanelSectionRow>
              <ButtonItem
                layout="below"
                disabled={downloads.problem !== null}
                onClick={() =>
                  confirmDelete(
                    `all ${total} installed ${platform.name} games`,
                    { platformId: platform.id },
                    setError,
                  )
                }
              >
                Delete all installed
              </ButtonItem>
            </PanelSectionRow>
          )}
          {games.map((g) => (
            <Row key={g.id} label={g.name} value={value(g)} cover={g} onClick={() => onOpen(g)} />
          ))}
        </>
      ) : (
        <>
          <Problem error={search.error} />
          <SearchSummary search={search} count={hits.length} />
          {hits.map((g) => (
            <Row
              key={g.id}
              label={g.name}
              value={stateLabel(g, downloads) ?? formatSize(g.sizeBytes)}
              cover={g}
              onClick={() => onOpen(g)}
            />
          ))}
        </>
      )}
      {search.blank && total !== null && games.length < total && (
        <PanelSectionRow>
          <ButtonItem layout="below" disabled={loading} onClick={more}>
            {loading ? 'Loading…' : `Show more (${total - games.length} left)`}
          </ButtonItem>
        </PanelSectionRow>
      )}
    </PanelSection>
  )
}

/** How long typing must pause before a search runs, so each keystroke doesn't ask the server. */
const SEARCH_DELAY_MS = 350

export interface Search {
  /** Set while the query is blank, when there's nothing to show. */
  blank: boolean
  /** Set while a search for the current query hasn't answered. */
  searching: boolean
  result: SearchGamesResult | null
  error: RpcError | null
}

/** Searches the library for query, in one platform or every platform when platformId is unset. */
export function useSearch(query: string, platformId?: number): Search {
  const catalog = useCatalog()
  const downloads = useDownloads()
  // The latest answer, and the query it answers: while it isn't for the current query, a search
  // is pending.
  const [answer, setAnswer] = useState<{
    query: string
    result?: SearchGamesResult
    error?: RpcError
  } | null>(null)

  // Searches again when a refresh ends, so new games show up in the results, and when a download
  // ends, so states stay current.
  const syncedAt = catalog?.syncedAt
  useEffect(() => {
    if (query.trim() === '') return
    // A slower answer to an earlier query must not replace this one's.
    let current = true
    const timer = setTimeout(() => {
      call('search_games', { query, platformId }).then(
        (result) => {
          if (current) setAnswer({ query, result })
        },
        (err: unknown) => {
          if (current) setAnswer({ query, error: asRpcError(err) })
        },
      )
    }, SEARCH_DELAY_MS)
    return () => {
      current = false
      clearTimeout(timer)
    }
  }, [query, platformId, syncedAt, downloads.ended])

  const blank = query.trim() === ''
  const searching = !blank && answer?.query !== query
  return {
    blank,
    searching,
    result: blank ? null : (answer?.result ?? null),
    error: blank || searching ? null : (answer?.error ?? null),
  }
}

/** How many games a search found, or that it's still running. */
function SearchSummary({ search, count }: { search: Search; count: number }) {
  if (search.blank || (!search.searching && !search.result)) return null
  let label = 'Searching…'
  if (!search.searching) label = count === 1 ? '1 game' : `${count} games`
  return (
    <PanelSectionRow>
      <Field
        label={label}
        description={
          !search.searching && search.result?.local
            ? "RomM didn't answer, so these are name matches from the cached library."
            : undefined
        }
      />
    </PanelSectionRow>
  )
}

export function SearchView({
  query,
  onQuery,
  onBack,
  onOpen,
}: {
  query: string
  onQuery: (query: string) => void
  onBack: () => void
  onOpen: (game: SearchHit) => void
}) {
  const downloads = useDownloads()
  const search = useSearch(query)
  const result = search.result

  return (
    <PanelSection title="Search">
      <Back onBack={onBack} />
      <OfflineBanner />
      <PanelSectionRow>
        <TextField
          label="Title or ROM hash"
          value={query}
          onChange={(e) => onQuery(e.target.value)}
        />
      </PanelSectionRow>
      <Problem error={search.error} />
      <SearchSummary search={search} count={result?.games.length ?? 0} />
      {result?.games.map((g) => {
        const state = stateLabel(g, downloads)
        return (
          <Row
            key={g.id}
            label={g.name}
            value={state ? `${g.platformName} · ${state}` : g.platformName}
            cover={g}
            onClick={() => onOpen(g)}
          />
        )
      })}
    </PanelSection>
  )
}

const STATE_LABELS: Record<string, string> = {
  on_romm: 'On RomM',
  queued: 'Queued',
  downloading: 'Downloading',
  installed: 'Installed',
  deleting: 'Deleting…',
  failed: 'Download failed',
  on_deck: 'On Deck (not managed)',
  update_available: '⟳ Update available',
}

/** Where ES-DE cleans up the gamelist entries and media of games deleted from disk. */
const ESDE_CLEANUP_TIP =
  "ES-DE keeps its gamelist entries for deleted games, with playtime and favorites. To clear them, use ES-DE's Utilities → Orphaned data cleanup."

/** Asks before deleting games, then deletes them. what names them in the question. */
function confirmDelete(what: string, params: DeleteGamesParams, onError: (e: RpcError) => void) {
  showModal(
    <ConfirmModal
      strTitle={`Delete ${what}?`}
      strDescription={
        <>
          <p>
            The plugin deletes only files it downloaded and that are unchanged since. Saves are
            kept.
          </p>
          <p>{ESDE_CLEANUP_TIP}</p>
        </>
      }
      strOKButtonText="Delete"
      bDestructiveWarning
      onOK={() => {
        call('delete_games', params).catch((err: unknown) => onError(asRpcError(err)))
      }}
    />,
  )
}

/**
 * Asks before deleting one game, then deletes it. Its save can go too, into the game's backups, but
 * only once RomM holds it as it is on this Deck.
 */
function DeleteGameModal({
  id,
  name,
  onError,
  closeModal,
}: {
  id: number
  name: string
  onError: (e: RpcError) => void
  closeModal?: () => void
}) {
  const [deletion, setDeletion] = useState<SaveDeletion | null>(null)
  const [withSaves, setWithSaves] = useState(false)
  useEffect(() => {
    call('save_deletion', { id }).then(setDeletion, () => setDeletion(null))
  }, [id])

  return (
    <ConfirmModal
      strTitle={`Delete ${name}?`}
      strDescription={
        <>
          <p>
            The plugin deletes only files it downloaded and that are unchanged since.
            {withSaves ? '' : ' Saves are kept.'}
          </p>
          {deletion?.has && (
            <ToggleField
              label="Delete saves too"
              description={
                deletion.why ?? "The save moves into this game's backups. RomM keeps its copy."
              }
              checked={withSaves}
              disabled={!!deletion.why}
              onChange={setWithSaves}
            />
          )}
          <p>{ESDE_CLEANUP_TIP}</p>
        </>
      }
      strOKButtonText="Delete"
      bDestructiveWarning
      closeModal={closeModal}
      onOK={() => {
        call('delete_games', { ids: [id], saves: withSaves || undefined }).catch((err: unknown) =>
          onError(asRpcError(err)),
        )
      }}
    />
  )
}

/** Asks before the plugin stops managing a game, then stops. */
function confirmStopManaging(name: string, id: number, onError: (e: RpcError) => void) {
  showModal(
    <ConfirmModal
      strTitle={`Stop managing ${name}?`}
      strDescription="Its files stay on this Deck as they are, but the plugin won't update or delete them any more, and its saves stop syncing."
      strOKButtonText="Stop managing"
      onOK={() => {
        call('stop_managing', { id }).catch((err: unknown) => onError(asRpcError(err)))
      }}
    />,
  )
}

function games(n: number): string {
  return n === 1 ? '1 game' : `${n} games`
}

/**
 * Asks before queueing every missing game of a platform, with how many and how big. When they don't
 * all fit, offers to queue the ones that do, taken by name.
 */
export async function confirmDownloadAll(
  source: { platformId?: number; collection?: CollectionRef },
  onError: (e: RpcError) => void,
) {
  let plan
  try {
    plan = await call('plan_download_all', source)
  } catch (err) {
    onError(asRpcError(err))
    return
  }
  const queue = (onlyWhatFits: boolean) =>
    call('download_all', { ...source, onlyWhatFits }).then(
      (r) =>
        toaster.toast({
          title: `Queued ${games(r.queued)}`,
          body: r.failed > 0 ? `${games(r.failed)} didn't queue.` : plan.name,
        }),
      (err: unknown) => onError(asRpcError(err)),
    )
  const left =
    plan.unplaceable > 0
      ? ` ${games(plan.unplaceable)} the plugin can't place in EmuDeck ${plan.unplaceable === 1 ? 'is' : 'are'} left out.`
      : ''

  if (plan.count === 0) {
    showModal(
      <ConfirmModal
        bAlertDialog
        strTitle="Nothing to download"
        strDescription={`Every ${plan.name} game is on this Deck or in the queue.${left}`}
      />,
    )
    return
  }
  if (plan.fits) {
    showModal(
      <ConfirmModal
        strTitle={`Download ${games(plan.count)}?`}
        strDescription={`${formatSize(plan.totalBytes)} from RomM.${left}`}
        strOKButtonText="Download"
        onOK={() => void queue(false)}
      />,
    )
    return
  }
  const room = `They need ${formatSize(plan.needBytes)}, and ${formatSize(plan.freeBytes)} is free after the downloads already queued.`
  if (plan.fitCount === 0) {
    showModal(
      <ConfirmModal
        bAlertDialog
        strTitle="Not enough space"
        strDescription={`${room} None of them fits.`}
      />,
    )
    return
  }
  showModal(
    <ConfirmModal
      strTitle={`Not enough space for ${games(plan.count)}`}
      strDescription={`${room} Queue the ${games(plan.fitCount)} that fit (${formatSize(plan.fitBytes)}), by name?${left}`}
      strOKButtonText="Queue what fits"
      onOK={() => void queue(true)}
    />,
  )
}

const downloadName: CSSProperties = { flex: '1 1 auto', minWidth: 0, padding: '8px 12px' }
const iconButton: CSSProperties = { flex: '0 0 auto', minWidth: 0, width: 40, padding: '8px 0' }

/** What a download row says under its name: progress, the wait for a slot, or why it failed. */
function DownloadDetail({ d }: { d: DownloadItem }) {
  if (d.state === 'failed') {
    return (
      <PanelSectionRow>
        <Field label="⚠ Failed" description={d.problem?.message} />
      </PanelSectionRow>
    )
  }
  let text = `${d.platformName} · ${formatSize(d.receivedBytes)} of ${formatSize(d.totalBytes)}`
  if (d.state === 'queued') text = `${d.platformName} · waiting`
  if (d.converting) text = `${d.platformName} · converting on RomM`
  if (d.waitingForGame) text = `${d.platformName} · waiting for game to close`
  return (
    <PanelSectionRow>
      <ProgressBarWithInfo
        layout="below"
        nProgress={d.state === 'queued' || d.converting ? 0 : percent(d)}
        sOperationText={text}
      />
    </PanelSectionRow>
  )
}

/**
 * Asks before going back to the plugin's data from before its last update, then restores it and
 * restarts the backend to use it.
 */
function confirmRestoreState() {
  showModal(
    <ConfirmModal
      strTitle="Restore backup?"
      strDescription="The plugin's records are from a newer version of the plugin. Restoring brings back the copy it kept before that update: games downloaded or changed since may no longer be managed. The newer records are kept aside, for updating the plugin again."
      strOKButtonText="Restore"
      onOK={() => {
        call('restore_state_backup').then(
          () => restartBackend(),
          (err: unknown) =>
            toaster.toast({ title: "Couldn't restore", body: asRpcError(err).message }),
        )
      }}
    />,
  )
}

/** The download queue on the QAM home, or why downloading is off. Hidden while there's neither. */
export function DownloadsSection({ onOpen }: { onOpen: (id: number) => void }) {
  const downloads = useDownloads()
  if (downloads.items.length === 0 && !downloads.problem) return null
  return (
    <PanelSection title="Downloads">
      {downloads.problem && (
        <PanelSectionRow>
          <Field label="⚠ Downloads are off" description={downloads.problem.message} />
        </PanelSectionRow>
      )}
      {downloads.problem?.data?.kind === 'newer_data' && (
        <PanelSectionRow>
          <ButtonItem layout="below" onClick={confirmRestoreState}>
            Restore backup
          </ButtonItem>
        </PanelSectionRow>
      )}
      {downloads.held && (
        <PanelSectionRow>
          <Field label="⚠ On hold" description={downloads.held.message} />
        </PanelSectionRow>
      )}
      {downloads.items.map((d) => (
        <Fragment key={d.id}>
          <PanelSectionRow>
            <Focusable style={{ display: 'flex', gap: 4 }} flow-children="horizontal">
              <DialogButton style={downloadName} onClick={() => onOpen(d.id)}>
                <span style={row}>
                  <span style={name}>{d.name}</span>
                  <span style={count}>{downloadLabel(d)}</span>
                </span>
              </DialogButton>
              {d.state === 'failed' && (
                <DialogButton
                  style={iconButton}
                  aria-label={`Retry ${d.name}`}
                  onClick={() => void call('download_game', { id: d.id }).catch(() => {})}
                >
                  ↻
                </DialogButton>
              )}
              <DialogButton
                style={iconButton}
                aria-label={d.state === 'failed' ? `Dismiss ${d.name}` : `Cancel ${d.name}`}
                onClick={() => void call('cancel_download', { id: d.id }).catch(() => {})}
              >
                ✕
              </DialogButton>
            </Focusable>
          </PanelSectionRow>
          <DownloadDetail d={d} />
        </Fragment>
      ))}
    </PanelSection>
  )
}

/** Whether a game's page shows its saves: the plugin's own games, and games linked to RomM. */
export function showsSaves(game: Game, state?: GameState): boolean {
  return (
    state === 'installed' ||
    state === 'update_available' ||
    (state === 'on_deck' && !!game.linkedPath && !game.removed)
  )
}

/** Something a game's page offers to do with it. */
export interface GameAction {
  label: string
  run: () => void
  disabled: boolean
  /** Set on actions that remove something, which the page sets apart. */
  destructive?: boolean
}

export interface GameDetail {
  game: Game | null
  /** The game's download, while it's in the queue. */
  download?: DownloadItem
  /** The game's state, its download's while it has one. */
  state?: GameState
  /** The state as the page says it, with a running download's progress. */
  stateText: string
  /** What can be done with the game in its state. */
  actions: GameAction[]
  /** What the page says about the game's file when it's On Deck (not managed). */
  notice?: string
  /** What the page says when RomM no longer has the game. */
  removed?: string
  error: RpcError | null
}

/** How often a check of an On Deck file still hashing asks again. */
const CHECK_POLL_MS = 1500

/** Compares an On Deck game's file with RomM's while state says it's there, asking again while it's hashed. */
function useOnDeckCheck(id: number, state: GameState | undefined, reload: number) {
  const [check, setCheck] = useState<{ id: number; result?: OnDeckCheck; error?: RpcError } | null>(
    null,
  )

  useEffect(() => {
    if (state !== 'on_deck') return
    let current = true
    let timer: ReturnType<typeof setTimeout> | undefined
    const ask = () => {
      call('check_on_deck', { id }).then(
        (result) => {
          if (!current) return
          setCheck({ id, result })
          if (result.checking) timer = setTimeout(ask, CHECK_POLL_MS)
        },
        (err: unknown) => {
          if (current) setCheck({ id, error: asRpcError(err) })
        },
      )
    }
    ask()
    return () => {
      current = false
      clearTimeout(timer)
    }
  }, [id, state, reload])

  return check?.id === id && state === 'on_deck' ? check : null
}

/** A game and what can be done with it, both pages' shared view of it. */
export function useGameDetail(id: number): GameDetail {
  const downloads = useDownloads()
  const [game, setGame] = useState<Game | null>(null)
  const [error, setError] = useState<RpcError | null>(null)
  const [busy, setBusy] = useState(false)

  // Reloads when a download ends, which may be this game's.
  useEffect(() => {
    call('get_game', { id }).then(setGame, (err: unknown) => setError(asRpcError(err)))
  }, [id, downloads.ended])

  const download = downloads.items.find((d) => d.id === id)
  const state = download?.state ?? game?.state
  // RomM has nothing left to compare a removed game's file with.
  const check = useOnDeckCheck(id, game?.removed ? undefined : state, downloads.ended)

  const act = async (action: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await action()
      setError(null)
    } catch (err) {
      setError(asRpcError(err))
    }
    setBusy(false)
  }
  const downloadOff = downloads.problem !== null
  const get = () => void act(() => call('download_game', { id }))
  const cancel = () => void act(() => call('cancel_download', { id }))

  const ownedActions = (game: Game): GameAction[] => [
    {
      label: 'Stop managing',
      run: () => confirmStopManaging(game.name, id, setError),
      disabled: busy,
    },
    {
      label: 'Delete ROM',
      run: () => showModal(<DeleteGameModal id={id} name={game.name} onError={setError} />),
      disabled: downloadOff,
      destructive: true,
    },
  ]

  const unlink: GameAction = {
    label: 'Unlink',
    run: () => void act(() => call('unlink_game', { id })),
    disabled: busy,
  }

  const actions: GameAction[] = []
  if (game?.removed) {
    if (state === 'installed' || state === 'update_available') actions.push(...ownedActions(game))
    else if (game.linkedPath) actions.push(unlink)
  } else if (game) {
    switch (state) {
      case 'on_romm':
        actions.push({ label: 'Download', run: get, disabled: busy || downloadOff })
        break
      case 'queued':
      case 'downloading':
        actions.push({ label: 'Cancel', run: cancel, disabled: busy })
        break
      case 'failed':
        actions.push({ label: 'Retry', run: get, disabled: busy || downloadOff })
        actions.push({ label: 'Dismiss', run: cancel, disabled: busy })
        break
      case 'update_available':
        actions.push({ label: 'Update', run: get, disabled: busy || downloadOff })
        actions.push({
          label: 'Ignore this update',
          run: () => void act(() => call('ignore_update', { id })),
          disabled: busy,
        })
        actions.push(...ownedActions(game))
        break
      case 'installed':
        actions.push(...ownedActions(game))
        break
      case 'on_deck':
        if (check?.result?.matches) {
          actions.push({
            label: 'Adopt',
            run: () => void act(() => call('adopt_game', { id })),
            disabled: busy,
          })
        }
        if (game.linkedPath) {
          actions.push(unlink)
        } else {
          actions.push({
            label: 'Link to RomM',
            run: () => void act(() => call('link_game', { id })),
            disabled: busy,
          })
        }
        break
    }
  }

  let removed: string | undefined
  if (game?.removed) {
    if (state === 'installed' || state === 'update_available') {
      removed =
        'RomM no longer has this game. The copy the plugin downloaded stays on this Deck until you delete it, and its saves no longer sync.'
    } else if (game.linkedPath) {
      removed = `RomM no longer has this game. The file at ${game.linkedPath} stays yours, and its saves no longer sync.`
    } else {
      removed = 'RomM no longer has this game.'
    }
  }

  let notice: string | undefined
  if (state === 'on_deck' && !game?.removed) {
    const path = game?.linkedPath ?? check?.result?.path
    const where = path ? ` at ${path}` : ''
    if (game?.linkedPath) {
      notice = `Linked to RomM, so its saves sync. The file${where} stays yours: the plugin never deletes or replaces it.`
    } else if (check?.error) {
      notice = `There's a copy of this game the plugin didn't download${where}. It couldn't be compared with RomM's: ${check.error.message}`
    } else if (!check?.result || check.result.checking) {
      notice = `There's a copy of this game the plugin didn't download${where}. Comparing it with RomM's…`
    } else if (check.result.matches) {
      notice = `There's a copy of this game the plugin didn't download${where}. It matches RomM's, so the plugin can adopt it and manage it from now on.`
    } else {
      notice = `There's a copy of this game the plugin didn't download${where}. ${check.result.why ?? ''} The plugin won't replace it, but you can link it to RomM.`
    }
  }

  let stateText = state ? (STATE_LABELS[state] ?? state) : ''
  if (download?.converting) stateText = 'Converting on RomM…'
  else if (download?.waitingForGame) stateText = 'Waiting for game to close'
  else if (download?.state === 'downloading') stateText = `Downloading ${percent(download)}%`
  else if (game?.removed) stateText = `${stateText} · Removed from RomM`

  return { game, download, state, stateText, actions, notice, removed, error }
}

export function GameView({ id, onBack }: { id: number; onBack: () => void }) {
  const { game, download, state, stateText, actions, notice, removed, error } = useGameDetail(id)

  return (
    <PanelSection title={game?.name ?? 'Game'}>
      <Back onBack={onBack} />
      <Problem error={error} />
      {game && state && (
        <>
          {game.hasCover && (
            <PanelSectionRow>
              <div style={{ display: 'flex', justifyContent: 'center', padding: '4px 0' }}>
                <Cover id={game.id} size="large" hasCover />
              </div>
            </PanelSectionRow>
          )}
          <PanelSectionRow>
            <Field label="State">{stateText}</Field>
          </PanelSectionRow>
          {removed && (
            <PanelSectionRow>
              <Field label="⚠ Removed from RomM" description={removed} />
            </PanelSectionRow>
          )}
          {notice && (
            <PanelSectionRow>
              <Field label="Not downloaded by the plugin" description={notice} />
            </PanelSectionRow>
          )}
          {(state === 'installed' || state === 'update_available') && game.installNote && (
            <PanelSectionRow>
              <Field label="⚠ Not ready to play" description={game.installNote} />
            </PanelSectionRow>
          )}
          <BiosSection platformId={game.platformId} nudge />
          {showsSaves(game, state) && <SavesSection id={game.id} />}
          {download?.state === 'downloading' && !download.converting && (
            <PanelSectionRow>
              <ProgressBarWithInfo
                layout="below"
                nProgress={percent(download)}
                sOperationText={`${formatSize(download.receivedBytes)} of ${formatSize(download.totalBytes)}`}
              />
            </PanelSectionRow>
          )}
          {state === 'failed' && download?.problem && (
            <PanelSectionRow>
              <Field label="⚠ Error" description={download.problem.message} />
            </PanelSectionRow>
          )}
          {actions.map((a) => (
            <PanelSectionRow key={a.label}>
              <ButtonItem layout="below" disabled={a.disabled} onClick={a.run}>
                {a.label}
              </ButtonItem>
            </PanelSectionRow>
          ))}
          {game.installedPaths?.map((p) => (
            <PanelSectionRow key={p}>
              <Field label="Installed at">
                <span style={{ wordBreak: 'break-all' }}>{p}</span>
              </Field>
            </PanelSectionRow>
          ))}
          <PanelSectionRow>
            <Field label="Platform">{game.platformName}</Field>
          </PanelSectionRow>
          <PanelSectionRow>
            <Field label="Size">{formatSize(game.sizeBytes)}</Field>
          </PanelSectionRow>
          {game.regions.length > 0 && (
            <PanelSectionRow>
              <Field label="Region">{game.regions.join(', ')}</Field>
            </PanelSectionRow>
          )}
          {game.revision && (
            <PanelSectionRow>
              <Field label="Revision">{game.revision}</Field>
            </PanelSectionRow>
          )}
          <PanelSectionRow>
            <Field
              label="File"
              description={game.multiFile ? 'A folder of several files' : undefined}
            >
              <span style={{ wordBreak: 'break-all' }}>{game.fileName}</span>
            </Field>
          </PanelSectionRow>
        </>
      )}
    </PanelSection>
  )
}
