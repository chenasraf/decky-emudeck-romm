/**
 * Library tab grid card. Renders cover thumbnail + title + Download CTA.
 * Cover loading uses ``get_browse_cover_base64`` (backend LRU-cached); the
 * Download button calls ``start_download``. Per-card live queue state is
 * deferred to Sprint 6 — for now we show a transient "Queued" badge on
 * tap and rely on the LibraryPage's Refresh button to re-poll if needed.
 */

import { useState, useEffect, FC } from "react";
import { toaster } from "@decky/api";
import { getBrowseCoverBase64, startDownload } from "../api/backend";
import type { BrowseRom } from "../types/browse";

interface RomCardProps {
  rom: BrowseRom;
  installed?: boolean;
  onDownloadQueued?: (rom: BrowseRom) => void;
}

export const RomCard: FC<RomCardProps> = ({ rom, installed = false, onDownloadQueued }) => {
  const [coverData, setCoverData] = useState<string | null>(null);
  const [coverMime, setCoverMime] = useState<string>("image/jpeg");
  const [coverLoaded, setCoverLoaded] = useState(false);
  const [queued, setQueued] = useState(false);

  useEffect(() => {
    let cancelled = false;
    getBrowseCoverBase64(rom.id)
      .then((res) => {
        if (cancelled) return;
        setCoverData(res.success ? res.base64 : null);
        setCoverMime(res.mime ?? "image/jpeg");
        setCoverLoaded(true);
      })
      .catch(() => {
        if (cancelled) return;
        setCoverData(null);
        setCoverLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, [rom.id]);

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
        {coverData ? (
          <img
            src={`data:${coverMime};base64,${coverData}`}
            alt={rom.name ?? `ROM ${rom.id}`}
            loading="lazy"
            style={{ width: "100%", height: "100%", objectFit: "cover" }}
          />
        ) : coverLoaded ? (
          <span style={{ fontSize: "10px", opacity: 0.6 }}>No art</span>
        ) : null}
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
