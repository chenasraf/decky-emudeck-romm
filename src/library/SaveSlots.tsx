import { DialogButton, Focusable, ModalRoot, showModal, TextField } from '@decky/ui'
import { type CSSProperties, useEffect, useState } from 'react'

import { call } from '../backend'
import { timeAgo } from '../catalog'
import type { Error as RpcError, SaveSlot, SaveSlots } from '../rpc/schema.gen'
import { refreshSaves } from '../saves'
import { asRpcError, ErrorBox, muted } from '../setup/common'

/** Longest slot name the backend takes. */
const MAX_SLOT_NAME = 64

const body: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 12, minWidth: 480 }
const slotRow: CSSProperties = { display: 'flex', alignItems: 'center', gap: 8 }
const slotText: CSSProperties = { flex: '1 1 auto', minWidth: 0 }
const button: CSSProperties = { width: 'auto', minWidth: 0, padding: '6px 14px' }

/** Opens the save slot picker of the game with id. */
export function showSaveSlots(id: number) {
  showModal(<SaveSlotsModal id={id} />)
}

/** RomM's slots of a game, with the default and the current one listed even while empty. */
function listed(s: SaveSlots): SaveSlot[] {
  const out = [...s.slots]
  for (const name of [s.current, s.default]) {
    if (!out.some((slot) => slot.name === name)) out.push({ name, count: 0 })
  }
  return out.sort((a, b) =>
    a.name === s.default ? -1 : b.name === s.default ? 1 : a.name.localeCompare(b.name),
  )
}

function SaveSlotsModal({ id, closeModal }: { id: number; closeModal?: () => void }) {
  const [slots, setSlots] = useState<SaveSlots | null>(null)
  const [error, setError] = useState<RpcError | null>(null)
  const [busy, setBusy] = useState(false)
  const [typed, setTyped] = useState('')

  useEffect(() => {
    call('save_slots', { id }).then(setSlots, (err: unknown) => setError(asRpcError(err)))
  }, [id])

  const pick = async (slot: string) => {
    setBusy(true)
    try {
      await call('switch_save_slot', { id, slot })
      refreshSaves()
      closeModal?.()
    } catch (err) {
      setError(asRpcError(err))
      setBusy(false)
    }
  }

  const fresh = typed.trim()
  const known = slots ? listed(slots) : []
  const exists = known.some((s) => s.name === fresh)

  return (
    <ModalRoot closeModal={closeModal}>
      <div style={body}>
        <div style={{ fontSize: 22, fontWeight: 700 }}>Save slot</div>
        <div style={muted}>
          Each RomM slot keeps its own line of saves. Switching syncs this game&apos;s save first,
          then the newest save in the chosen slot replaces the one on this Deck, which is backed up.
          An empty slot starts from this Deck&apos;s save. Nothing is deleted.
        </div>
        {error && <ErrorBox>{error.message}</ErrorBox>}
        {!slots && !error && <div style={muted}>Asking RomM for this game&apos;s slots…</div>}
        {slots && (
          <Focusable style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {known.map((s) => (
              <SlotRow
                key={s.name}
                slot={s}
                current={s.name === slots.current}
                isDefault={s.name === slots.default}
                busy={busy}
                onPick={() => void pick(s.name)}
              />
            ))}
          </Focusable>
        )}
        {slots && (
          <Focusable style={slotRow} flow-children="horizontal">
            <div style={slotText}>
              <TextField
                label="New slot"
                value={typed}
                disabled={busy}
                onChange={(e) => setTyped(e.target.value.slice(0, MAX_SLOT_NAME))}
              />
            </div>
            <DialogButton
              style={button}
              disabled={busy || fresh === '' || exists}
              onClick={() => void pick(fresh)}
            >
              Use
            </DialogButton>
          </Focusable>
        )}
        <Focusable style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <DialogButton style={button} onClick={closeModal}>
            Close
          </DialogButton>
        </Focusable>
      </div>
    </ModalRoot>
  )
}

function SlotRow({
  slot,
  current,
  isDefault,
  busy,
  onPick,
}: {
  slot: SaveSlot
  current: boolean
  isDefault: boolean
  busy: boolean
  onPick: () => void
}) {
  const saves = slot.count === 1 ? '1 save' : `${slot.count} saves`
  const detail =
    slot.count === 0
      ? 'Empty'
      : slot.latestAt
        ? `${saves} · newest ${timeAgo(slot.latestAt)}`
        : saves
  return (
    <Focusable style={slotRow} flow-children="horizontal">
      <div style={slotText}>
        <div>
          {slot.name}
          {isDefault && ' (default, shared with RomM’s other apps)'}
        </div>
        <div style={{ ...muted, fontSize: 12 }}>{detail}</div>
      </div>
      <DialogButton style={button} disabled={busy || current} onClick={onPick}>
        {current ? 'In use' : 'Switch'}
      </DialogButton>
    </Focusable>
  )
}
