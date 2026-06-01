/**
 * Browse-first Library tab.
 *
 * Paginated grid of ROMs fetched live from RomM (via the ``browse_roms``
 * callable). F8 ships the grid + thumbnails; F9 adds filter pills +
 * debounced search; F10 wires the per-card Download CTA.
 */

import { useState, useEffect, FC } from "react";
import { PanelSection, PanelSectionRow, ButtonItem, Spinner } from "@decky/ui";
import { browseRoms } from "../api/backend";
import type { BrowseRom } from "../types";
import { RomCard } from "./RomCard";

interface LibraryPageProps {
  onBack: () => void;
}

type LoadState = "loading" | "empty" | "error" | "ready";

const PAGE_SIZE = 30;

export const LibraryPage: FC<LibraryPageProps> = ({ onBack }) => {
  const [state, setState] = useState<LoadState>("loading");
  const [items, setItems] = useState<BrowseRom[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [errorMsg, setErrorMsg] = useState<string>("");

  const load = async (pageIndex: number) => {
    setState("loading");
    setErrorMsg("");
    try {
      const result = await browseRoms(null, null, PAGE_SIZE, pageIndex * PAGE_SIZE);
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
    void load(page);
  }, [page]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <>
      <PanelSection title="Library">
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
              <ButtonItem layout="below" onClick={() => void load(page)}>
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
                  <RomCard key={rom.id} rom={rom} />
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
