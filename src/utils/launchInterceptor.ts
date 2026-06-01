/**
 * Global launch interceptor — cancels Steam game launches for RomM shortcuts
 * when the ROM is not downloaded or a save conflict needs resolution.
 *
 * Registered on plugin load, unregistered on unload.
 */

import { toaster } from "@decky/api";
import { isRomMAppId } from "../patches/gameDetailPatch";
import { evaluateLaunch, logInfo, logError } from "../api/backend";

let gameActionHook: { unregister: () => void } | null = null;

export function registerLaunchInterceptor(): void {
  gameActionHook = SteamClient.Apps.RegisterForGameActionStart(
    async (gameActionId: number, appIdStr: string, action: string, _launchSource: number) => {
      if (action !== "LaunchApp") return;

      const appId = Number.parseInt(appIdStr, 10);
      if (Number.isNaN(appId) || !isRomMAppId(appId)) return;

      // Single round-trip — backend composes the rom-lookup + installed-check
      // + save-status read and returns a typed verdict.
      try {
        const verdict = await evaluateLaunch(appId);
        if (verdict.action === "block") {
          SteamClient.Apps.CancelGameAction(gameActionId);
          if (verdict.toast_title && verdict.toast_body) {
            toaster.toast({ title: verdict.toast_title, body: verdict.toast_body });
          }
        } else if (verdict.action === "warn") {
          // Soft warning — launch proceeds. Surfaced when the backend's
          // save-status check failed for a ROM with tracked saves; silently
          // allowing would risk corrupting the wrong slot on an unseen
          // conflict, so the user gets a toast and can choose to retry.
          if (verdict.toast_title && verdict.toast_body) {
            toaster.toast({ title: verdict.toast_title, body: verdict.toast_body });
          }
        }
      } catch (e) {
        logError(`Launch interceptor error: ${e}`);
        // On error, don't block the launch.
      }
    },
  );

  logInfo("Launch interceptor registered");
}

export function unregisterLaunchInterceptor(): void {
  if (gameActionHook) {
    gameActionHook.unregister();
    gameActionHook = null;
  }
  logInfo("Launch interceptor unregistered");
}
