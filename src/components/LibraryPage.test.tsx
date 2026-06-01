/**
 * LibraryPage state-render tests — F7 scaffold.
 *
 * Drives the four load states (loading / empty / error / ready) via the
 * mocked ``browseRoms`` callable and asserts the right slot renders.
 *
 * F8 will extend with grid + cover assertions; F10 with Download CTA.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor, fireEvent } from "@testing-library/react";
import { LibraryPage } from "./LibraryPage";
import { browseRoms } from "../api/backend";

vi.mock("../api/backend", () => ({
  browseRoms: vi.fn(),
}));

describe("LibraryPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
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

  it("renders the ready slot with the item count when browseRoms returns items", async () => {
    vi.mocked(browseRoms).mockResolvedValue({
      success: true,
      total: 2,
      items: [
        { id: 1, name: "Zelda" },
        { id: 2, name: "Mario" },
      ],
    });
    const { findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const grid = await findByTestId("library-grid");
    expect(grid.textContent).toContain("2");
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
      .mockResolvedValueOnce({ success: true, total: 1, items: [{ id: 1, name: "Zelda" }] });
    const { findByText, findByTestId } = render(<LibraryPage onBack={vi.fn()} />);
    const retry = await findByText("Retry");
    fireEvent.click(retry);
    await waitFor(() => expect(vi.mocked(browseRoms)).toHaveBeenCalledTimes(2));
    expect(await findByTestId("library-grid")).toBeDefined();
  });
});
