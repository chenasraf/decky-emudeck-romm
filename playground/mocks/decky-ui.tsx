// Browser-side passthrough shim for @decky/ui. Real @decky/ui hooks into
// Steam's webpack chunks at import time (window.webpackChunksteamui) and
// blows up outside the Steam client. Components here render to plain HTML
// with light Steam-ish styling — enough for layout/state iteration, NOT
// pixel-perfect previews.

import {
  createContext,
  createElement,
  Fragment,
  cloneElement,
  isValidElement,
  useContext,
  type CSSProperties,
  type ReactElement,
  type ReactNode,
} from "react";
import { createRoot, type Root } from "react-dom/client";

// Any ConfirmModal / ModalRoot inside the overlay reads this to self-close.
const ModalCloseContext = createContext<() => void>(() => {});

type AnyProps = Record<string, unknown> & { children?: ReactNode };

const btn: CSSProperties = {
  padding: "8px 16px",
  background: "#67c1f5",
  color: "#1b2838",
  border: "none",
  borderRadius: 2,
  cursor: "pointer",
  fontWeight: 600,
};

const btnDisabled: CSSProperties = { ...btn, background: "#3a4856", color: "#7a8a9d", cursor: "not-allowed" };

const panel: CSSProperties = { padding: "12px 0", borderBottom: "1px solid #2a3548" };

export const ConfirmModal = (
  p: AnyProps & {
    strTitle?: ReactNode;
    strDescription?: ReactNode;
    strOKButtonText?: ReactNode;
    strCancelButtonText?: ReactNode;
    bAlertDialog?: boolean;
    bHideCloseIcon?: boolean;
    onOK?: () => void;
    onCancel?: () => void;
    closeModal?: () => void;
  },
) => {
  const closeFromCtx = useContext(ModalCloseContext);
  const close = p.closeModal ?? closeFromCtx;
  const handleOK = () => {
    p.onOK?.();
    close();
  };
  const handleCancel = () => {
    p.onCancel?.();
    close();
  };
  return createElement(
    "div",
    { "data-shim": "ConfirmModal" },
    p.strTitle ? createElement("h3", { style: { margin: "0 0 8px", color: "#fff" } }, p.strTitle) : null,
    p.strDescription ? createElement("div", { style: { color: "#c7d5e0", marginBottom: 12 } }, p.strDescription) : null,
    createElement("div", { style: { marginBottom: 12 } }, p.children),
    createElement(
      "div",
      { style: { display: "flex", gap: 8, justifyContent: "flex-end" } },
      !p.bAlertDialog
        ? createElement(
            "button",
            { onClick: handleCancel, style: { ...btnDisabled, cursor: "pointer", color: "#c7d5e0" } },
            (p.strCancelButtonText as ReactNode) ?? "Cancel",
          )
        : null,
      createElement(
        "button",
        { onClick: handleOK, style: btn },
        (p.strOKButtonText as ReactNode) ?? "OK",
      ),
    ),
  );
};

export const ModalRoot = (
  p: AnyProps & { onCancel?: () => void; onOK?: () => void; bAllowFullSize?: boolean; closeModal?: () => void },
) => {
  const closeFromCtx = useContext(ModalCloseContext);
  // ModalRoot in real @decky/ui injects closeModal into children automatically.
  // Approximate that by forwarding the context's close as a `closeModal` prop
  // to any direct ReactElement children.
  const close = p.closeModal ?? closeFromCtx;
  const inject = (n: ReactNode): ReactNode => {
    if (!isValidElement(n)) return n;
    const childProps = (n.props ?? {}) as Record<string, unknown>;
    if ("closeModal" in childProps) return n;
    return cloneElement(n as ReactElement, { closeModal: close } as Record<string, unknown>);
  };
  const kids = Array.isArray(p.children)
    ? (p.children as ReactNode[]).map(inject)
    : inject(p.children as ReactNode);
  return createElement("div", { "data-shim": "ModalRoot" }, kids);
};

export const DialogButton = (p: AnyProps & { onClick?: () => void; disabled?: boolean }) =>
  createElement("button", { onClick: p.onClick, disabled: p.disabled, style: p.disabled ? btnDisabled : btn }, p.children);
export const DialogButtonPrimary = DialogButton;
export const ButtonItem = DialogButton;

