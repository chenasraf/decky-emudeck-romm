/**
 * Library tab grid card. Cover thumbnail + title + Download CTA.
 *
 * Covers arrive inline on the ``browse_roms`` payload (server-side
 * ``asyncio.gather`` fetches all covers in one round-trip), so this
 * component is pure render — no per-card backend calls. Per-card live
 * queue state (Queued/Downloading%/Updates) is deferred to Sprint 6.
 */

import { useState, FC } from "react";
import { toaster } from "@decky/api";
import { startDownload } from "../api/backend";
import type { BrowseRom } from "../types/browse";

interface RomCardProps {
  rom: BrowseRom;
  installed?: boolean;
  onDownloadQueued?: (rom: BrowseRom) => void;
}

export const RomCard: FC<RomCardProps> = ({ rom, installed = false, onDownloadQueued }) => {
  const [queued, setQueued] = useState(false);

  const cover = rom.cover_base64;
  const mime = rom.cover_mime ?? "image/jpeg";

  const handleDownload = async () => {
    setQueued(true);
    try {
      const res = await startDownload(rom.id);
      if (res.success) {
        toaster.toast({ title: "Download queued", body: rom.name ?? `ROM ${rom.id}` });
        onDownloadQueued?.(rom);
      } else {
        setQueued(false);
        toaster.toast({ title: "Download failed", body: res.message ?? "Could not queue download" });
      }
    } catch (e) {
      setQueued(false);
      toaster.toast({ title: "Download failed", body: String(e) });
    }
  };

  return (
    <div data-testid="rom-card" style={{ display: "flex", flexDirection: "column", padding: "4px" }}>
      <div
        data-testid="rom-card-cover"
        style={{
          width: "100%",
          aspectRatio: "3 / 4",
          background: "rgba(255,255,255,0.05)",
          borderRadius: "4px",
          overflow: "hidden",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        {cover ? (
          <img
            src={`data:${mime};base64,${cover}`}
            alt={rom.name ?? `ROM ${rom.id}`}
            loading="lazy"
            style={{ width: "100%", height: "100%", objectFit: "cover" }}
          />
        ) : (
          <span style={{ fontSize: "10px", opacity: 0.6 }}>No art</span>
        )}
      </div>
      <div
        data-testid="rom-card-title"
        style={{
          marginTop: "4px",
          fontSize: "11px",
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
        title={rom.name}
      >
        {rom.name ?? `ROM ${rom.id}`}
      </div>
      <button
        data-testid="rom-card-download"
        data-installed={installed ? "true" : "false"}
        onClick={() => void handleDownload()}
        disabled={queued}
        style={{
          marginTop: "4px",
          padding: "2px 6px",
          fontSize: "10px",
          borderRadius: "3px",
          border: installed
            ? "1px solid #4caf50"
            : "1px solid rgba(255,255,255,0.2)",
          background: queued || installed ? "rgba(76,175,80,0.2)" : "transparent",
          color: "inherit",
          cursor: queued ? "default" : "pointer",
        }}
      >
        {queued ? "Queued" : installed ? "Installed — Re-download" : "Download"}
      </button>
    </div>
  );
};
