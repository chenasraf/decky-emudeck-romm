import type { ComponentType } from "react";
import { PluginDemo } from "./PluginDemo";

// Add new demos here. The key becomes the URL hash (#demo=name) and the
// label shown in the sidebar.
export const demos: Record<string, ComponentType> = {
  "Full QAM": PluginDemo,
};
