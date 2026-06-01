/**
 * Plugin-wide developer/diagnostic toggles. Houses the log-level dropdown and
 * the Steam-shortcut creation toggle. Future additions belong here when
 * they're orthogonal to any other panel. Pure renderer: parent owns the
 * current values.
 */

import { FC } from "react";
import { PanelSection, PanelSectionRow, DropdownItem, ToggleField } from "@decky/ui";

interface AdvancedSectionProps {
  logLevel: string;
  createShortcuts: boolean;
  onLogLevelChange: (level: string) => void;
  onCreateShortcutsChange: (enabled: boolean) => void;
}

export const AdvancedSection: FC<AdvancedSectionProps> = ({
  logLevel,
  createShortcuts,
  onLogLevelChange,
  onCreateShortcutsChange,
}) => {
  return (
    <PanelSection title="Advanced">
      <PanelSectionRow>
        <ToggleField
          label="Create Steam Shortcuts"
          description="When off (the default), ROM downloads still happen but no Non-Steam shortcuts are created. Turn this on to mirror your library into Steam."
          checked={createShortcuts}
          onChange={onCreateShortcutsChange}
        />
      </PanelSectionRow>
      <PanelSectionRow>
        <DropdownItem
          label="Log Level"
          description="Controls how much detail is written to plugin logs"
          rgOptions={[
            { data: "error", label: "Error" },
            { data: "warn", label: "Warn" },
            { data: "info", label: "Info" },
            { data: "debug", label: "Debug" },
          ]}
          selectedOption={logLevel}
          onChange={(option) => onLogLevelChange(option.data)}
        />
      </PanelSectionRow>
    </PanelSection>
  );
};
