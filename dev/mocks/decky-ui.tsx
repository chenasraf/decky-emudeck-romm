// Browser stand-in for @decky/ui, whose real components hook into Steam's webpack chunks and fail
// outside the Steam client. These render plain HTML in roughly Steam's QAM style: good for layout
// and state, not pixel-accurate. Add components here as src/ starts using them.

import {
  type ChangeEventHandler,
  cloneElement,
  type CSSProperties,
  type HTMLAttributes,
  type ReactElement,
  type ReactNode,
} from 'react'
import { createRoot } from 'react-dom/client'

import { navigate } from './router'

const muted = '#8b929a'

export const Navigation = {
  Navigate: (path: string) => navigate(path),
  NavigateBack: () => navigate(null),
  CloseSideMenus: () => undefined,
}

export const staticClasses = { Title: 'dev-title' }

export function PanelSection({ title, children }: { title?: ReactNode; children?: ReactNode }) {
  return (
    <section style={{ padding: '8px 0' }}>
      {title && (
        <div
          style={{
            color: muted,
            fontSize: 12,
            fontWeight: 600,
            textTransform: 'uppercase',
            letterSpacing: 0.5,
            padding: '0 16px 4px',
          }}
        >
          {title}
        </div>
      )}
      {children}
    </section>
  )
}

export function PanelSectionRow({ children }: { children?: ReactNode }) {
  return <div style={{ padding: '0 16px' }}>{children}</div>
}

