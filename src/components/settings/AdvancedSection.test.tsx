import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { createElement } from "react";
import { AdvancedSection } from "./AdvancedSection";

interface DropdownOption { data: unknown; label: string }
interface DropdownItemProps {
  label?: string;
  rgOptions?: DropdownOption[];
  selectedOption?: unknown;
  onChange?: (option: DropdownOption) => void;
}
interface ToggleFieldProps {
  label?: string;
  checked?: boolean;
  onChange?: (value: boolean) => void;
}
const captured: { dropdowns: DropdownItemProps[]; toggles: ToggleFieldProps[] } = {
  dropdowns: [],
  toggles: [],
};

vi.mock("@decky/ui", () => {
  type AnyProps = Record<string, unknown> & { children?: unknown };
  const passthrough = (tag: string) => (p: AnyProps) =>
    createElement(tag, {}, p.children as never);
  return {
    PanelSection: passthrough("section"),
    PanelSectionRow: passthrough("div"),
    DropdownItem: (p: DropdownItemProps) => {
      captured.dropdowns.push(p);
      return createElement("div", { "data-testid": "dropdown" }, p.label as never);
    },
    ToggleField: (p: ToggleFieldProps) => {
      captured.toggles.push(p);
      return createElement("div", { "data-testid": "toggle" }, p.label as never);
    },
  };
});

const noop = () => {};

describe("AdvancedSection", () => {
  beforeEach(() => {
    captured.dropdowns = [];
    captured.toggles = [];
  });

  it("renders the log-level dropdown with the four canonical options", () => {
    render(
      <AdvancedSection
        logLevel="info"
        createShortcuts={false}
        onLogLevelChange={vi.fn()}
        onCreateShortcutsChange={noop}
      />,
    );
    expect(captured.dropdowns).toHaveLength(1);
    expect(captured.dropdowns[0]?.label).toBe("Log Level");
    expect(captured.dropdowns[0]?.rgOptions?.map((o) => o.data)).toEqual([
      "error",
      "warn",
      "info",
      "debug",
    ]);
  });

  it("forwards the current logLevel as selectedOption", () => {
    render(
      <AdvancedSection
        logLevel="debug"
        createShortcuts={false}
        onLogLevelChange={vi.fn()}
        onCreateShortcutsChange={noop}
      />,
    );
    expect(captured.dropdowns[0]?.selectedOption).toBe("debug");
  });

  it("dispatches onLogLevelChange with option.data when the dropdown fires", () => {
    const onChange = vi.fn();
    render(
      <AdvancedSection
        logLevel="info"
        createShortcuts={false}
        onLogLevelChange={onChange}
        onCreateShortcutsChange={noop}
      />,
    );
    captured.dropdowns[0]?.onChange?.({ data: "warn", label: "Warn" });
    expect(onChange).toHaveBeenCalledWith("warn");
  });

  it("renders Create Steam Shortcuts toggle reflecting the current value", () => {
    render(
      <AdvancedSection
        logLevel="info"
        createShortcuts={true}
        onLogLevelChange={noop}
        onCreateShortcutsChange={noop}
      />,
    );
    expect(captured.toggles).toHaveLength(1);
    expect(captured.toggles[0]?.label).toBe("Create Steam Shortcuts");
    expect(captured.toggles[0]?.checked).toBe(true);
  });

  it("dispatches onCreateShortcutsChange when the toggle fires", () => {
    const onChange = vi.fn();
    render(
      <AdvancedSection
        logLevel="info"
        createShortcuts={false}
        onLogLevelChange={noop}
        onCreateShortcutsChange={onChange}
      />,
    );
    captured.toggles[0]?.onChange?.(true);
    expect(onChange).toHaveBeenCalledWith(true);
  });
});
