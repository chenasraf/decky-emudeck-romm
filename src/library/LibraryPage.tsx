import { DialogButton, Field, Focusable, Navigation, TextField } from '@decky/ui'
import { type CSSProperties, type ReactNode, useEffect, useRef, useState } from 'react'

import { call } from '../backend'
import { onOpenGame, refreshLibrary, takePendingGame, useCatalog } from '../catalog'
import { SETUP_ROUTE, useConnection } from '../connection'
import { percent, useDownloads } from '../downloads'
import type {
  GameFilter,
  GameSort,
  LibraryShelves,
  PlatformSummary,
  Error as RpcError,
} from '../rpc/schema.gen'
import { asRpcError, ErrorBox, muted, routeScroll } from '../setup/common'
import { GameHero } from './GameHero'
import { downloadLabel, OfflineBanner, useSearch } from './Library'
import { PlatformGrid } from './PlatformGrid'
import { ActionTile, GameTile, grid, Tile, tileCss } from './tiles'

/** How many games each shelf shows; a platform's shelf ends in a See all tile for the rest. */
const SHELF_SIZE = 8

/** Where the library is: its home of shelves, a platform's grid, or a game. */
type Place =
  | { name: 'home' }
  | {
      name: 'platform'
      platform: PlatformSummary
      filter: GameFilter
      sort: GameSort
      game?: number
    }
  | { name: 'game'; id: number }

const pageStyle: CSSProperties = { ...routeScroll, padding: '16px 0 48px', color: '#dcdedf' }
const header: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 16,
  padding: '0 32px 8px',
}
const shelfTitle: CSSProperties = {
  display: 'flex',
  alignItems: 'baseline',
  gap: 10,
  padding: '0 32px',
  fontSize: 16,
  fontWeight: 700,
  color: '#fff',
}
const shelfRow: CSSProperties = {
  display: 'flex',
  gap: 14,
  overflowX: 'auto',
  scrollbarWidth: 'none',
  padding: '10px 32px 14px',
}

function Shelf({
  title,
  detail,
  children,
}: {
  title: string
  detail?: string
  children: ReactNode
}) {
  return (
    <div style={{ marginTop: 8 }}>
      <div style={shelfTitle}>
        {title}
        {detail && (
          <span style={{ fontSize: 12, fontWeight: 400, color: '#8b929a' }}>{detail}</span>
        )}
      </div>
      <Focusable className="erm-row" style={shelfRow} flow-children="horizontal">
        {children}
      </Focusable>
    </div>
  )
}

/** The home's shelves, loaded again when a refresh or a download ends. */
function useShelves(): { shelves: LibraryShelves | null; error: RpcError | null } {
  const catalog = useCatalog()
  const downloads = useDownloads()
  const [shelves, setShelves] = useState<LibraryShelves | null>(null)
  const [error, setError] = useState<RpcError | null>(null)

  const syncedAt = catalog?.syncedAt
  useEffect(() => {
    let current = true
    call('library_shelves', { limit: SHELF_SIZE }).then(
      (r) => {
        if (!current) return
        setShelves(r)
        setError(null)
      },
      (err: unknown) => {
        if (current) setError(asRpcError(err))
      },
    )
    return () => {
      current = false
    }
  }, [syncedAt, downloads.ended])

  return { shelves, error }
}

