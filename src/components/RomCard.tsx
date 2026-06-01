/**
 * Library tab grid card. Renders cover thumbnail + title + a Download
 * placeholder; F10 wires the placeholder to ``start_download``.
 *
 * Cover loading: each card calls ``get_browse_cover_base64`` on mount
 * (poor-man's lazy-load — the backend LRU dedupes within a sprint of
 * pagination). The grid below the fold paints placeholders first, real
 * art swaps in as covers resolve.
 */

import { useState, useEffect, FC } from "react";
import { getBrowseCoverBase64 } from "../api/backend";
import type { BrowseRom } from "../types/browse";

interface RomCardProps {
  rom: BrowseRom;
  onDownload?: (rom: BrowseRom) => void;
}

export const RomCard: FC<RomCardProps> = ({ rom }) => {
  const [coverData, setCoverData] = useState<string | null>(null);
  const [coverMime, setCoverMime] = useState<string>("image/jpeg");
  const [coverLoaded, setCoverLoaded] = useState(false);

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
    </div>
  );
};
