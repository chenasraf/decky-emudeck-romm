import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor, fireEvent } from "@testing-library/react";
import { toaster } from "@decky/api";
import { RomCard } from "./RomCard";
import { startDownload } from "../api/backend";

vi.mock("../api/backend", () => ({
  startDownload: vi.fn(),
}));

describe("RomCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the ROM name", () => {
    const { getByTestId } = render(<RomCard rom={{ id: 1, name: "Zelda" }} />);
    expect(getByTestId("rom-card-title").textContent).toBe("Zelda");
  });

  it("falls back to the ROM id when name is missing", () => {
    const { getByTestId } = render(<RomCard rom={{ id: 7 }} />);
    expect(getByTestId("rom-card-title").textContent).toBe("ROM 7");
  });

  it("renders the cover img from inline cover_base64 + cover_mime", () => {
    const { getByRole } = render(
      <RomCard rom={{ id: 1, name: "Z", cover_base64: "AAAA", cover_mime: "image/png" }} />,
    );
    const img = getByRole("img") as HTMLImageElement;
    expect(img.src).toBe("data:image/png;base64,AAAA");
    expect(img.loading).toBe("lazy");
  });

  it("defaults to image/jpeg mime when cover_mime is absent", () => {
    const { getByRole } = render(
      <RomCard rom={{ id: 1, cover_base64: "ZZZZ" }} />,
    );
    const img = getByRole("img") as HTMLImageElement;
    expect(img.src).toBe("data:image/jpeg;base64,ZZZZ");
  });

  it("shows the 'No art' placeholder when no cover is present", () => {
    const { getByText } = render(<RomCard rom={{ id: 1 }} />);
    expect(getByText("No art")).toBeDefined();
  });

  it("calls startDownload and switches to Queued on tap", async () => {
    vi.mocked(startDownload).mockResolvedValue({ success: true });
    const { getByTestId } = render(<RomCard rom={{ id: 42, name: "Zelda" }} />);
    const btn = getByTestId("rom-card-download") as HTMLButtonElement;
    fireEvent.click(btn);
    await waitFor(() => expect(vi.mocked(startDownload)).toHaveBeenCalledWith(42));
    await waitFor(() => expect(btn.textContent).toBe("Queued"));
    expect(btn.disabled).toBe(true);
  });

  it("reverts to Download and toasts when start_download returns success=false", async () => {
    vi.mocked(startDownload).mockResolvedValue({ success: false, message: "queue full" });
    const { getByTestId } = render(<RomCard rom={{ id: 1, name: "X" }} />);
    const btn = getByTestId("rom-card-download") as HTMLButtonElement;
    fireEvent.click(btn);
    await waitFor(() => expect(btn.textContent).toBe("Download"));
    expect(btn.disabled).toBe(false);
    const toastMock = vi.mocked(toaster).toast;
    const calls = toastMock.mock.calls.map((c) => c[0]);
    expect(calls.some((c) => c?.title === "Download failed" && c?.body === "queue full")).toBe(true);
  });

  it("reverts to Download and toasts on a startDownload rejection (post-catch state)", async () => {
    vi.mocked(startDownload).mockRejectedValue(new Error("network gone"));
    const { getByTestId } = render(<RomCard rom={{ id: 1, name: "Y" }} />);
    const btn = getByTestId("rom-card-download") as HTMLButtonElement;
    fireEvent.click(btn);
    await waitFor(() => expect(btn.textContent).toBe("Download"));
    expect(btn.disabled).toBe(false);
    const toastMock = vi.mocked(toaster).toast;
    const calls = toastMock.mock.calls.map((c) => c[0]);
    expect(
      calls.some((c) => c?.title === "Download failed" && String(c?.body).includes("network gone")),
    ).toBe(true);
  });

  it("renders the Installed badge and Re-download label when installed=true", () => {
    const { getByTestId } = render(<RomCard rom={{ id: 1, name: "Z" }} installed />);
    const btn = getByTestId("rom-card-download") as HTMLButtonElement;
    expect(btn.dataset.installed).toBe("true");
    expect(btn.textContent).toContain("Installed");
  });

  it("still lets Re-download fire startDownload when installed=true", async () => {
    vi.mocked(startDownload).mockResolvedValue({ success: true });
    const { getByTestId } = render(<RomCard rom={{ id: 5, name: "Mario" }} installed />);
    const btn = getByTestId("rom-card-download") as HTMLButtonElement;
    fireEvent.click(btn);
    await waitFor(() => expect(vi.mocked(startDownload)).toHaveBeenCalledWith(5));
  });
});
