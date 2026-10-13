import { Focusable } from '@decky/ui'
import type { CSSProperties, ReactNode } from 'react'

import { Cover } from '../covers'
import { percent, useDownloads } from '../downloads'
import type { GameSummary } from '../rpc/schema.gen'
import { stateLabel } from './Library'

export const TILE_WIDTH = 140
export const TILE_GAP = 14

// Steam's focus engine marks the focused element with focusClassName; :focus-visible covers a
// keyboard in the dev shell.
export const tileCss = `
.erm-tile { outline: none; transition: transform 0.1s; }
.erm-tile.erm-focus, .erm-tile:focus-visible { transform: scale(1.05); }
.erm-tile.erm-focus .erm-art, .erm-tile:focus-visible .erm-art { box-shadow: 0 0 0 3px #fff; }
.erm-row::-webkit-scrollbar { display: none; }
`

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
const action: CSSProperties = {
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

export const grid: CSSProperties = {
  display: 'flex',
  flexWrap: 'wrap',
  gap: TILE_GAP,
  padding: '10px 32px',
}

/** Keeps the focused tile in view as the D-pad moves. */
function reveal(el: Element) {
  el.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'smooth' })
}

function TileFrame({
  onOpen,
  onFocus,
  children,
}: {
  onOpen: () => void
  onFocus?: () => void
  children: ReactNode
}) {
  return (
    <Focusable
      className="erm-tile"
      focusClassName="erm-focus"
      style={tile}
      onActivate={onOpen}
      onClick={onOpen}
      onGamepadFocus={(e) => {
        reveal(e.currentTarget as Element)
        onFocus?.()
      }}
    >
      {children}
    </Focusable>
  )
}

export function Tile({
  name,
  sub,
  cover,
  progress,
  onOpen,
  onFocus,
}: {
  name: string
  sub?: string
  /** The game whose cover fills the tile. */
  cover?: { id: number; hasCover: boolean }
  /** A running download's percentage, drawn over the cover's foot. */
  progress?: number
  onOpen: () => void
  onFocus?: () => void
}) {
  return (
    <TileFrame onOpen={onOpen} onFocus={onFocus}>
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
    </TileFrame>
  )
}

/** A game's tile, with its state and, while it downloads, its progress. */
export function GameTile({
  game,
  withPlatform,
  onOpen,
  onFocus,
}: {
  game: GameSummary
  /** Names the game's platform, where the tiles around it mix platforms. */
  withPlatform?: boolean
  onOpen: () => void
  onFocus?: () => void
}) {
  const downloads = useDownloads()
  const d = downloads.items.find((i) => i.id === game.id)
  const parts = [withPlatform ? game.platformName : undefined, stateLabel(game, downloads)]
  return (
    <Tile
      name={game.name}
      sub={parts.filter(Boolean).join(' · ') || undefined}
      cover={game}
      progress={d?.state === 'downloading' && !d.converting ? percent(d) : undefined}
      onOpen={onOpen}
      onFocus={onFocus}
    />
  )
}

/** A tile that does something rather than open a game: See all, Show more. */
export function ActionTile({
  label,
  detail,
  onOpen,
}: {
  label: string
  detail?: string
  onOpen: () => void
}) {
  return (
    <TileFrame onOpen={onOpen}>
      <div className="erm-art" style={action}>
        <span>{label}</span>
        {detail && (
          <span style={{ fontSize: 11, fontWeight: 400, color: '#8b929a' }}>{detail}</span>
        )}
      </div>
    </TileFrame>
  )
}
