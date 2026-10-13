import { DialogButton, Field, Focusable } from '@decky/ui'
import { type ReactNode, useEffect, useState } from 'react'

import { loadInstallState } from '../install'
import type { EmulatorStatus, InstallState } from '../rpc/schema.gen'
import { mono, muted, page } from '../setup/common'

const SOURCE_LABELS: Record<string, string> = {
  settings_sh: "from EmuDeck's settings.sh",
  settings_json: "from EmuDeck's app settings",
  default: 'the default location',
}

/** Shows this many of an emulator's systems before summing up the rest. */
const SHOWN_SYSTEMS = 6

function Check({
  ok,
  label,
  detail,
  fix,
}: {
  ok: boolean
  label: string
  detail?: ReactNode
  fix?: ReactNode
}) {
  return (
    <Field
      label={`${ok ? '✓' : '⚠'} ${label}`}
      description={
        <>
          {detail && <div>{detail}</div>}
          {!ok && fix && <div style={{ color: '#e8b04b' }}>{fix}</div>}
        </>
      }
    />
  )
}

function Heading({ children }: { children: ReactNode }) {
  return (
    <div style={{ fontSize: 18, fontWeight: 700, color: '#fff', marginTop: 8 }}>{children}</div>
  )
}

function systemsLabel(systems: string[]): string {
  if (systems.length === 0) return 'No system uses it by default.'
  if (systems.length <= SHOWN_SYSTEMS) return `For ${systems.join(', ')}.`
  const rest = systems.length - SHOWN_SYSTEMS
  return `For ${systems.slice(0, SHOWN_SYSTEMS).join(', ')} and ${rest} more.`
}

function EmulatorChecks({ emu }: { emu: EmulatorStatus }) {
  const unresolved = emu.saveDirs?.filter((d) => !d.resolved) ?? []
  return (
    <>
      <Check
        ok={emu.installed}
        label={emu.name}
        detail={systemsLabel(emu.systems)}
        fix={`${emu.name} isn't installed. Install it from EmuDeck.`}
      />
      {emu.saveSync && emu.installed && (
        <Check
          ok={unresolved.length === 0}
          label={`${emu.name} save folders`}
          detail={
            <span style={mono}>
              {(unresolved.length ? unresolved : (emu.saveDirs ?? [])).map((d) => (
                <div key={d.path}>{d.resolved ? `${d.path} → ${d.resolved}` : d.path}</div>
              ))}
            </span>
          }
          fix={`${emu.name}'s save folder isn't set up, so its saves don't sync. Run its setup in EmuDeck.`}
        />
      )}
    </>
  )
}

function Checks({ state }: { state: InstallState }) {
  const { emudeck, esde } = state
  return (
    <>
      <Heading>EmuDeck</Heading>
      <Check
        ok={emudeck.found}
        label="Emulation folder"
        detail={
          emudeck.found ? (
            <>
              <span style={mono}>{emudeck.root}</span>, {SOURCE_LABELS[emudeck.source ?? ''] ?? ''}
            </>
          ) : (
            <>
              Looked for a roms folder at:
              <span style={mono}>
                {emudeck.tried.map((p) => (
                  <div key={p}>{p}</div>
                ))}
              </span>
            </>
          )
        }
        fix="Install EmuDeck and finish its setup. Until the plugin finds the Emulation folder, downloads, BIOS and save sync stay off."
      />

      <Heading>ES-DE</Heading>
      <Check
        ok={esde.settingsFound}
        label="Settings"
        detail={
          <>
            Game media goes in <span style={mono}>{esde.mediaDirectory}</span>
          </>
        }
        fix="ES-DE's settings weren't found. Open ES-DE once so it creates them."
      />
      {esde.settingsFound && state.emudeck.found && (
        <Check
          ok={esde.romDirectoryMatches}
          label="Reads EmuDeck's ROMs"
          detail={
            <>
              ES-DE reads games from <span style={mono}>{esde.romDirectory}</span>
            </>
          }
          fix={`ES-DE reads games from ${esde.romDirectory}, but EmuDeck's ROMs are in ${state.emudeck.romsPath}, so games the plugin downloads won't appear in ES-DE. Point ES-DE at EmuDeck's ROMs folder: reset ES-DE's configuration from EmuDeck, or set ROMDirectory in ES-DE's es_settings.xml.`}
        />
      )}
      <Check
        ok={!esde.parseGamelistOnly}
        label="Shows new games"
        fix="ES-DE only shows games listed in its gamelists, so downloaded games won't appear. In ES-DE, turn off Other Settings → Only show games from gamelist files."
      />
      <Field label="ES-DE" description={esde.running ? 'Running' : 'Not running'} />

      <Heading>Emulators</Heading>
      <div style={muted}>
        The emulators EmuDeck sets up for each system, and the ones whose saves sync.
      </div>
      {state.emulators.map((emu) => (
        <EmulatorChecks key={emu.id} emu={emu} />
      ))}
    </>
  )
}

export function StatusPage() {
  const [state, setState] = useState<InstallState | null>(null)
  const [checking, setChecking] = useState(true)

  const load = (refresh: boolean) =>
    loadInstallState(refresh).then((s) => {
      setState(s)
      setChecking(false)
    })

  const checkAgain = () => {
    setChecking(true)
    void load(true)
  }

  useEffect(() => {
    void load(false)
  }, [])

  return (
    <div style={page}>
      <div style={{ fontSize: 24, fontWeight: 700, color: '#fff' }}>Setup status</div>
      <Focusable style={{ display: 'flex', gap: 12 }}>
        <DialogButton disabled={checking} onClick={checkAgain}>
          {checking ? 'Checking…' : 'Check again'}
        </DialogButton>
      </Focusable>
      {state ? (
        <Checks state={state} />
      ) : (
        !checking && (
          <div style={muted}>
            {"The backend isn't answering. Check the plugin's backend status."}
          </div>
        )
      )}
    </div>
  )
}
