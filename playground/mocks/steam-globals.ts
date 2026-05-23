// Minimal browser stubs for Steam Deck ambient globals that components
// reach for. Extend per-demo as needed — anything not listed here will be
// `undefined` and most components defensively guard for that.

const noop = () => {};
const noopReg = () => ({ unregister: noop });

function attachGlobals() {
  const g = globalThis as Record<string, unknown>;

  g.SteamClient ??= {
    Apps: {
      AddShortcut: async () => 99999,
      SetShortcutName: noop,
      SetShortcutExe: noop,
      SetShortcutStartDir: noop,
      SetAppLaunchOptions: noop,
      RemoveShortcut: noop,
      RegisterForAppDetails: noopReg,
    },
    GameSessions: {
      RegisterForAppLifetimeNotifications: noopReg,
    },
    System: {
      GetSystemInfo: async () => ({ sHostname: "playground" }),
      RegisterForOnSuspendRequest: noopReg,
      RegisterForOnResumeFromSuspend: noopReg,
    },
  };

  g.appStore ??= {
    GetAppOverviewByAppID: () => null,
    allApps: [],
  };
  g.appDetailsStore ??= { GetAppDetails: () => null };
  g.appDetailsCache ??= { GetAppData: () => null };
  g.collectionStore ??= {
    userCollections: [],
    deckDesktopApps: { apps: new Map() },
  };
}

attachGlobals();
