import { DialogButton, Field, Focusable, Navigation, TextField } from '@decky/ui'
import { type CSSProperties, type ReactNode, useEffect, useState } from 'react'

import { call } from '../backend'
import { refreshLibrary, useCatalog } from '../catalog'
import { SETUP_ROUTE, useConnection } from '../connection'
import { Cover } from '../covers'
import { percent, useDownloads } from '../downloads'
import type {
  GameFilter,
  GameSummary,
  LibraryShelves,
  PlatformSummary,
  Error as RpcError,
} from '../rpc/schema.gen'
import { asRpcError, ErrorBox, muted } from '../setup/common'
import { downloadLabel, GamesView, GameView, OfflineBanner, stateLabel, useSearch } from './Library'

/** How many games each shelf shows; a platform's shelf ends in a See all tile for the rest. */
const SHELF_SIZE = 8

/** Where the library is: its home of shelves, a platform's full list, or a game. */
type Place =
  | { name: 'home' }
  | { name: 'platform'; platform: PlatformSummary; filter: GameFilter; game?: number }
  | { name: 'game'; id: number }

const TILE_WIDTH = 140

// Steam's focus engine marks the focused element with focusClassName; :focus covers a mouse and
// keyboard in the dev shell.
const css = `
.erm-tile { outline: none; transition: transform 0.1s; }
.erm-tile.erm-focus, .erm-tile:focus-visible { transform: scale(1.05); }
.erm-tile.erm-focus .erm-art, .erm-tile:focus-visible .erm-art { box-shadow: 0 0 0 3px #fff; }
.erm-row::-webkit-scrollbar { display: none; }
`

const pageStyle: CSSProperties = { padding: '56px 0 32px', color: '#dcdedf' }
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
const tile: CSSProperties = { flex: 'none', width: TILE_WIDTH, cursor: 'pointer' }
const art: CSSProperties = {
  position: 'relative',
  width: TILE_WIDTH,
  aspectRatio: '3 / 4',
  borderRadius: 4,
  overflow: 'hidden',
}
const coverFill: CSSProperties = { width: '100%', height: '100%', borderRadius: 0 }
const tileName: CSSProperties = {
  marginTop: 6,
  fontSize: 13,
  color: '#fff',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
}
const tileSub: CSSProperties = { ...tileName, marginTop: 2, fontSize: 11, color: '#8b929a' }
const progressTrack: CSSProperties = {
  position: 'absolute',
  left: 0,
  right: 0,
  bottom: 0,
  height: 6,
  background: 'rgba(0, 0, 0, 0.6)',
}
const seeAll: CSSProperties = {
  ...art,
  display: 'flex',
  flexDirection: 'column',
  alignItems: 'center',
  justifyContent: 'center',
  gap: 6,
  background: '#1a2029',
  fontWeight: 600,
  textAlign: 'center',
}
const grid: CSSProperties = { display: 'flex', flexWrap: 'wrap', gap: 14, padding: '10px 32px' }
const column: CSSProperties = { maxWidth: 720, padding: '0 16px' }

/** Keeps the focused tile in view as the D-pad moves along a shelf. */
function reveal(el: Element) {
  el.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'smooth' })
}

