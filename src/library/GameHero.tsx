import { DialogButton, Focusable } from '@decky/ui'
import type { CSSProperties, ReactNode } from 'react'

import { formatSize } from '../catalog'
import { Cover } from '../covers'
import { percent } from '../downloads'
import { ErrorBox, mono, muted } from '../setup/common'
import { BiosSection, useGameDetail } from './Library'

const hero: CSSProperties = {
  position: 'relative',
  display: 'flex',
  gap: 40,
  padding: '16px 48px',
  overflow: 'hidden',
  // Fills the screen, so the backdrop does too however little the game has to show.
  minHeight: '100%',
  boxSizing: 'border-box',
}
const backdrop: CSSProperties = {
  position: 'absolute',
  top: -80,
  left: -80,
  width: 'calc(100% + 160px)',
  height: 'calc(100% + 160px)',
  borderRadius: 0,
  opacity: 0.25,
  filter: 'blur(48px)',
  pointerEvents: 'none',
}
const coverStyle: CSSProperties = {
  width: 280,
  height: 'auto',
  aspectRatio: '3 / 4',
  borderRadius: 6,
}
const info: CSSProperties = {
  position: 'relative',
  flex: 1,
  minWidth: 0,
  maxWidth: 640,
  display: 'flex',
  flexDirection: 'column',
  gap: 12,
}
const actionButton: CSSProperties = { width: 320 }
const progressTrack: CSSProperties = {
  width: 320,
  height: 6,
  borderRadius: 3,
  background: 'rgba(255, 255, 255, 0.15)',
  overflow: 'hidden',
}

function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div style={{ display: 'flex', gap: 12, fontSize: 14 }}>
      <span style={{ ...muted, width: 96, flex: 'none' }}>{label}</span>
      <span style={{ minWidth: 0 }}>{children}</span>
    </div>
  )
}

/** A game's full-screen page: its cover, what it is, where it stands, and what can be done with it. */
export function GameHero({ id }: { id: number }) {
  const { game, download, state, stateText, actions, error } = useGameDetail(id)

  if (!game) {
    return (
      <div style={{ padding: '16px 48px' }}>
        {error ? <ErrorBox>{error.message}</ErrorBox> : <span style={muted}>Loading…</span>}
      </div>
    )
  }

  const facts = [
    game.platformName,
    game.regions.join(', '),
    game.revision,
    formatSize(game.sizeBytes),
  ]
  const downloading = download?.state === 'downloading' && !download.converting

  return (
    <div style={hero}>
      {game.hasCover && <Cover id={game.id} size="large" hasCover style={backdrop} />}
      {actions.length > 0 ? (
        <div style={{ position: 'relative', flex: 'none' }}>
          <Cover id={game.id} size="large" hasCover={game.hasCover} style={coverStyle} />
        </div>
      ) : (
        // With no action to take focus, the cover holds it, so B still reaches the page.
        <Focusable
          style={{ position: 'relative', flex: 'none' }}
          noFocusRing
          preferredFocus
          onActivate={() => undefined}
        >
          <Cover id={game.id} size="large" hasCover={game.hasCover} style={coverStyle} />
        </Focusable>
      )}
      <div style={info}>
        <div style={{ fontSize: 32, fontWeight: 700, color: '#fff', lineHeight: 1.15 }}>
          {game.name}
        </div>
        <div style={muted}>{facts.filter(Boolean).join(' · ')}</div>
        <div style={{ fontSize: 18, color: '#fff' }}>{stateText}</div>
        {downloading && download && (
          <div style={progressTrack}>
            <div
              style={{ width: `${percent(download)}%`, height: '100%', background: '#1a9fff' }}
            />
          </div>
        )}
        {downloading && download && (
          <div style={muted}>
            {formatSize(download.receivedBytes)} of {formatSize(download.totalBytes)}
          </div>
        )}
        {state === 'failed' && download?.problem && <ErrorBox>{download.problem.message}</ErrorBox>}
        {state === 'installed' && game.installNote && (
          <ErrorBox>{`Not ready to play: ${game.installNote}`}</ErrorBox>
        )}
        {error && <ErrorBox>{error.message}</ErrorBox>}
        {actions.length > 0 && (
          <Focusable style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {actions.map((a, i) => (
              <DialogButton
                key={a.label}
                style={a.destructive ? { ...actionButton, background: '#a3302f' } : actionButton}
                preferredFocus={i === 0}
                disabled={a.disabled}
                onClick={a.run}
              >
                {a.label}
              </DialogButton>
            ))}
          </Focusable>
        )}
        <div style={{ margin: '0 -16px' }}>
          <BiosSection platformId={game.platformId} nudge />
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 4 }}>
          {game.installedPaths?.map((p) => (
            <Detail key={p} label="Installed at">
              <span style={mono}>{p}</span>
            </Detail>
          ))}
          <Detail label="File">
            <span style={mono}>{game.fileName}</span>
            {game.multiFile && <span style={muted}> · a folder of several files</span>}
          </Detail>
        </div>
      </div>
    </div>
  )
}
