import {
  ConfirmModal,
  DialogButton,
  DropdownItem,
  Field,
  Focusable,
  ModalRoot,
  PanelSectionRow,
  showModal,
} from '@decky/ui'
import { type CSSProperties, useEffect, useState } from 'react'

import { call } from '../backend'
import { timeAgo } from '../catalog'
import type {
  Error as RpcError,
  SaveStateEntry,
  StatePrefs,
  StateSettings,
} from '../rpc/schema.gen'
import {
  refreshSaves,
  STATE_KEEPS,
  STATE_SLOTS,
  stateKeepLabel,
  stateSlotLabel,
  useStateStatus,
} from '../saves'
import { asRpcError, ErrorBox, muted } from '../setup/common'

const body: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 12, minWidth: 560 }
const row: CSSProperties = { display: 'flex', alignItems: 'center', gap: 12 }
const text: CSSProperties = { flex: '1 1 auto', minWidth: 0 }
const shot: CSSProperties = {
  width: 120,
  height: 80,
  objectFit: 'contain',
  background: '#000',
  borderRadius: 4,
  flexShrink: 0,
}
const button: CSSProperties = { width: 'auto', minWidth: 0, padding: '6px 14px' }

/** Where the slots RomM's state goes in are, as a sentence's end. */
function placedIn(s: StateSettings): string {
  const target = stateSlotLabel(s.target).toLowerCase()
  return s.extra && s.extra !== s.target
    ? `${target}, with a copy in ${stateSlotLabel(s.extra).toLowerCase()}`
    : target
}

/** A game's save state sync: where it stands, its own settings, and RomM's states to load. */
export function StatesSection({
  id,
  onError,
}: {
  id: number
  onError: (e: RpcError | null) => void
}) {
  const status = useStateStatus(id)
  // Holds the game's own settings from the moment they change, before state_status says so again.
  const [own, setOwn] = useState<StatePrefs | null>(null)
  if (!status) return null

  if (!status.syncs) {
    return (
      <PanelSectionRow>
        <Field label="Save states" description={status.why}>
          Not synced
        </Field>
      </PanelSectionRow>
    )
  }

  const current = own ?? status.own
  const set = (next: StatePrefs) => {
    setOwn(next)
    call('set_state_prefs', { id, own: next }).then(
      () => {
        onError(null)
        refreshSaves()
      },
      (err: unknown) => {
        setOwn(null)
        onError(asRpcError(err))
      },
    )
  }
  const settings: StateSettings = {
    sync: current.sync ?? status.global.sync,
    target: current.target ?? status.global.target,
    extra: current.extra ?? status.global.extra,
    keep: current.keep ?? status.global.keep,
  }

  const line = !settings.sync
    ? 'Off for this game'
    : status.waiting
      ? `Waiting for ${status.emulator ?? 'the emulator'} to close`
      : status.problem
        ? '⚠ Sync failed'
        : 'Synced with saves'
  const detail = !settings.sync
    ? undefined
    : status.waiting
      ? `RomM has a newer save state. It goes in ${placedIn(settings)} once ${status.emulator ?? 'the emulator'} closes, which writes its auto-state on the way out.`
      : status.problem
        ? status.problem
        : `New states on this Deck go to RomM. RomM's newest from elsewhere goes in ${placedIn(settings)}.`

  // "Default" follows Settings → Preferences; picking it drops the game's own setting.
  const DEFAULT = 'default'
  const pick = (key: keyof StatePrefs, value: unknown) => {
    const next: StatePrefs = { ...current }
    if (value === DEFAULT) delete next[key]
    else (next as Record<string, unknown>)[key] = value
    set(next)
  }
  const def = (label: string) => ({ data: DEFAULT, label: `Default (${label})` })

  return (
    <>
      <PanelSectionRow>
        <Field label="Save states" description={detail}>
          {line}
        </Field>
      </PanelSectionRow>
      <PanelSectionRow>
        <Field
          childrenLayout="below"
          label="RomM's save states"
          description="Every state of this game on RomM, newest first. Load one to play from it."
        >
          <DialogButton
            style={{ minWidth: 0, width: '100%' }}
            onClick={() => showSaveStates(id, settings)}
          >
            Browse save states
          </DialogButton>
        </Field>
      </PanelSectionRow>
      <PanelSectionRow>
        <DropdownItem
          label="Sync save states"
          rgOptions={[
            def(status.global.sync ? 'on' : 'off'),
            { data: true, label: 'On' },
            { data: false, label: 'Off' },
          ]}
          selectedOption={current.sync ?? DEFAULT}
          onChange={(o) => pick('sync', o.data)}
        />
      </PanelSectionRow>
      {settings.sync && (
        <>
          <PanelSectionRow>
            <DropdownItem
              label="RomM's state goes in"
              description="The slot RomM's newest save state is put in. RetroArch loads its auto-state when the game starts."
              rgOptions={[
                def(stateSlotLabel(status.global.target)),
                ...STATE_SLOTS.map((s) => ({ data: s, label: stateSlotLabel(s) })),
              ]}
              selectedOption={current.target ?? DEFAULT}
              onChange={(o) => pick('target', o.data)}
            />
          </PanelSectionRow>
          <PanelSectionRow>
            <DropdownItem
              label="Also copy it to"
              description="A slot that keeps RomM's state after the game writes its own auto-state on quitting."
              rgOptions={[
                def(status.global.extra ? stateSlotLabel(status.global.extra) : 'none'),
                { data: '', label: 'None' },
                ...STATE_SLOTS.filter((s) => s !== 'auto').map((s) => ({
                  data: s,
                  label: stateSlotLabel(s),
                })),
              ]}
              selectedOption={current.extra ?? DEFAULT}
              onChange={(o) => pick('extra', o.data)}
            />
          </PanelSectionRow>
          <PanelSectionRow>
            <DropdownItem
              label="This Deck's states on RomM"
              description="How many of the states this Deck uploads RomM keeps. Others' states are never removed."
              rgOptions={[
                def(stateKeepLabel(status.global.keep).toLowerCase()),
                ...STATE_KEEPS.map((k) => ({ data: k, label: stateKeepLabel(k) })),
              ]}
              selectedOption={current.keep ?? DEFAULT}
              onChange={(o) => pick('keep', o.data)}
            />
          </PanelSectionRow>
        </>
      )}
    </>
  )
}