export const Field = (p: AnyProps & { label?: ReactNode; description?: ReactNode }) =>
  createElement(
    "div",
    { "data-shim": "Field", style: panel },
    createElement("div", { style: { color: "#fff", fontWeight: 600 } }, p.label),
    createElement("div", { style: { color: "#7a8a9d", fontSize: 12 } }, p.description),
    p.children,
  );

export const Focusable = (p: AnyProps & { style?: CSSProperties }) =>
  createElement("div", { "data-shim": "Focusable", style: p.style, tabIndex: 0 }, p.children);

export const GamepadButton = {
  OK: 1,
  CANCEL: 2,
  SECONDARY: 3,
  TRIGGER_RIGHT: 8,
  DIR_UP: 9,
  DIR_DOWN: 10,
};

export const PanelSection = (p: AnyProps & { title?: ReactNode }) =>
  createElement(
    "section",
    { "data-shim": "PanelSection", style: { marginBottom: 16 } },
    p.title ? createElement("h4", { style: { color: "#66c0f4", margin: "0 0 8px" } }, p.title) : null,
    p.children,
  );

export const PanelSectionRow = (p: AnyProps) =>
  createElement("div", { "data-shim": "PanelSectionRow", style: { padding: "4px 0" } }, p.children);

export const TextField = (p: AnyProps & { value?: string; onChange?: (e: { target: { value: string } }) => void; label?: ReactNode }) =>
  createElement(
    "label",
    { style: { display: "block", padding: "4px 0" } },
    p.label ? createElement("div", { style: { color: "#fff", fontSize: 12, marginBottom: 4 } }, p.label) : null,
    createElement("input", {
      type: "text",
      value: p.value ?? "",
      onChange: (e: React.ChangeEvent<HTMLInputElement>) => p.onChange?.({ target: { value: e.target.value } }),
      style: { background: "#1b2838", color: "#c7d5e0", border: "1px solid #2a3548", padding: "6px 8px", borderRadius: 2, width: "100%" },
    }),
  );

export const ToggleField = (p: AnyProps & { checked?: boolean; onChange?: (v: boolean) => void; label?: ReactNode }) =>
  createElement(
    "label",
    { style: { display: "flex", alignItems: "center", gap: 8, padding: "4px 0", color: "#fff" } },
    createElement("input", {
      type: "checkbox",
      checked: p.checked ?? false,
      onChange: (e: React.ChangeEvent<HTMLInputElement>) => p.onChange?.(e.target.checked),
    }),
    typeof p.label === "string" ? p.label : (p.label as ReactNode),
  );

export const Dropdown = (p: AnyProps) => createElement("select", { "data-shim": "Dropdown" }, p.children);
export const DropdownItem = (p: AnyProps & { rgOptions?: Array<{ label: ReactNode; data?: unknown }> }) =>
  createElement(
    "select",
    { "data-shim": "DropdownItem" },
    (p.rgOptions ?? []).map((opt, i) => createElement("option", { key: i }, String(opt.label))),
  );

export const Spinner = () =>
  createElement(
    "span",
    {
      "data-shim": "Spinner",
      style: {
        display: "inline-block",
        width: 16,
        height: 16,
        border: "2px solid #66c0f4",
        borderTopColor: "transparent",
        borderRadius: "50%",
        animation: "decky-spin 1s linear infinite",
      },
    },
  );

export const Menu = (p: AnyProps) => createElement("div", { "data-shim": "Menu", style: { background: "#1e3346", padding: 8, borderRadius: 4 } }, p.children);
export const MenuItem = (p: AnyProps & { onClick?: () => void }) =>
  createElement("button", { onClick: p.onClick, style: { ...btn, display: "block", width: "100%", marginBottom: 4 } }, p.children);

type ModalHandle = { Close: () => void };

function mountOverlay(content: (close: () => void) => ReactNode): ModalHandle {
  if (typeof document === "undefined") return { Close: () => {} };
  const host = document.createElement("div");
  Object.assign(host.style, {
    position: "fixed",
    inset: "0",
    zIndex: "10000",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    background: "rgba(0,0,0,0.55)",
  } as CSSStyleDeclaration);
  document.body.appendChild(host);
  const root: Root = createRoot(host);
  const close = () => {
    queueMicrotask(() => {
      root.unmount();
      host.remove();
    });
  };
  host.addEventListener("click", (e) => {
    if (e.target === host) close();
  });
  root.render(
    createElement(
      ModalCloseContext.Provider,
      { value: close },
      createElement(
        "div",
        { style: { background: "#1b2838", border: "1px solid #2a3548", borderRadius: 4, padding: 20, minWidth: 320, maxWidth: 480, color: "#c7d5e0" } },
        content(close),
      ),
    ),
  );
  return { Close: close };
}

