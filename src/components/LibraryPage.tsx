/**
 * Browse-first Library tab.
 *
 * Paginated grid of ROMs fetched live from RomM (via the ``browse_roms``
 * callable). Sprint 5 ships the scaffold + loading/empty/error states; F8
 * adds the grid + thumbnails, F9 adds filter pills + debounced search,
 * F10 adds the per-card Download CTA.
 */

import { useState, useEffect, FC } from "react";
import { PanelSection, PanelSectionRow, ButtonItem, Spinner } from "@decky/ui";
import { browseRoms } from "../api/backend";
import type { BrowseRom } from "../types";

interface LibraryPageProps {
  onBack: () => void;
}

type LoadState = "loading" | "empty" | "error" | "ready";

export const LibraryPage: FC<LibraryPageProps> = ({ onBack }) => {
  const [state, setState] = useState<LoadState>("loading");
  const [items, setItems] = useState<BrowseRom[]>([]);
  const [errorMsg, setErrorMsg] = useState<string>("");

  const load = async () => {
    setState("loading");
    setErrorMsg("");
    try {
      const result = await browseRoms(null, null, 30, 0);
      if (!result.success) {
        setErrorMsg(result.message ?? "Couldn't reach RomM");
        setState("error");
        return;
      }
      setItems(result.items);
      setState(result.items.length === 0 ? "empty" : "ready");
    } catch (e) {
      setErrorMsg(String(e));
      setState("error");
    }
  };

  useEffect(() => {
    void load();
  }, []);

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
              <ButtonItem layout="below" onClick={() => void load()}>
                Retry
              </ButtonItem>
            </PanelSectionRow>
          </>
        )}
        {state === "ready" && (
          <PanelSectionRow>
            <div data-testid="library-grid">{items.length} ROM(s) loaded — grid lands in F8.</div>
          </PanelSectionRow>
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