function Tile({
  name,
  sub,
  cover,
  progress,
  onOpen,
}: {
  name: string
  sub?: string
  /** The game whose cover fills the tile. */
  cover?: { id: number; hasCover: boolean }
  /** A running download's percentage, drawn over the cover's foot. */
  progress?: number
  onOpen: () => void
}) {
  return (
    <Focusable
      className="erm-tile"
      focusClassName="erm-focus"
      style={tile}
      onActivate={onOpen}
      onClick={onOpen}
      onGamepadFocus={(e) => reveal(e.currentTarget as Element)}
    >
      <div className="erm-art" style={art}>
        {cover && <Cover id={cover.id} size="small" hasCover={cover.hasCover} style={coverFill} />}
        {progress !== undefined && (
          <div style={progressTrack}>
            <div style={{ width: `${progress}%`, height: '100%', background: '#1a9fff' }} />
          </div>
        )}
      </div>
      <div style={tileName}>{name}</div>
      {sub && <div style={tileSub}>{sub}</div>}
    </Focusable>
  )
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

/** A game tile's second line: its platform where the shelf mixes them, and its state. */
function subOf(game: GameSummary, label: string | null, withPlatform: boolean): string | undefined {
  const parts = [withPlatform ? game.platformName : undefined, label ?? undefined]
  return parts.filter(Boolean).join(' · ') || undefined
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

  const gameTile = (g: GameSummary, withPlatform: boolean) => {
    const d = downloads.items.find((i) => i.id === g.id)
    return (
      <Tile
        key={g.id}
        name={g.name}
        sub={subOf(g, stateLabel(g, downloads), withPlatform)}
        cover={g}
        progress={d?.state === 'downloading' && !d.converting ? percent(d) : undefined}
        onOpen={() => onGame(g.id)}
      />
    )
  }

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
      {shelves && shelves.recent.length > 0 && (
        <Shelf title="Recently added">{shelves.recent.map((g) => gameTile(g, true))}</Shelf>
      )}
      {shelves && shelves.installed.length > 0 && (
        <Shelf
          title="Installed on this Deck"
          detail={shelves.installedTotal === 1 ? '1 game' : `${shelves.installedTotal} games`}
        >
          {shelves.installed.map((g) => gameTile(g, true))}
        </Shelf>
      )}
      {shelves?.platforms.map(({ platform, games }) => (
        <Shelf
          key={platform.id}
          title={platform.name}
          detail={`${platform.installed}/${platform.games} installed`}
        >
          {games.map((g) => gameTile(g, false))}
          {platform.games > games.length && (
            <Focusable
              className="erm-tile"
              focusClassName="erm-focus"
              style={tile}
              onActivate={() => onPlatform(platform)}
              onClick={() => onPlatform(platform)}
              onGamepadFocus={(e) => reveal(e.currentTarget as Element)}
            >
              <div className="erm-art" style={seeAll}>
                <span>See all {platform.games} →</span>
                <span style={{ fontSize: 11, fontWeight: 400, color: '#8b929a' }}>
                  filter · download all
                </span>
              </div>
            </Focusable>
          )}
        </Shelf>
      ))}
    </>
  )
}

function SearchResults({ query, onGame }: { query: string; onGame: (id: number) => void }) {
  const downloads = useDownloads()
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
          <Tile
            key={g.id}
            name={g.name}
            sub={subOf(g, stateLabel(g, downloads), true)}
            cover={g}
            onOpen={() => onGame(g.id)}
          />
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
  const [place, setPlace] = useState<Place>({ name: 'home' })
  const [searching, setSearching] = useState(false)
  const [query, setQuery] = useState('')
  // Bumped by each Y press, so the search field mounts again and takes focus.
  const [searchFocus, setSearchFocus] = useState(0)

  useEffect(() => {
    void refreshLibrary()
  }, [])

  const openSearch = () => {
    setPlace({ name: 'home' })
    setSearching(true)
    setSearchFocus((n) => n + 1)
  }
  const back = () => {
    if (place.name === 'platform' && place.game !== undefined) {
      setPlace({ ...place, game: undefined })
    } else if (place.name !== 'home') {
      setPlace({ name: 'home' })
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
            onGame={(id) => setPlace({ name: 'game', id })}
            onPlatform={(platform) => setPlace({ name: 'platform', platform, filter: 'all' })}
          />
        </div>
        {searching && query.trim() !== '' && (
          <SearchResults query={query} onGame={(id) => setPlace({ name: 'game', id })} />
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
      <style>{css}</style>
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
        <div style={column}>
          {/* Kept mounted while a game is open, so Back returns to the same place in the list. */}
          <div style={{ display: place.game === undefined ? undefined : 'none' }}>
            <GamesView
              source={{ kind: 'platform', platform: place.platform }}
              filter={place.filter}
              onFilter={(filter) => setPlace({ ...place, filter })}
              onBack={back}
              onOpen={(game) => setPlace({ ...place, game: game.id })}
            />
          </div>
          {place.game !== undefined && <GameView id={place.game} onBack={back} />}
        </div>
      )}
      {place.name === 'game' && (
        <div style={column}>
          <GameView id={place.id} onBack={back} />
        </div>
      )}
    </Focusable>
  )
}