// Inject `closeModal` and `onCancel`/`onOK` close-then-call wrappers into
// the modal element's props if the consumer expects them (matches @decky/ui's
// real showModal behaviour for ConfirmModal et al).
function withCloseProps(el: ReactElement, close: () => void): ReactElement {
  if (!isValidElement(el)) return el;
  const props = (el.props ?? {}) as Record<string, unknown>;
  const wrap = (k: string) => {
    const orig = props[k];
    if (typeof orig === "function") {
      return (...args: unknown[]) => {
        try {
          (orig as (...a: unknown[]) => unknown)(...args);
        } finally {
          close();
        }
      };
    }
    return orig;
  };
  return cloneElement(el, {
    closeModal: close,
    onCancel: wrap("onCancel"),
    onOK: wrap("onOK"),
    onEscKeypress: wrap("onEscKeypress"),
  } as Record<string, unknown>);
}

export const showModal = (modal: ReactNode): ModalHandle => {
  console.info("[playground] showModal", modal);
  return mountOverlay((close) =>
    isValidElement(modal) ? withCloseProps(modal as ReactElement, close) : (modal as ReactNode),
  );
};

export const showContextMenu = (menu: ReactNode): ModalHandle => {
  console.info("[playground] showContextMenu", menu);
  return mountOverlay((close) =>
    isValidElement(menu) ? withCloseProps(menu as ReactElement, close) : (menu as ReactNode),
  );
};

export const Router = {
  CloseSideMenus: () => console.info("[playground] Router.CloseSideMenus"),
  Navigate: (path: string) => console.info("[playground] Router.Navigate", path),
};

export const findSP = () => undefined;
export const appActionButtonClasses: Record<string, string> = {};
export const basicAppDetailsSectionStylerClasses: Record<string, string> = {};
export const appDetailsClasses: Record<string, string> = {};
export const playSectionClasses: Record<string, string> = {};

export const ProgressBar = (p: AnyProps & { nProgress?: number; nTransitionSec?: number }) => {
  const pct = Math.max(0, Math.min(100, Number(p.nProgress ?? 0)));
  return createElement(
    "div",
    {
      "data-shim": "ProgressBar",
      style: { width: "100%", height: 6, background: "#2a3548", borderRadius: 3, overflow: "hidden" },
    },
    createElement("div", {
      style: {
        width: `${pct}%`,
        height: "100%",
        background: "#66c0f4",
        transition: `width ${p.nTransitionSec ?? 0.2}s linear`,
      },
    }),
  );
};

export const ProgressBarWithInfo = (
  p: AnyProps & { nProgress?: number; sOperationText?: ReactNode; sTimeRemaining?: ReactNode; description?: ReactNode },
) =>
  createElement(
    "div",
    { "data-shim": "ProgressBarWithInfo", style: { padding: "4px 0" } },
    p.sOperationText ? createElement("div", { style: { color: "#fff", fontSize: 12, marginBottom: 4 } }, p.sOperationText) : null,
    createElement(ProgressBar, { nProgress: p.nProgress }),
    p.sTimeRemaining ? createElement("div", { style: { color: "#7a8a9d", fontSize: 11, marginTop: 4 } }, p.sTimeRemaining) : null,
    p.description ? createElement("div", { style: { color: "#7a8a9d", fontSize: 11 } }, p.description) : null,
  );

export const MenuSeparator = () =>
  createElement("hr", { "data-shim": "MenuSeparator", style: { border: "none", borderTop: "1px solid #2a3548", margin: "4px 0" } });

// Patch utilities — no-ops outside Steam's React tree.
export const afterPatch = () => () => {};
export const findInReactTree = () => null;
export const createReactTreePatcher = () => () => () => {};

export type GamepadEvent = { detail?: { button?: number } };

// Catch-all for less common exports — components that import something we
// haven't shimmed will get this as a Fragment passthrough rather than
// undefined.
export const __unknown = (p: AnyProps) => createElement(Fragment, null, p.children);