function Shelves({
  onGame,
  onPlatform,
}: {
  onGame: (id: number) => void
  onPlatform: (platform: PlatformSummary) => void
}) {
  const downloads = useDownloads()
  const { shelves, error } = useShelves()

  return (
    <>
      {error && (
        <div style={{ padding: '0 32px' }}>
          <ErrorBox>{`Couldn't load the library: ${error.message}`}</ErrorBox>
        </div>
      )}
      {downloads.items.length > 0 && (
        <Shelf title="Downloading" detail={downloads.held?.message}>
          {downloads.items.map((d) => (
            <Tile
              key={d.id}
              name={d.name}
              sub={`${d.platformName} · ${downloadLabel(d)}`}
              cover={{ id: d.id, hasCover: true }}
              progress={d.state === 'downloading' && !d.converting ? percent(d) : undefined}
              onOpen={() => onGame(d.id)}
            />
          ))}
        </Shelf>
      )}
      {shelves && shelves.updates.length > 0 && (
        <Shelf
          title="Updates available"
          detail={shelves.updatesTotal === 1 ? '1 game' : `${shelves.updatesTotal} games`}
        >
          {shelves.updates.map((g) => (
            <GameTile key={g.id} game={g} withPlatform onOpen={() => onGame(g.id)} />
          ))}
        </Shelf>
      )}
      {shelves && shelves.recent.length > 0 && (
        <Shelf title="Recently added">
          {shelves.recent.map((g) => (
            <GameTile key={g.id} game={g} withPlatform onOpen={() => onGame(g.id)} />
          ))}
        </Shelf>
      )}
      {shelves && shelves.installed.length > 0 && (
        <Shelf
          title="Installed on this Deck"
          detail={shelves.installedTotal === 1 ? '1 game' : `${shelves.installedTotal} games`}
        >
          {shelves.installed.map((g) => (
            <GameTile key={g.id} game={g} withPlatform onOpen={() => onGame(g.id)} />
          ))}
        </Shelf>
      )}
      {shelves?.platforms.map(({ platform, games }) => (
        <Shelf
          key={platform.id}
          title={platform.name}
          detail={`${platform.installed}/${platform.games} installed`}
        >
          {games.map((g) => (
            <GameTile key={g.id} game={g} onOpen={() => onGame(g.id)} />
          ))}
          {platform.games > games.length && (
            <ActionTile
              label={`See all ${platform.games} →`}
              detail="filter · sort · download all"
              onOpen={() => onPlatform(platform)}
            />
          )}
        </Shelf>
      ))}
    </>
  )
}

function SearchResults({ query, onGame }: { query: string; onGame: (id: number) => void }) {
  const search = useSearch(query)
  const games = search.result?.games ?? []

  let summary = 'Searching…'
  if (!search.searching) summary = games.length === 1 ? '1 game' : `${games.length} games`
  return (
    <>
      <div style={{ ...muted, padding: '0 32px' }}>
        {summary}
        {!search.searching && search.result?.local
          ? " · RomM didn't answer, so these are name matches from the cached library."
          : ''}
      </div>
      {search.error && (
        <div style={{ padding: '0 32px' }}>
          <ErrorBox>{search.error.message}</ErrorBox>
        </div>
      )}
      <Focusable style={grid}>
        {games.map((g) => (
          <GameTile key={g.id} game={g} withPlatform onOpen={() => onGame(g.id)} />
        ))}
      </Focusable>
    </>
  )
}

/** The header's server line: where the library comes from, and whether it answers. */
function ServerLine() {
  const connection = useConnection()
  if (!connection?.serverUrl) return null
  const up = connection.state === 'connected'
  return (
    <span style={{ ...muted, display: 'flex', alignItems: 'center', gap: 6 }}>
      <span
        style={{
          width: 8,
          height: 8,
          borderRadius: 4,
          background: up ? '#59bf40' : '#e8b04b',
        }}
      />
      {connection.serverUrl}
    </span>
  )
}

