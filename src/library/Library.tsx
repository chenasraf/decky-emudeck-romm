import {
  ButtonItem,
  ConfirmModal,
  DialogButton,
  Field,
  Focusable,
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

import { call } from '../backend'
import { usePlatformBios } from '../bios'
import { formatSize, refreshLibrary, timeAgo, useCatalog } from '../catalog'
import { useConnection } from '../connection'
import { type Downloads, percent, useDownloads } from '../downloads'
import type {
  DeleteGamesParams,
  DownloadItem,
  Game,
  GameFilter,
  GameState,
  GameSummary,
  ListGamesResult,
  PlatformSummary,
  Error as RpcError,
  SearchGamesResult,
  SearchHit,
} from '../rpc/schema.gen'
import { asRpcError } from '../setup/common'

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

function Row({ label, value, onClick }: { label: string; value?: ReactNode; onClick: () => void }) {
  return (
    <PanelSectionRow>
      <ButtonItem layout="below" onClick={onClick}>
        <span style={row}>
          <span style={name}>{label}</span>
          {value !== undefined && <span style={count}>{value}</span>}
        </span>
      </ButtonItem>
    </PanelSectionRow>
  )
}

function Back({ onBack }: { onBack: () => void }) {
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

/** A download's short state: Queued, Converting, its progress, or Failed. */
function downloadLabel(d: DownloadItem): string {
  if (d.state === 'queued') return 'Queued'
  if (d.state === 'failed') return 'Failed'
  if (d.converting) return 'Converting'
  return `${percent(d)}%`
}

/**
 * A game's state for its row: its download's state while in the queue, "Installed" once on this
 * Deck, and null while it's only on RomM.
 */
function stateLabel(game: { id: number; state: GameState }, downloads: Downloads): string | null {
  const d = downloads.items.find((i) => i.id === game.id)
  if (d) return downloadLabel(d)
  if (game.state === 'failed') return 'Failed'
  if (game.state === 'installed') return 'Installed'
  if (game.state === 'deleting') return 'Deleting…'
  return null
}

/** Says the server is down while the library shows from the local copy. */
function OfflineBanner() {
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
  onSearch,
}: {
  onBrowse: () => void
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
        <>
          <Row label="Search" onClick={onSearch} />
          <Row label="Browse library" onClick={onBrowse} />
        </>
      )}
      <PanelSectionRow>
        <ButtonItem
          layout="below"
          disabled={catalog.refreshing || connection.state === 'signed_out'}
          onClick={() => void refreshLibrary(true)}
        >
          Refresh library
        </ButtonItem>
      </PanelSectionRow>
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

const FILTERS: { filter: GameFilter; label: string }[] = [
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
function BiosSection({ platformId, nudge }: { platformId: number; nudge?: boolean }) {
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

/** Whether a game in state belongs under filter, for narrowing search results like the list. */
function matchesFilter(state: GameState, filter: GameFilter): boolean {
  switch (filter) {
    case 'installed':
      return state === 'installed'
    case 'not_installed':
      return state !== 'installed'
    case 'updates':
      return false
    default:
      return true
  }
}

export function GamesView({
  platform,
  filter,
  onFilter,
  onBack,
  onOpen,
}: {
  platform: PlatformSummary
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
  const search = useSearch(query, platform.id)
  const hits = search.result?.games.filter((g) => matchesFilter(g.state, filter)) ?? []

  const load = useCallback(
    (offset: number) =>
      call('list_games', { platformId: platform.id, filter, offset, limit: PAGE_SIZE }).then(
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
    [platform.id, filter],
  )

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
    <PanelSection title={platform.name}>
      <Back onBack={onBack} />
      <OfflineBanner />
      <FilterChips
        value={filter}
        onChange={(f) => {
          setLoading(true)
          onFilter(f)
        }}
      />
      <KeepArchivesToggle platformId={platform.id} result={listing} onError={setError} />
      <BiosSection platformId={platform.id} />
      <PanelSectionRow>
        <TextField
          label={`Search ${platform.name}`}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </PanelSectionRow>
      {search.blank ? (
        <>
          <Problem error={error} />
          {total !== null && (
            <PanelSectionRow>
              <Field label={`${total} games`} />
            </PanelSectionRow>
          )}
          {filter === 'installed' && total !== null && total > 0 && (
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
            <Row
              key={g.id}
              label={g.name}
              value={stateLabel(g, downloads) ?? formatSize(g.sizeBytes)}
              onClick={() => onOpen(g)}
            />
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

interface Search {
  /** Set while the query is blank, when there's nothing to show. */
  blank: boolean
  /** Set while a search for the current query hasn't answered. */
  searching: boolean
  result: SearchGamesResult | null
  error: RpcError | null
}

/** Searches the library for query, in one platform or every platform when platformId is unset. */
function useSearch(query: string, platformId?: number): Search {
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
  return (
    <PanelSectionRow>
      <ProgressBarWithInfo
        nProgress={d.state === 'queued' || d.converting ? 0 : percent(d)}
        sOperationText={text}
      />
    </PanelSectionRow>
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

export function GameView({ id, onBack }: { id: number; onBack: () => void }) {
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

  return (
    <PanelSection title={game?.name ?? 'Game'}>
      <Back onBack={onBack} />
      <Problem error={error} />
      {game && state && (
        <>
          <PanelSectionRow>
            <Field label="State">
              {download?.converting
                ? 'Converting on RomM…'
                : download?.state === 'downloading'
                  ? `Downloading ${percent(download)}%`
                  : (STATE_LABELS[state] ?? state)}
            </Field>
          </PanelSectionRow>
          {state === 'installed' && game.installNote && (
            <PanelSectionRow>
              <Field label="⚠ Not ready to play" description={game.installNote} />
            </PanelSectionRow>
          )}
          <BiosSection platformId={game.platformId} nudge />
          {download?.state === 'downloading' && !download.converting && (
            <PanelSectionRow>
              <ProgressBarWithInfo
                nProgress={percent(download)}
                sOperationText={`${formatSize(download.receivedBytes)} of ${formatSize(download.totalBytes)}`}
              />
            </PanelSectionRow>
          )}
          {state === 'failed' && (
            <>
              {download?.problem && (
                <PanelSectionRow>
                  <Field label="⚠ Error" description={download.problem.message} />
                </PanelSectionRow>
              )}
              <PanelSectionRow>
                <ButtonItem
                  layout="below"
                  disabled={busy || downloads.problem !== null}
                  onClick={() => void act(() => call('download_game', { id }))}
                >
                  Retry
                </ButtonItem>
              </PanelSectionRow>
              <PanelSectionRow>
                <ButtonItem
                  layout="below"
                  disabled={busy}
                  onClick={() => void act(() => call('cancel_download', { id }))}
                >
                  Dismiss
                </ButtonItem>
              </PanelSectionRow>
            </>
          )}
          {state === 'on_romm' && (
            <PanelSectionRow>
              <ButtonItem
                layout="below"
                disabled={busy || downloads.problem !== null}
                onClick={() => void act(() => call('download_game', { id }))}
              >
                Download
              </ButtonItem>
            </PanelSectionRow>
          )}
          {(state === 'queued' || state === 'downloading') && (
            <PanelSectionRow>
              <ButtonItem
                layout="below"
                disabled={busy}
                onClick={() => void act(() => call('cancel_download', { id }))}
              >
                Cancel
              </ButtonItem>
            </PanelSectionRow>
          )}
          {state === 'installed' && (
            <PanelSectionRow>
              <ButtonItem
                layout="below"
                disabled={downloads.problem !== null}
                onClick={() => confirmDelete(game.name, { ids: [id] }, setError)}
              >
                Delete ROM
              </ButtonItem>
            </PanelSectionRow>
          )}
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
