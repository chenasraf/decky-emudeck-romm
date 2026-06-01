import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { createElement } from "react";
import { FrontendSection } from "./FrontendSection";

interface DropdownOption { data: unknown; label: string }
interface DropdownItemProps {
  label?: string;
  description?: string;
  rgOptions?: DropdownOption[];
  selectedOption?: unknown;
  onChange?: (option: DropdownOption) => void;
}
interface FieldProps {
  label?: string;
  description?: string;
}
const captured: { dropdowns: DropdownItemProps[]; fields: FieldProps[] } = { dropdowns: [], fields: [] };

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
    Field: (p: FieldProps) => {
      captured.fields.push(p);
      return createElement("div", { "data-testid": "field" }, p.label as never);
    },
  };
});

describe("FrontendSection", () => {
  beforeEach(() => {
    captured.dropdowns = [];
    captured.fields = [];
  });

  it("renders the dropdown with the four canonical options", () => {
    render(<FrontendSection frontend="auto" onFrontendChange={vi.fn()} />);
    expect(captured.dropdowns).toHaveLength(1);
    const item = captured.dropdowns[0];
    expect(item?.label).toBe("Emulator frontend");
    expect(item?.rgOptions?.map((o) => o.data)).toEqual([
      "auto",
      "emudeck",
      "retrodeck",
      "custom",
    ]);
  });

  it("forwards the current frontend value as selectedOption", () => {
    render(<FrontendSection frontend="emudeck" onFrontendChange={vi.fn()} />);
    expect(captured.dropdowns[0]?.selectedOption).toBe("emudeck");
  });

  it("dispatches onFrontendChange with option.data when the dropdown fires", () => {
    const onChange = vi.fn();
    render(<FrontendSection frontend="auto" onFrontendChange={onChange} />);
    captured.dropdowns[0]?.onChange?.({ data: "retrodeck", label: "RetroDECK" });
    expect(onChange).toHaveBeenCalledWith("retrodeck");
  });

  it("does not render the Custom note when a non-custom value is selected", () => {
    render(<FrontendSection frontend="auto" onFrontendChange={vi.fn()} />);
    expect(captured.fields).toHaveLength(0);
  });

  it("renders the Custom note only when frontend === 'custom'", () => {
    render(<FrontendSection frontend="custom" onFrontendChange={vi.fn()} />);
    expect(captured.fields).toHaveLength(1);
    expect(captured.fields[0]?.description).toContain("Custom path overrides are coming");
  });
});
