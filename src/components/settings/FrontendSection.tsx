/**
 * Host emulator-frontend selector. Drives ``bootstrap._select_frontend``
 * on the next plugin reload — the running adapter is not swapped live,
 * so changes only take effect after the user reloads the plugin.
 *
 * ``Custom`` is selectable but path overrides are not yet implemented:
 * an inline note tells the user the choice falls through to autodetect
 * for now. Pure renderer: parent owns the selected value + save callable.
 */

import { FC } from "react";
import { PanelSection, PanelSectionRow, DropdownItem, Field } from "@decky/ui";
import type { FrontendChoice } from "../../types";

interface FrontendSectionProps {
  frontend: FrontendChoice;
  onFrontendChange: (value: FrontendChoice) => void;
}

const OPTIONS: { data: FrontendChoice; label: string }[] = [
  { data: "auto", label: "Auto-detect" },
  { data: "emudeck", label: "EmuDeck" },
  { data: "retrodeck", label: "RetroDECK" },
  { data: "custom", label: "Custom" },
];

export const FrontendSection: FC<FrontendSectionProps> = ({ frontend, onFrontendChange }) => {
  return (
    <PanelSection title="Frontend">
      <PanelSectionRow>
        <DropdownItem
          label="Emulator frontend"
          description="Which emulator frontend the plugin writes ROMs, BIOS, and saves under. Takes effect on the next plugin reload."
          rgOptions={OPTIONS}
          selectedOption={frontend}
          onChange={(option) => onFrontendChange(option.data as FrontendChoice)}
        />
      </PanelSectionRow>
      {frontend === "custom" && (
        <PanelSectionRow>
          <Field
            label="Custom paths"
            description="Custom path overrides are coming in a later release; behaves as Auto-detect for now."
          />
        </PanelSectionRow>
      )}
    </PanelSection>
  );
};
