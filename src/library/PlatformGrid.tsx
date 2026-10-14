import { DialogButton, Focusable } from '@decky/ui'
import { type CSSProperties, useCallback, useEffect, useRef, useState } from 'react'

import { call } from '../backend'
import { useCatalog } from '../catalog'
import { useDownloads } from '../downloads'
import type {
  GameFilter,
  GameSort,
  GameSummary,
  PlatformSummary,
  Error as RpcError,
} from '../rpc/schema.gen'
import { asRpcError, ErrorBox, muted } from '../setup/common'
import { showFindGames } from './FindGames'
import { BiosSection, confirmDownloadAll, FILTERS } from './Library'
import { ActionTile, GameTile, grid } from './tiles'

/** How many games each step of the grid loads. */
const PAGE_SIZE = 60

/**
 * How close to the end of what's loaded focus may get before the next page loads: two rows, so
 * the D-pad rarely waits.
 */
const PRELOAD = 14

export const SORTS: { sort: GameSort; label: string }[] = [
  { sort: 'name', label: 'Name' },
  { sort: 'added', label: 'Date added' },
  { sort: 'size', label: 'Size' },
]

const toolbar: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 8,
  padding: '0 32px',
}
const chip: CSSProperties = {
  width: 'auto',
  minWidth: 0,
  padding: '6px 14px',
  borderRadius: 14,
  fontSize: 13,
  whiteSpace: 'nowrap',
}
const chipOn: CSSProperties = { ...chip, background: '#dcdedf', color: '#111' }
const actionButton: CSSProperties = {
  width: 'auto',
  minWidth: 0,
  padding: '8px 16px',
  fontSize: 14,
  whiteSpace: 'nowrap',
}
const title: CSSProperties = {
  fontSize: 24,
  fontWeight: 700,
  color: '#fff',
  minWidth: 0,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
}

/** A platform's every game as a grid of covers, filtered and sorted, loading more as focus nears the end. */
export function PlatformGrid({
  platform,
  filter,
  sort,
  onFilter,
  onSort,
  onGame,
}: {
  platform: PlatformSummary
  filter: GameFilter
  sort: GameSort
  onFilter: (filter: GameFilter) => void
  onSort: (sort: GameSort) => void
  onGame: (game: GameSummary) => void
}) {
  const catalog = useCatalog()
  const downloads = useDownloads()
  const [games, setGames] = useState<GameSummary[]>([])
  const [total, setTotal] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<RpcError | null>(null)
  // Counts the loads started, so an answer to a filter or sort since left behind is dropped.
  const latest = useRef(0)

  const load = useCallback(
    (offset: number) => {
      const seq = ++latest.current
      call('list_games', { platformId: platform.id, filter, sort, offset, limit: PAGE_SIZE }).then(
        (r) => {
          if (seq !== latest.current) return
          setGames((prev) => (offset === 0 ? r.games : [...prev, ...r.games]))
          setTotal(r.total)
          setError(null)
          setLoading(false)
        },
        (err: unknown) => {
          if (seq !== latest.current) return
          setError(asRpcError(err))
          setLoading(false)
        },
      )
    },
    [platform.id, filter, sort],
  )

  // Starts over when the filter or sort changes, a refresh ends, or a download ends and moves a
  // game between filters.
  const syncedAt = catalog?.syncedAt
  useEffect(() => {
    load(0)
  }, [load, syncedAt, downloads.ended])

  const hasMore = total !== null && games.length < total
  const more = () => {
    if (loading || !hasMore) return
    setLoading(true)
    load(games.length)
  }
  const sortLabel = SORTS.find((s) => s.sort === sort)?.label ?? 'Name'
  const nextSort = SORTS[(SORTS.findIndex((s) => s.sort === sort) + 1) % SORTS.length].sort

  return (
    <div>
      {/* The platform and what to do with it on top, how to narrow the grid under it. */}
      <Focusable style={{ ...toolbar, paddingBottom: 8 }} flow-children="horizontal">
        <span style={title}>{platform.name}</span>
        <span style={{ ...muted, flexShrink: 0 }}>
          {total === null ? '' : total === 1 ? '1 game' : `${total} games`}
        </span>
        <span style={{ flex: 1 }} />
        <DialogButton
          style={actionButton}
          disabled={downloads.problem !== null}
          onClick={() => showFindGames(platform)}
        >
          Find my games
        </DialogButton>
        {filter !== 'installed' && total !== null && total > 0 && (
          <DialogButton
            style={actionButton}
            disabled={downloads.problem !== null}
            onClick={() => void confirmDownloadAll({ platformId: platform.id }, setError)}
          >
            Download all missing
          </DialogButton>
        )}
      </Focusable>
      <Focusable style={toolbar} flow-children="horizontal">
        {FILTERS.map(({ filter: f, label }) => (
          <DialogButton key={f} style={f === filter ? chipOn : chip} onClick={() => onFilter(f)}>
            {label}
          </DialogButton>
        ))}
        <DialogButton style={chip} onClick={() => onSort(nextSort)}>
          Sort: {sortLabel}
        </DialogButton>
      </Focusable>
      <div style={{ maxWidth: 720, padding: '4px 16px 0' }}>
        <BiosSection platformId={platform.id} nudge />
      </div>
      {error && (
        <div style={{ padding: '8px 32px 0' }}>
          <ErrorBox>{error.message}</ErrorBox>
        </div>
      )}
      {total === 0 && !loading && (
        <div style={{ ...muted, padding: '16px 32px' }}>No games match this filter.</div>
      )}
      <Focusable style={grid}>
        {games.map((g, i) => (
          <GameTile
            key={g.id}
            game={g}
            onOpen={() => onGame(g)}
            onFocus={i >= games.length - PRELOAD ? more : undefined}
          />
        ))}
        {hasMore && (
          <ActionTile
            label={loading ? 'Loading…' : 'Show more'}
            detail={`${(total ?? 0) - games.length} left`}
            onOpen={more}
          />
        )}
      </Focusable>
    </div>
  )
}
