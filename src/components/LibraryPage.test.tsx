/**
 * LibraryPage tests — F7 state scaffolding + F8 grid + pagination.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor, fireEvent } from "@testing-library/react";
import { LibraryPage } from "./LibraryPage";
import { browseRoms, getBrowseCoverBase64 } from "../api/backend";

vi.mock("../api/backend", () => ({
  browseRoms: vi.fn(),
  getBrowseCoverBase64: vi.fn(),
}));

const _makeRoms = (n: number) =>
  Array.from({ length: n }, (_, i) => ({ id: i + 1, name: `Game ${i + 1}` }));

describe("LibraryPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getBrowseCoverBase64).mockResolvedValue({ success: true, base64: null });
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
});
