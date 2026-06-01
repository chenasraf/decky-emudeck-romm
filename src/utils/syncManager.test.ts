import { describe, it, expect, vi, beforeEach } from "vitest";

import { initUnitSyncManager } from "./syncManager";
import { getSettings, reportUnitResults } from "../api/backend";
import { addShortcut, getExistingRomMShortcuts } from "./steamShortcuts";
import type { SyncApplyUnitData } from "../types";

vi.mock("./steamShortcuts", () => ({
  getExistingRomMShortcuts: vi.fn(),
  addShortcut: vi.fn(),
}));

vi.mock("./syncProgress", () => ({
  updateSyncProgress: vi.fn(),
}));

const _unitData: SyncApplyUnitData = {
  unit_type: "platform",
  unit_name: "N64",
  unit_index: 0,
  total_units: 1,
  shortcuts: [
    { rom_id: 1, name: "Mario 64", exe: "/run", start_dir: "/", launch_options: "" },
  ],
};

type Listener = (d: SyncApplyUnitData) => Promise<void>;

describe("initUnitSyncManager — create_shortcuts gate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(reportUnitResults).mockResolvedValue({ success: true });
    vi.mocked(getExistingRomMShortcuts).mockResolvedValue(new Map());
    vi.mocked(addShortcut).mockResolvedValue(42);
  });

  it("skips shortcut + artwork phase when create_shortcuts is off", async () => {
    vi.mocked(getSettings).mockResolvedValue({ create_shortcuts: false } as never);
    const listener = initUnitSyncManager() as unknown as Listener;
    await listener(_unitData);

    expect(vi.mocked(addShortcut)).not.toHaveBeenCalled();
    expect(vi.mocked(getExistingRomMShortcuts)).not.toHaveBeenCalled();
    expect(vi.mocked(reportUnitResults)).toHaveBeenCalledWith({});
  });

  it("runs the shortcut pipeline when create_shortcuts is on", async () => {
    vi.mocked(getSettings).mockResolvedValue({ create_shortcuts: true } as never);
    const listener = initUnitSyncManager() as unknown as Listener;
    await listener(_unitData);

    expect(vi.mocked(getExistingRomMShortcuts)).toHaveBeenCalledTimes(1);
    expect(vi.mocked(addShortcut)).toHaveBeenCalled();
  });

  it("treats getSettings failure as default-off (no shortcut creation)", async () => {
    vi.mocked(getSettings).mockRejectedValue(new Error("backend down"));
    const listener = initUnitSyncManager() as unknown as Listener;
    await listener(_unitData);

    expect(vi.mocked(addShortcut)).not.toHaveBeenCalled();
    expect(vi.mocked(reportUnitResults)).toHaveBeenCalledWith({});
  });
});