export function LibraryPage() {
  const catalog = useCatalog()
  const connection = useConnection()
  const [place, setPlace] = useState<Place>(() => {
    const id = takePendingGame()
    return id === null ? { name: 'home' } : { name: 'game', id }
  })
  const [searching, setSearching] = useState(false)
  const [query, setQuery] = useState('')
  // Bumped by each Y press, so the search field mounts again and takes focus.
  const [searchFocus, setSearchFocus] = useState(0)
  // The element that had focus when each open level was opened, and the one Back hands focus to
  // once the level under it shows again.
  const opener = useRef<Element[]>([])
  const refocus = useRef<Element | null>(null)

  useEffect(() => {
    void refreshLibrary()
  }, [])

  // A game opened from the Quick Access Menu replaces whatever the library shows, and Back goes home.
  useEffect(
    () =>
      onOpenGame((id) => {
        opener.current = []
        setSearching(false)
        setQuery('')
        setPlace({ name: 'game', id })
      }),
    [],
  )

  useEffect(() => {
    const el = refocus.current
    refocus.current = null
    if (!(el instanceof HTMLElement) || !el.isConnected) return
    el.focus()
    el.scrollIntoView({ block: 'center', inline: 'nearest' })
  }, [place])

  const open = (next: Place) => {
    if (document.activeElement) opener.current.push(document.activeElement)
    setPlace(next)
  }
  const close = (next: Place) => {
    refocus.current = opener.current.pop() ?? null
    setPlace(next)
  }
  const openSearch = () => {
    opener.current = []
    setPlace({ name: 'home' })
    setSearching(true)
    setSearchFocus((n) => n + 1)
  }
  const back = () => {
    if (place.name === 'platform' && place.game !== undefined) {
      close({ ...place, game: undefined })
    } else if (place.name !== 'home') {
      close({ name: 'home' })
    } else if (searching) {
      setSearching(false)
      setQuery('')
    } else {
      Navigation.NavigateBack()
    }
  }
  const home = place.name === 'home'

  let body: ReactNode
  if (connection?.state === 'not_set_up') {
    body = (
      <div style={{ padding: '0 32px' }}>
        <Field label="RomM isn't set up" description="Connect a RomM server to browse its library.">
          <DialogButton onClick={() => Navigation.Navigate(SETUP_ROUTE)}>Set up RomM</DialogButton>
        </Field>
      </div>
    )
  } else if (catalog && catalog.games === 0) {
    body = (
      <div style={{ ...muted, padding: '0 32px' }}>
        {catalog.refreshing
          ? 'Loading the library from RomM…'
          : 'The library is empty. Refresh it from the Quick Access Menu once RomM has games.'}
      </div>
    )
  } else {
    body = (
      <>
        <div style={{ display: searching && query.trim() !== '' ? 'none' : undefined }}>
          <Shelves
            onGame={(id) => open({ name: 'game', id })}
            onPlatform={(platform) =>
              open({ name: 'platform', platform, filter: 'all', sort: 'name' })
            }
          />
        </div>
        {searching && query.trim() !== '' && (
          <SearchResults query={query} onGame={(id) => open({ name: 'game', id })} />
        )}
      </>
    )
  }

  return (
    <Focusable
      style={pageStyle}
      onCancelButton={back}
      onCancelActionDescription="Back"
      onOptionsButton={openSearch}
      onOptionsActionDescription="Search"
    >
      <style>{tileCss}</style>
      {/* Kept mounted while a platform or game is open, so Back returns to the same shelf. */}
      <div style={{ display: home ? undefined : 'none' }}>
        <Focusable style={header} flow-children="horizontal">
          <span style={{ fontSize: 24, fontWeight: 700, color: '#fff' }}>RomM</span>
          {searching ? (
            <div style={{ flex: '0 1 360px' }}>
              <TextField
                key={searchFocus}
                focusOnMount
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
          ) : (
            <DialogButton style={{ width: 'auto', minWidth: 0 }} onClick={openSearch}>
              Search
            </DialogButton>
          )}
          <span style={{ flex: 1 }} />
          <ServerLine />
        </Focusable>
        <div style={{ padding: '0 16px' }}>
          <OfflineBanner />
        </div>
        {body}
      </div>
      {place.name === 'platform' && (
        <>
          {/* Kept mounted while a game is open, so Back returns to the same place in the grid. */}
          <div style={{ display: place.game === undefined ? undefined : 'none' }}>
            <PlatformGrid
              platform={place.platform}
              filter={place.filter}
              sort={place.sort}
              onFilter={(filter) => setPlace((p) => ({ ...p, filter }))}
              onSort={(sort) => setPlace((p) => ({ ...p, sort }))}
              onGame={(game) => open({ ...place, game: game.id })}
            />
          </div>
          {place.game !== undefined && <GameHero id={place.game} />}
        </>
      )}
      {place.name === 'game' && <GameHero id={place.id} />}
    </Focusable>
  )
}
