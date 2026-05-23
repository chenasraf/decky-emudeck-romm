// Full QAM panel mounted standalone. Mirrors the page-switching shell
// from src/index.tsx's QAMPanel, but skips definePlugin/registerLaunchInterceptor
// /registerGameDetailPatch — those depend on Steam's React tree which doesn't
// exist outside the Steam client.
//
// All callables route through playground/mocks/decky-api.ts to the bridge at
// :5175. The bridge calls into the real Plugin instance in py_modules/.
// Start it with: `pnpm playground:server` in a separate terminal.

import { useState, type FC, type ReactNode } from "react";
import { MainPage } from "../../src/components/MainPage";
import { SettingsPage } from "../../src/components/SettingsPage";
import { LibraryPage } from "../../src/components/LibraryPage";
import { DownloadQueue } from "../../src/components/DownloadQueue";
import { DangerZone } from "../../src/components/DangerZone";

type Page = "main" | "settings" | "library" | "data" | "downloads";

export const PluginDemo: FC = () => {
  const [page, setPage] = useState<Page>("main");

  let content: ReactNode;
  switch (page) {
    case "settings":
      content = <SettingsPage onBack={() => setPage("main")} />;
      break;
    case "library":
      content = <LibraryPage onBack={() => setPage("main")} />;
      break;
    case "data":
      content = <DangerZone onBack={() => setPage("main")} />;
      break;
    case "downloads":
      content = <DownloadQueue onBack={() => setPage("main")} />;
      break;
    default:
      content = <MainPage onNavigate={(p) => setPage(p as Page)} />;
  }

  return (
    <div style={{ maxWidth: 380, background: "#1b2838", padding: 12, borderRadius: 4 }}>
      {content}
    </div>
  );
};
