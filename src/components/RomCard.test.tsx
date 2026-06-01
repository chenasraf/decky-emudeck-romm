import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { RomCard } from "./RomCard";
import { getBrowseCoverBase64 } from "../api/backend";

vi.mock("../api/backend", () => ({
  getBrowseCoverBase64: vi.fn(),
}));

describe("RomCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the ROM name", () => {
    vi.mocked(getBrowseCoverBase64).mockReturnValue(new Promise(() => {}));
    const { getByTestId } = render(<RomCard rom={{ id: 1, name: "Zelda" }} />);
    expect(getByTestId("rom-card-title").textContent).toBe("Zelda");
  });

  it("falls back to the ROM id when name is missing", () => {
    vi.mocked(getBrowseCoverBase64).mockReturnValue(new Promise(() => {}));
    const { getByTestId } = render(<RomCard rom={{ id: 7 }} />);
    expect(getByTestId("rom-card-title").textContent).toBe("ROM 7");
  });

  it("renders the cover img once base64 resolves", async () => {
    vi.mocked(getBrowseCoverBase64).mockResolvedValue({ success: true, base64: "AAAA" });
    const { findByRole } = render(<RomCard rom={{ id: 1, name: "Z" }} />);
    const img = (await findByRole("img")) as HTMLImageElement;
    expect(img.src).toBe("data:image/jpeg;base64,AAAA");
    expect(img.loading).toBe("lazy");
  });

  it("shows the 'No art' placeholder when the cover resolves null", async () => {
    vi.mocked(getBrowseCoverBase64).mockResolvedValue({ success: true, base64: null });
    const { findByText } = render(<RomCard rom={{ id: 1 }} />);
    expect(await findByText("No art")).toBeDefined();
  });

  it("falls into the 'No art' placeholder on callable rejection", async () => {
    vi.mocked(getBrowseCoverBase64).mockRejectedValue(new Error("offline"));
    const { findByText } = render(<RomCard rom={{ id: 1 }} />);
    expect(await findByText("No art")).toBeDefined();
  });

  it("re-fetches the cover when the rom id changes", async () => {
    vi.mocked(getBrowseCoverBase64).mockResolvedValue({ success: true, base64: null });
    const { rerender } = render(<RomCard rom={{ id: 1 }} />);
    await waitFor(() => expect(getBrowseCoverBase64).toHaveBeenCalledWith(1));
    rerender(<RomCard rom={{ id: 2 }} />);
    await waitFor(() => expect(getBrowseCoverBase64).toHaveBeenCalledWith(2));
  });
});
