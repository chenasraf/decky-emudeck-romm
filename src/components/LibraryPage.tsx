/**
 * Browse-first Library tab.
 *
 * Paginated grid of ROMs fetched live from RomM (via the ``browse_roms``
 * callable). F8 ships the grid + thumbnails; F9 adds filter pills +
 * debounced search; F10 wires the per-card Download CTA.
 */

import { useState, useEffect, useCallback, FC } from "react";
import { PanelSection, PanelSectionRow, ButtonItem, Spinner, TextField } from "@decky/ui";
import { addEventListener, removeEventListener, toaster } from "@decky/api";
import { browseRoms, getInstalledRomIds, getPlatforms } from "../api/backend";
import type { BrowseRom, DownloadCompleteEvent, DownloadFailedEvent, DownloadProgressEvent, PlatformSyncSetting } from "../types";
import { RomCard } from "./RomCard";
import { useDebounce } from "../utils/useDebounce";

interface LibraryPageProps {
  onBack: () => void;
}

type LoadState = "loading" | "empty" | "error" | "ready";

const PAGE_SIZE = 30;
const SEARCH_DEBOUNCE_MS = 300;

export const LibraryPage: FC<LibraryPageProps> = ({ onBack }) => {
  const [state, setState] = useState<LoadState>("loading");
  const [items, setItems] = useState<BrowseRom[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [errorMsg, setErrorMsg] = useState<string>("");

  const [platforms, setPlatforms] = useState<PlatformSyncSetting[]>([]);
  const [selectedPlatformIds, setSelectedPlatformIds] = useState<number[]>([]);
  const [searchInput, setSearchInput] = useState("");
  const debouncedSearch = useDebounce(searchInput, SEARCH_DEBOUNCE_MS);
  const [installedIds, setInstalledIds] = useState<Set<number>>(new Set());
  // Map rom_id → progress fraction (0..1). Presence implies "in flight";
  // 0 means just queued (no progress yet), <1 means downloading.
  const [downloadProgress, setDownloadProgress] = useState<Map<number, number>>(new Map());

  useEffect(() => {
    getPlatforms()
      .then((res) => {
        if (res.success) setPlatforms(res.platforms.filter((p) => p.sync_enabled));
      })
      .catch(() => {
        /* leave the pill row empty if platforms can't load */
      });
    getInstalledRomIds()
      .then((res) => setInstalledIds(new Set(res?.ids ?? [])))
      .catch(() => {
        /* without an installed-set, every card shows Download — acceptable */
      });
  }, []);

  // Listen for backend download lifecycle events so per-card state resolves
  // as the download moves through Queued → Downloading X% → Installed (or
  // Failed). The Library tab is the only place that surfaces ad-hoc downloads
  // triggered from a RomCard — other listeners (CustomPlayButton, DownloadQueue)
  // cover their own flows.
  useEffect(() => {
    const onProgress = addEventListener<[DownloadProgressEvent]>("download_progress", (evt) => {
      setDownloadProgress((prev) => {
        const current = prev.get(evt.rom_id);
        if (current === evt.progress) return prev;
        const next = new Map(prev);
        next.set(evt.rom_id, evt.progress);
        return next;
      });
    });
    const onComplete = addEventListener<[DownloadCompleteEvent]>("download_complete", (evt) => {
      setDownloadProgress((prev) => {
        if (!prev.has(evt.rom_id)) return prev;
        const next = new Map(prev);
        next.delete(evt.rom_id);
        return next;
      });
      setInstalledIds((prev) => {
        if (prev.has(evt.rom_id)) return prev;
        const next = new Set(prev);
        next.add(evt.rom_id);
        return next;
      });
    });
    const onFailed = addEventListener<[DownloadFailedEvent]>("download_failed", (evt) => {
      setDownloadProgress((prev) => {
        if (!prev.has(evt.rom_id)) return prev;
        const next = new Map(prev);
        next.delete(evt.rom_id);
        return next;
      });
      toaster.toast({ title: "Download failed", body: `${evt.rom_name}: ${evt.error_message}` });
    });
    return () => {
      removeEventListener("download_progress", onProgress);
      removeEventListener("download_complete", onComplete);
      removeEventListener("download_failed", onFailed);
    };
  }, []);

  const handleDownloadQueued = useCallback((rom: BrowseRom) => {
    setDownloadProgress((prev) => {
      if (prev.has(rom.id)) return prev;
      const next = new Map(prev);
      next.set(rom.id, 0);
      return next;
    });
  }, []);

  const load = async (pageIndex: number, ids: number[], search: string) => {
    setState("loading");
    setErrorMsg("");
    try {
      const result = await browseRoms({
        platform_ids: ids.length > 0 ? ids : null,
        search: search.trim() || null,
        limit: PAGE_SIZE,
        offset: pageIndex * PAGE_SIZE,
      });
      if (!result.success) {
        setErrorMsg(result.message ?? "Couldn't reach RomM");
        setState("error");
        return;
      }
      setItems(result.items);
      setTotal(result.total);
      setState(result.items.length === 0 ? "empty" : "ready");
    } catch (e) {
      setErrorMsg(String(e));
      setState("error");
    }
  };

  useEffect(() => {
    void load(page, selectedPlatformIds, debouncedSearch);
    // ``debouncedSearch`` already debounces; ``selectedPlatformIds`` and ``page`` fire immediately.
  }, [page, selectedPlatformIds, debouncedSearch]);

  // Reset to page 0 when filters or search change.
  useEffect(() => {
    setPage(0);
  }, [selectedPlatformIds, debouncedSearch]);

  const togglePlatform = (id: number) => {
    setSelectedPlatformIds((prev) =>
      prev.includes(id) ? prev.filter((p) => p !== id) : [...prev, id],
    );
  };

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <>
      <PanelSection title="Library">
        <PanelSectionRow>
          <TextField
            label="Search"
            value={searchInput}
            onChange={(e: { target: { value: string } }) => setSearchInput(e.target.value)}
          />
        </PanelSectionRow>
        {platforms.length > 0 && (
          <PanelSectionRow>
            <div
              data-testid="library-filter-pills"
              style={{ display: "flex", flexWrap: "wrap", gap: "4px" }}
            >
              {platforms.map((p) => {
                const selected = selectedPlatformIds.includes(p.id);
                return (
                  <button
                    key={p.id}
                    data-testid={`library-pill-${p.id}`}
                    data-selected={selected ? "true" : "false"}
                    onClick={() => togglePlatform(p.id)}
                    style={{
                      padding: "2px 8px",
                      borderRadius: "12px",
                      border: selected ? "1px solid #4caf50" : "1px solid rgba(255,255,255,0.2)",
                      background: selected ? "rgba(76,175,80,0.2)" : "transparent",
                      color: "inherit",
                      fontSize: "11px",
                      cursor: "pointer",
                    }}
                  >
                    {p.name}
                  </button>
                );
              })}
            </div>
          </PanelSectionRow>
        )}

        {state === "loading" && (
          <PanelSectionRow>
            <Spinner />
          </PanelSectionRow>
        )}
        {state === "empty" && (
          <PanelSectionRow>
            <div data-testid="library-empty">No ROMs match these filters.</div>
          </PanelSectionRow>
        )}
        {state === "error" && (
          <>
            <PanelSectionRow>
              <div data-testid="library-error">Couldn't reach RomM — {errorMsg || "check your connection."}</div>
            </PanelSectionRow>
            <PanelSectionRow>
              <ButtonItem layout="below" onClick={() => void load(page, selectedPlatformIds, debouncedSearch)}>
                Retry
              </ButtonItem>
            </PanelSectionRow>
          </>
        )}
        {state === "ready" && (
          <>
            <PanelSectionRow>
              <div
                data-testid="library-grid"
                style={{
                  display: "grid",
                  gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
                  gap: "6px",
                }}
              >
                {items.map((rom) => (
                  <RomCard
                    key={rom.id}
                    rom={rom}
                    installed={installedIds.has(rom.id)}
                    progress={downloadProgress.get(rom.id)}
                    onDownloadQueued={handleDownloadQueued}
                  />
                ))}
              </div>
            </PanelSectionRow>
            <PanelSectionRow>
              <div data-testid="library-pagination" style={{ display: "flex", justifyContent: "space-between" }}>
                <span>
                  Page {page + 1} of {totalPages} — {total} ROM{total === 1 ? "" : "s"}
                </span>
              </div>
            </PanelSectionRow>
            <PanelSectionRow>
              <ButtonItem layout="below" disabled={page === 0} onClick={() => setPage((p) => Math.max(0, p - 1))}>
                Previous
              </ButtonItem>
            </PanelSectionRow>
            <PanelSectionRow>
              <ButtonItem
                layout="below"
                disabled={page >= totalPages - 1}
                onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              >
                Next
              </ButtonItem>
            </PanelSectionRow>
          </>
        )}
      </PanelSection>

      <PanelSection>
        <PanelSectionRow>
          <ButtonItem layout="below" onClick={onBack}>
            Back
          </ButtonItem>
        </PanelSectionRow>
      </PanelSection>
    </>
  );
};