export function Field({
  label,
  description,
  children,
}: {
  label?: ReactNode
  description?: ReactNode
  children?: ReactNode
}) {
  return (
    <div style={{ padding: '10px 0', borderBottom: '1px solid #23262e' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
        <span style={{ color: '#fff' }}>{label}</span>
        <span>{children}</span>
      </div>
      {description && <div style={{ color: muted, fontSize: 12, marginTop: 4 }}>{description}</div>}
    </div>
  )
}

export function ProgressBarWithInfo({
  nProgress,
  sOperationText,
}: {
  nProgress?: number
  sOperationText?: ReactNode
}) {
  return (
    <div style={{ padding: '8px 0' }}>
      <div style={{ height: 6, background: '#23262e', borderRadius: 3 }}>
        <div
          style={{
            width: `${nProgress ?? 0}%`,
            height: '100%',
            background: '#1a9fff',
            borderRadius: 3,
          }}
        />
      </div>
      {sOperationText && (
        <div style={{ color: muted, fontSize: 12, marginTop: 4 }}>{sOperationText}</div>
      )}
    </div>
  )
}

export function ToggleField({
  label,
  description,
  checked,
  disabled,
  onChange,
}: {
  label?: ReactNode
  description?: ReactNode
  checked: boolean
  disabled?: boolean
  onChange?: (checked: boolean) => void
}) {
  return (
    <Field label={label} description={description}>
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange?.(e.target.checked)}
      />
    </Field>
  )
}

export function DropdownItem({
  label,
  description,
  rgOptions,
  selectedOption,
  disabled,
  onChange,
}: {
  label?: ReactNode
  description?: ReactNode
  rgOptions: { data: unknown; label: ReactNode }[]
  selectedOption: unknown
  disabled?: boolean
  onChange?: (option: { data: unknown; label: ReactNode }) => void
}) {
  const index = rgOptions.findIndex((o) => o.data === selectedOption)
  return (
    <Field label={label} description={description}>
      <select
        value={index}
        disabled={disabled}
        onChange={(e) => onChange?.(rgOptions[Number(e.target.value)])}
      >
        {rgOptions.map((o, i) => (
          <option key={i} value={i}>
            {typeof o.label === 'string' ? o.label : String(o.data)}
          </option>
        ))}
      </select>
    </Field>
  )
}

const button: CSSProperties = {
  width: '100%',
  margin: '6px 0',
  padding: '8px 12px',
  background: '#3d4450',
  color: '#fff',
  border: 'none',
  borderRadius: 2,
  cursor: 'pointer',
  fontSize: 14,
}

export function ButtonItem({
  onClick,
  disabled,
  children,
}: {
  layout?: 'below' | 'inline'
  onClick?: () => void
  disabled?: boolean
  children?: ReactNode
}) {
  return (
    <button
      style={{ ...button, opacity: disabled ? 0.5 : 1 }}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

export function DialogButton({
  onClick,
  disabled,
  style,
  children,
}: {
  onClick?: () => void
  disabled?: boolean
  style?: CSSProperties
  children?: ReactNode
}) {
  return (
    <button
      style={{ ...button, width: 'auto', minWidth: 120, opacity: disabled ? 0.5 : 1, ...style }}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

/**
 * Gamepad-only props are dropped. An element with onActivate takes keyboard focus and activates on
 * Enter, standing in for the A button.
 */
export function Focusable({
  children,
  style,
  className,
  onClick,
  onActivate,
}: HTMLAttributes<HTMLDivElement> & {
  children?: ReactNode
  'flow-children'?: string
  focusClassName?: string
  onActivate?: () => void
  onCancelButton?: () => void
  onCancelActionDescription?: ReactNode
  onOptionsButton?: () => void
  onOptionsActionDescription?: ReactNode
  onGamepadFocus?: (e: { currentTarget: EventTarget }) => void
}) {
  return (
    <div
      style={style}
      className={className}
      tabIndex={onActivate ? 0 : undefined}
      onClick={onClick}
      onKeyDown={onActivate ? (e) => e.key === 'Enter' && onActivate() : undefined}
    >
      {children}
    </div>
  )
}

export function TextField({
  label,
  description,
  value,
  disabled,
  bIsPassword,
  onChange,
}: {
  label?: ReactNode
  description?: ReactNode
  value?: string
  disabled?: boolean
  bIsPassword?: boolean
  onChange?: ChangeEventHandler<HTMLInputElement>
}) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
      {label && <span style={{ color: '#fff', fontSize: 14 }}>{label}</span>}
      <input
        type={bIsPassword ? 'password' : 'text'}
        value={value}
        disabled={disabled}
        onChange={onChange}
        style={{
          padding: '8px 10px',
          background: '#0e141b',
          color: '#fff',
          border: '1px solid #3d4450',
          borderRadius: 2,
          fontSize: 14,
        }}
      />
      {description && <span style={{ color: muted, fontSize: 12 }}>{description}</span>}
    </label>
  )
}

/**
 * Renders modal over the page, the way Steam shows it over the whole UI. The real showModal
 * passes closeModal to the element; so does this one.
 */
export function showModal(modal: ReactElement<{ closeModal?: () => void }>) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const root = createRoot(host)
  const closeModal = () => {
    root.unmount()
    host.remove()
  }
  root.render(
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0, 0, 0, 0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
    >
      {cloneElement(modal, { closeModal })}
    </div>,
  )
  return { Close: closeModal }
}

export function ConfirmModal({
  strTitle,
  strDescription,
  strOKButtonText = 'Confirm',
  strCancelButtonText = 'Cancel',
  bDestructiveWarning,
  onOK,
  onCancel,
  closeModal,
}: {
  strTitle?: ReactNode
  strDescription?: ReactNode
  strOKButtonText?: ReactNode
  strCancelButtonText?: ReactNode
  bDestructiveWarning?: boolean
  onOK?: () => void
  onCancel?: () => void
  closeModal?: () => void
}) {
  return (
    <div
      style={{ background: '#23262e', padding: 24, width: 420, borderRadius: 4, color: '#dcdedf' }}
    >
      <div style={{ color: '#fff', fontSize: 18, marginBottom: 12 }}>{strTitle}</div>
      <div style={{ color: muted, fontSize: 14, marginBottom: 20 }}>{strDescription}</div>
      <div style={{ display: 'flex', gap: 8 }}>
        <DialogButton
          style={bDestructiveWarning ? { background: '#c33' } : undefined}
          onClick={() => {
            onOK?.()
            closeModal?.()
          }}
        >
          {strOKButtonText}
        </DialogButton>
        <DialogButton
          onClick={() => {
            onCancel?.()
            closeModal?.()
          }}
        >
          {strCancelButtonText}
        </DialogButton>
      </div>
    </div>
  )
}
