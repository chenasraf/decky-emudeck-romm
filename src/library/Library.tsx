import {
  ButtonItem,
  DialogButton,
  Field,
  Focusable,
  PanelSection,
  PanelSectionRow,
} from '@decky/ui'
import { type CSSProperties, type ReactNode, useCallback, useEffect, useState } from 'react'

import { call } from '../backend'
import { formatSize, refreshLibrary, timeAgo, useCatalog } from '../catalog'
import { useConnection } from '../connection'
import type {
  Game,
  GameFilter,
  GameSummary,
  PlatformSummary,
  Error as RpcError,
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
export function LibrarySection({ onBrowse }: { onBrowse: () => void }) {
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
      {catalog.games > 0 && <Row label="Browse library" onClick={onBrowse} />}
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
  const [games, setGames] = useState<GameSummary[]>([])
  const [total, setTotal] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<RpcError | null>(null)

  const load = useCallback(
    (offset: number) =>
      call('list_games', { platformId: platform.id, filter, offset, limit: PAGE_SIZE }).then(
        (r) => {
          setGames((prev) => (offset === 0 ? r.games : [...prev, ...r.games]))
          setTotal(r.total)
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

  // Starts over when the filter changes or a refresh ends.
  const syncedAt = catalog?.syncedAt
  useEffect(() => {
    void load(0)
  }, [load, syncedAt])

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
      <Problem error={error} />
      {total !== null && (
        <PanelSectionRow>
          <Field label={`${total} games`} />
        </PanelSectionRow>
      )}
      {games.map((g) => (
        <Row key={g.id} label={g.name} value={formatSize(g.sizeBytes)} onClick={() => onOpen(g)} />
      ))}
      {total !== null && games.length < total && (
        <PanelSectionRow>
          <ButtonItem layout="below" disabled={loading} onClick={more}>
            {loading ? 'Loading…' : `Show more (${total - games.length} left)`}
          </ButtonItem>
        </PanelSectionRow>
      )}
    </PanelSection>
  )
}

const STATE_LABELS: Record<string, string> = {
  on_romm: 'On RomM',
}

export function GameView({ id, onBack }: { id: number; onBack: () => void }) {
  const [game, setGame] = useState<Game | null>(null)
  const [error, setError] = useState<RpcError | null>(null)

  useEffect(() => {
    call('get_game', { id }).then(setGame, (err: unknown) => setError(asRpcError(err)))
  }, [id])

  return (
    <PanelSection title={game?.name ?? 'Game'}>
      <Back onBack={onBack} />
      <Problem error={error} />
      {game && (
        <>
          <PanelSectionRow>
            <Field label="State">{STATE_LABELS[game.state] ?? game.state}</Field>
          </PanelSectionRow>
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
