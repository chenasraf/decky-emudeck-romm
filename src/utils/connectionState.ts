import type { FrontendUnsupportedPayload } from "../types";

/** Shared RomM connection state — set by RomMPlaySection, read by CustomPlayButton and sessionManager */
let _state: "checking" | "connected" | "offline" = "checking";
export function getRommConnectionState() { return _state; }
export function setRommConnectionState(s: "checking" | "connected" | "offline") { _state = s; }

/** Version mismatch error — set when server returns error_code: "version_error" */
let _versionError: string | null = null;
const versionErrorListeners = new Set<(err: string | null) => void>();

export function getVersionError() { return _versionError; }
export function setVersionError(msg: string | null) {
  if (_versionError === msg) return;
  _versionError = msg;
  versionErrorListeners.forEach((l) => l(msg));
}
export function onVersionErrorChange(cb: (err: string | null) => void): () => void {
  versionErrorListeners.add(cb);
  return () => { versionErrorListeners.delete(cb); };
}

/**
 * Frontend-host (RetroDECK / EmuDeck) version-band error — set when the
 * backend returns ``error_code: "version_unsupported"`` because
 * ``bootstrap`` rejected the chosen frontend's reported version. Distinct
 * from ``versionError`` (which tracks the RomM SERVER version): the
 * plugin can't proceed in either state, but the remediation differs
 * (update the server vs update the host emulator-frontend).
 */
let _frontendUnsupported: FrontendUnsupportedPayload | null = null;
const frontendUnsupportedListeners = new Set<(p: FrontendUnsupportedPayload | null) => void>();

export function getFrontendUnsupported(): FrontendUnsupportedPayload | null {
  return _frontendUnsupported;
}
export function setFrontendUnsupported(payload: FrontendUnsupportedPayload | null): void {
  // Reference equality is enough for the null⇄null no-op case; payload
  // mutations always arrive as fresh objects from the callable response.
  if (_frontendUnsupported === payload) return;
  _frontendUnsupported = payload;
  frontendUnsupportedListeners.forEach((l) => l(payload));
}
export function onFrontendUnsupportedChange(cb: (p: FrontendUnsupportedPayload | null) => void): () => void {
  frontendUnsupportedListeners.add(cb);
  return () => { frontendUnsupportedListeners.delete(cb); };
}
