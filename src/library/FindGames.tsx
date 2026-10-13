import { DialogButton, Focusable, ModalRoot, ProgressBarWithInfo, showModal } from '@decky/ui'
import { type CSSProperties, useEffect, useState } from 'react'

import { call, onEvent } from '../backend'
import { useDownloads } from '../downloads'
import type {
  FindGamesStatus,
  FoundGame,
  PlatformSummary,
  Error as RpcError,
} from '../rpc/schema.gen'
import { asRpcError, ErrorBox, mono, muted } from '../setup/common'

const body: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 12, minWidth: 480 }
const foundRow: CSSProperties = { display: 'flex', alignItems: 'center', gap: 8 }
const foundText: CSSProperties = { flex: '1 1 auto', minWidth: 0 }
const button: CSSProperties = { width: 'auto', minWidth: 0, padding: '6px 14px' }

/** Opens Find my games for a platform. */
export function showFindGames(platform: PlatformSummary) {
  showModal(<FindGamesModal platform={platform} />)
}

/** Keeps a platform's Find my games status current from its events and from adopts and links. */
function useFindStatus(platformId: number) {
  const downloads = useDownloads()
  const [status, setStatus] = useState<FindGamesStatus | null>(null)
  const [error, setError] = useState<RpcError | null>(null)

  useEffect(
    () =>
      onEvent('find_games', (s) => {
        if (s.platformId === platformId) setStatus(s)
      }),
    [platformId],
  )
  // Adopting or linking a game takes it off the list.
  useEffect(() => {
    call('find_games_status', { platformId }).then(setStatus, (err: unknown) =>
      setError(asRpcError(err)),
    )
  }, [platformId, downloads.ended])

  return { status, setStatus, error, setError }
}

function FindGamesModal({
  platform,
  closeModal,
}: {
  platform: PlatformSummary
  closeModal?: () => void
}) {
  const { status, setStatus, error, setError } = useFindStatus(platform.id)
  const [busy, setBusy] = useState<number | null>(null)

  const start = () => {
    setError(null)
    call('find_games', { platformId: platform.id }).then(setStatus, (err: unknown) =>
      setError(asRpcError(err)),
    )
  }
  const act = async (id: number, action: 'adopt_game' | 'link_game') => {
    setBusy(id)
    try {
      await call(action, { id })
      setError(null)
    } catch (err) {
      setError(asRpcError(err))
    }
    setBusy(null)
  }

  const running = status?.running ?? false
  const problem = error ?? status?.problem ?? null

  return (
    <ModalRoot closeModal={closeModal}>
      <div style={body}>
        <div style={{ fontSize: 22, fontWeight: 700 }}>Find my games · {platform.name}</div>
        <div style={muted}>
          Hashes the files in this platform&apos;s ROM folder and matches them against RomM, to find
          games you keep under other names. Nothing changes until you adopt or link a game.
        </div>
        {problem && <ErrorBox>{problem.message}</ErrorBox>}
        {running && status && (
          <ProgressBarWithInfo
            nProgress={status.total > 0 ? Math.floor((status.hashed / status.total) * 100) : 0}
            sOperationText={
              status.total > 0
                ? `Hashing ${status.hashed} of ${status.total} files…`
                : 'Reading RomM’s files…'
            }
          />
        )}
        {!running && status?.done && status.found.length === 0 && (
          <div style={muted}>No games of RomM&apos;s turned up under other names.</div>
        )}
        {!running && status && status.found.length > 0 && (
          <div style={muted}>
            Adopt hands a game to the plugin, to update and delete like one it downloaded. Link to
            RomM syncs its saves while the file stays yours.
          </div>
        )}
        <Focusable style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {status?.found.map((g) => (
            <FoundRow
              key={g.id}
              game={g}
              busy={busy !== null}
              onAdopt={() => void act(g.id, 'adopt_game')}
              onLink={() => void act(g.id, 'link_game')}
            />
          ))}
        </Focusable>
        <Focusable style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <DialogButton style={button} disabled={running} onClick={start}>
            {status?.done ? 'Search again' : 'Search'}
          </DialogButton>
          <DialogButton style={button} onClick={closeModal}>
            Close
          </DialogButton>
        </Focusable>
      </div>
    </ModalRoot>
  )
}

function FoundRow({
  game,
  busy,
  onAdopt,
  onLink,
}: {
  game: FoundGame
  busy: boolean
  onAdopt: () => void
  onLink: () => void
}) {
  return (
    <Focusable style={foundRow} flow-children="horizontal">
      <div style={foundText}>
        <div>{game.name}</div>
        <div style={{ ...mono, color: '#8b929a' }}>{game.path}</div>
      </div>
      <DialogButton style={button} disabled={busy} onClick={onAdopt}>
        Adopt
      </DialogButton>
      <DialogButton style={button} disabled={busy} onClick={onLink}>
        Link to RomM
      </DialogButton>
    </Focusable>
  )
}
