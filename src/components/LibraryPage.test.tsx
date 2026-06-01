/**
 * LibraryPage tests — F7 state scaffolding + F8 grid + pagination.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, render, waitFor, fireEvent } from "@testing-library/react";
import { emitDeckyEvent } from "../test-utils/decky-api-mock";
import { LibraryPage } from "./LibraryPage";
import { browseRoms, getInstalledRomIds, getPlatforms, startDownload } from "../api/backend";
import type { DownloadCompleteEvent, DownloadFailedEvent } from "../types";

vi.mock("../api/backend", () => ({
  browseRoms: vi.fn(),
  getInstalledRomIds: vi.fn(),
  getPlatforms: vi.fn(),
  startDownload: vi.fn(),
}));

const _makeRoms = (n: number) =>
  Array.from({ length: n }, (_, i) => ({ id: i + 1, name: `Game ${i + 1}` }));

describe("LibraryPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getPlatforms).mockResolvedValue({ success: true, platforms: [] });
    vi.mocked(getInstalledRomIds).mockResolvedValue({ ids: [] });
    vi.mocked(startDownload).mockResolvedValue({ success: true });
  });

  it("shows the spinner while browseRoms is in flight", () => {
    vi.mocked(browseRoms).mockReturnValue(new Promise(() => {}));
    const { getByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    expect(getByTestId("spinner")).toBeDefined();
  });

  it("renders the empty state when browseRoms returns no items", async () => {
    vi.mocked(browseRoms).mockResolvedValue({ success: true, items: [], total: 0 });
    const { findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    expect(await findByTestId("library-empty")).toBeDefined();
  });

  it("renders the grid with one card per item when ready", async () => {
    vi.mocked(browseRoms).mockResolvedValue({ success: true, items: _makeRoms(5), total: 5 });
    const { findByTestId, findAllByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    expect(await findByTestId("library-grid")).toBeDefined();
    const cards = await findAllByTestId("rom-card");
    expect(cards).toHaveLength(5);
  });

  it("renders the error state and surfaces the backend message on success=false", async () => {
    vi.mocked(browseRoms).mockResolvedValue({
      success: false,
      items: [],
      total: 0,
      message: "Server is having a bad day",
      error_code: "connection_error",
    });
    const { findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const err = await findByTestId("library-error");
    expect(err.textContent).toContain("Server is having a bad day");
  });

  it("falls into the error state when the callable rejects", async () => {
    vi.mocked(browseRoms).mockRejectedValue(new Error("kaboom"));
    const { findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const err = await findByTestId("library-error");
    expect(err.textContent).toContain("kaboom");
  });

  it("re-fetches when Retry is pressed", async () => {
    vi.mocked(browseRoms)
      .mockRejectedValueOnce(new Error("first call failed"))
      .mockResolvedValueOnce({ success: true, total: 1, items: _makeRoms(1) });
    const { findByText, findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const retry = await findByText("Retry");
    fireEvent.click(retry);
    await waitFor(() => expect(vi.mocked(browseRoms)).toHaveBeenCalledTimes(2));
    expect(await findByTestId("library-grid")).toBeDefined();
  });

  it("advances offset when Next is pressed", async () => {
    vi.mocked(browseRoms)
      .mockResolvedValueOnce({ success: true, items: _makeRoms(30), total: 75 })
      .mockResolvedValueOnce({ success: true, items: _makeRoms(30), total: 75 });
    const { findByText } = render(<LibraryPage onBack={vi.fn()} />);
    const next = await findByText("Next");
    fireEvent.click(next);
    await waitFor(() => expect(vi.mocked(browseRoms)).toHaveBeenLastCalledWith(null, null, 30, 30));
  });

  it("disables Previous on the first page", async () => {
    vi.mocked(browseRoms).mockResolvedValue({ success: true, items: _makeRoms(30), total: 90 });
    const { findByText } = render(<LibraryPage onBack={vi.fn()} />);
    const prev = (await findByText("Previous")) as HTMLButtonElement;
    expect(prev.disabled).toBe(true);
  });

  it("surfaces pagination metadata in the status row", async () => {
    vi.mocked(browseRoms).mockResolvedValue({ success: true, items: _makeRoms(30), total: 75 });
    const { findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const meta = await findByTestId("library-pagination");
    expect(meta.textContent).toContain("Page 1 of 3");
    expect(meta.textContent).toContain("75 ROMs");
  });

  it("only shows pills for enabled platforms", async () => {
    vi.mocked(browseRoms).mockResolvedValue({ success: true, items: [], total: 0 });
    vi.mocked(getPlatforms).mockResolvedValue({
      success: true,
      platforms: [
        { id: 1, name: "N64", slug: "n64", rom_count: 10, sync_enabled: true },
        { id: 2, name: "SNES", slug: "snes", rom_count: 5, sync_enabled: false },
      ],
    });
    const { findByTestId, queryByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    expect(await findByTestId("library-pill-1")).toBeDefined();
    expect(queryByTestId("library-pill-2")).toBeNull();
  });

  it("re-queries browse_roms with the selected platform id when a pill is tapped", async () => {
    vi.mocked(browseRoms).mockResolvedValue({ success: true, items: [], total: 0 });
    vi.mocked(getPlatforms).mockResolvedValue({
      success: true,
      platforms: [{ id: 7, name: "GBA", slug: "gba", rom_count: 4, sync_enabled: true }],
    });
    const { findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const pill = await findByTestId("library-pill-7");
    fireEvent.click(pill);
    await waitFor(() => expect(vi.mocked(browseRoms)).toHaveBeenLastCalledWith([7], null, 30, 0));
  });

  it("toggles a pill off and clears the platform filter on second click", async () => {
    vi.mocked(browseRoms).mockResolvedValue({ success: true, items: [], total: 0 });
    vi.mocked(getPlatforms).mockResolvedValue({
      success: true,
      platforms: [{ id: 9, name: "PS1", slug: "ps", rom_count: 100, sync_enabled: true }],
    });
    const { findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const pill = await findByTestId("library-pill-9");
    fireEvent.click(pill);
    await waitFor(() => expect(vi.mocked(browseRoms)).toHaveBeenLastCalledWith([9], null, 30, 0));
    fireEvent.click(pill);
    await waitFor(() => expect(vi.mocked(browseRoms)).toHaveBeenLastCalledWith(null, null, 30, 0));
  });

  it("flips a card from Download to Installed when download_complete fires", async () => {
    vi.mocked(browseRoms).mockResolvedValue({
      success: true,
      total: 1,
      items: [{ id: 42, name: "Zelda" }],
    });
    const { findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const btn = (await findByTestId("rom-card-download")) as HTMLButtonElement;
    expect(btn.dataset.installed).toBe("false");
    act(() => {
      emitDeckyEvent<[DownloadCompleteEvent]>("download_complete", {
        rom_id: 42,
        rom_name: "Zelda",
        platform_name: "N64",
        file_path: "/Emulation/roms/n64/zelda.z64",
      });
    });
    await waitFor(() => expect(btn.dataset.installed).toBe("true"));
  });

  it("clears the Queued badge when download_failed fires", async () => {
    vi.mocked(browseRoms).mockResolvedValue({
      success: true,
      total: 1,
      items: [{ id: 7, name: "Mario" }],
    });
    vi.mocked(startDownload).mockResolvedValue({ success: true });
    const { findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const btn = (await findByTestId("rom-card-download")) as HTMLButtonElement;
    fireEvent.click(btn);
    await waitFor(() => expect(btn.textContent).toBe("Queued"));
    act(() => {
      emitDeckyEvent<[DownloadFailedEvent]>("download_failed", {
        rom_id: 7,
        rom_name: "Mario",
        platform_name: "N64",
        error_message: "timeout",
      });
    });
    await waitFor(() => expect(btn.textContent).toBe("Download"));
  });

  it("marks ROMs in the installed set with the Installed badge", async () => {
    vi.mocked(browseRoms).mockResolvedValue({
      success: true,
      total: 2,
      items: [
        { id: 1, name: "Zelda" },
        { id: 2, name: "Mario" },
      ],
    });
    vi.mocked(getInstalledRomIds).mockResolvedValue({ ids: [1] });
    const { findAllByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const buttons = await findAllByTestId("rom-card-download");
    await waitFor(() => expect(buttons[0]?.dataset.installed).toBe("true"));
    expect(buttons[1]?.dataset.installed).toBe("false");
  });

  it("debounces search-box input before re-querying browse_roms", async () => {
    vi.mocked(browseRoms).mockResolvedValue({ success: true, items: [], total: 0 });
    const { container } = render(<LibraryPage onBack={vi.fn()} />);
    await waitFor(() => expect(vi.mocked(browseRoms)).toHaveBeenCalled());
    const initialCalls = vi.mocked(browseRoms).mock.calls.length;

    const input = container.querySelector("input[data-testid='text-field']") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "mario" } });
    // Immediately after the keystroke, browse_roms hasn't been re-issued yet.
    expect(vi.mocked(browseRoms).mock.calls.length).toBe(initialCalls);
    // After the 300ms debounce window the post-debounce call lands with the new term.
    await waitFor(
      () => {
        const last = vi.mocked(browseRoms).mock.calls.at(-1);
        expect(last?.[1]).toBe("mario");
      },
      { timeout: 1000 },
    );
  });
});