/** Opens the list of the game with id's save states on RomM. */
export function showSaveStates(id: number, settings: StateSettings) {
  showModal(<SaveStatesModal id={id} settings={settings} />)
}

function SaveStatesModal({
  id,
  settings,
  closeModal,
}: {
  id: number
  settings: StateSettings
  closeModal?: () => void
}) {
  const [states, setStates] = useState<SaveStateEntry[] | null>(null)
  const [error, setError] = useState<RpcError | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    call('save_states', { id }).then(
      (r) => setStates(r.states),
      (err: unknown) => setError(asRpcError(err)),
    )
  }, [id])

  const load = (s: SaveStateEntry) =>
    showModal(
      <ConfirmModal
        strTitle="Load this save state?"
        strDescription={`The state from ${timeAgo(s.at)} goes in ${placedIn(settings)}. What's there now goes to RomM first if it isn't already, so nothing is lost.`}
        strOKButtonText="Load"
        onOK={() => {
          setBusy(true)
          call('load_save_state', { id, stateId: s.id }).then(
            () => {
              refreshSaves()
              closeModal?.()
            },
            (err: unknown) => {
              setError(asRpcError(err))
              setBusy(false)
            },
          )
        }}
      />,
    )

  return (
    <ModalRoot closeModal={closeModal}>
      <div style={body}>
        <div style={{ fontSize: 22, fontWeight: 700 }}>Save states</div>
        <div style={muted}>
          This game&apos;s save states on RomM, from this Deck, RomM&apos;s web player and your
          other devices. Loading one puts it in {placedIn(settings)}.
        </div>
        {error && <ErrorBox>{error.message}</ErrorBox>}
        {!states && !error && <div style={muted}>Asking RomM for this game&apos;s states…</div>}
        {states && states.length === 0 && (
          <div style={muted}>RomM has no save states of this game yet.</div>
        )}
        {states && states.length > 0 && (
          <Focusable style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {states.map((s) => (
              <Focusable key={s.id} style={row} flow-children="horizontal">
                {s.screenshot ? (
                  <img src={s.screenshot} style={shot} alt="" />
                ) : (
                  <div style={shot} />
                )}
                <div style={text}>
                  <div>{timeAgo(s.at)}</div>
                  <div style={{ ...muted, fontSize: 12 }}>
                    {s.fromDeck
                      ? `This Deck${s.slot ? ` · ${stateSlotLabel(s.slot).toLowerCase()}` : ''}`
                      : 'Web player or another device'}
                    {s.placed && ' · on this Deck'}
                  </div>
                </div>
                <DialogButton style={button} disabled={busy} onClick={() => load(s)}>
                  Load
                </DialogButton>
              </Focusable>
            ))}
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
