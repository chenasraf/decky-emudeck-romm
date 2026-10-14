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

// Sizes and colors are measured from the Deck's Quick Access Menu (CEF at 854×454 CSS px, 1.5×).
const muted = 'rgb(184, 188, 191)'
const text = 'rgb(220, 222, 223)'
const separator = '1px solid rgba(255, 255, 255, 0.1)'

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
            color: 'rgba(255, 255, 255, 0.7)',
            fontSize: 16,
            lineHeight: '22px',
            fontWeight: 600,
            textTransform: 'uppercase',
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

interface ItemProps {
  label?: ReactNode
  description?: ReactNode
  children?: ReactNode
  /** Steam's default, inline, sets children in a column right of the label, even with no label. */
  layout?: 'below' | 'inline'
  icon?: ReactNode
  bottomSeparator?: 'standard' | 'thick' | 'none'
  focusable?: boolean
  onActivate?: () => void
}

/** Field names it childrenLayout; the Item components built on it, layout. */
type FieldProps = Omit<ItemProps, 'layout'> & { childrenLayout?: 'below' | 'inline' }

export function Field({
  label,
  description,
  children,
  childrenLayout: layout = 'inline',
  icon,
  bottomSeparator = 'standard',
  focusable,
  onActivate,
}: FieldProps) {
  const head = (
    <span
      style={{ display: 'flex', alignItems: 'center', gap: 8, color: text, lineHeight: '20px' }}
    >
      {icon}
      {label}
    </span>
  )
  const desc = description && (
    <div style={{ color: muted, fontSize: 12, lineHeight: '16px', marginTop: 4 }}>
      {description}
    </div>
  )
  return (
    <div
      tabIndex={focusable ? 0 : undefined}
      onClick={onActivate}
      onKeyDown={(e) => {
        if (e.key === 'Enter') onActivate?.()
      }}
      style={{
        padding: '10px 0',
        borderBottom: bottomSeparator === 'none' ? undefined : separator,
        cursor: onActivate ? 'pointer' : undefined,
      }}
    >
      {layout === 'below' ? (
        <>
          {label !== undefined && head}
          {children !== undefined && (
            <div style={{ marginTop: label !== undefined ? 6 : 0 }}>{children}</div>
          )}
          {desc}
        </>
      ) : (
        // Inline, the description wraps inside the label's column, not under the whole row.
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <div style={{ flex: '1 1 auto', minWidth: 0 }}>
            {head}
            {desc}
          </div>
          {children !== undefined && (
            <div style={{ flex: '0 1 auto', minWidth: 0, maxWidth: '50%', textAlign: 'right' }}>
              {children}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export function ProgressBarWithInfo({
  nProgress,
  sOperationText,
  sTimeRemaining,
  layout,
  bottomSeparator,
  label,
  description,
}: ItemProps & {
  nProgress?: number
  sOperationText?: ReactNode
  sTimeRemaining?: ReactNode
}) {
  return (
    <Field
      label={label}
      description={description}
      childrenLayout={layout}
      bottomSeparator={bottomSeparator}
    >
      <div style={{ minWidth: 120 }}>
        {(sOperationText || sTimeRemaining) && (
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              gap: 8,
              color: '#1a9fff',
              fontSize: 12,
              fontWeight: 600,
              textTransform: 'uppercase',
              marginBottom: 4,
            }}
          >
            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {sOperationText}
            </span>
            <span>{sTimeRemaining}</span>
          </div>
        )}
        <div style={{ height: 6, background: 'rgba(255, 255, 255, 0.1)', borderRadius: 3 }}>
          <div
            style={{
              width: `${nProgress ?? 0}%`,
              height: '100%',
              background: '#1a9fff',
              borderRadius: 3,
            }}
          />
        </div>
      </div>
    </Field>
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
  minHeight: 40,
  padding: '10px 24px',
  background: 'rgba(255, 255, 255, 0.15)',
  color: text,
  border: 'none',
  borderRadius: 10,
  cursor: 'pointer',
  fontFamily: 'inherit',
  fontSize: 16,
  lineHeight: '20px',
}

/** A button in a Field row, as Steam lays it out: 10px above and below, a separator under. */
export function ButtonItem({
  onClick,
  disabled,
  children,
  label,
  description,
  bottomSeparator = 'standard',
}: ItemProps & {
  onClick?: () => void
  disabled?: boolean
}) {
  return (
    <Field
      label={label}
      description={description}
      childrenLayout="below"
      bottomSeparator={bottomSeparator}
    >
      <button
        style={{ ...button, opacity: disabled ? 0.5 : 1 }}
        disabled={disabled}
        onClick={onClick}
      >
        {children}
      </button>
    </Field>
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
      style={{ ...button, opacity: disabled ? 0.5 : 1, ...style }}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

/**
 * Keys stand in for the gamepad: an element with onActivate takes keyboard focus and activates on
 * Enter (A); Escape is B and y is Y for the nearest Focusable that handles them. Other gamepad
 * props are dropped.
 */
export function Focusable({
  children,
  style,
  className,
  onClick,
  onActivate,
  onCancelButton,
  onOptionsButton,
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
      onKeyDown={(e) => {
        const typing = e.target instanceof HTMLInputElement
        let handler: (() => void) | undefined
        if (e.key === 'Enter' && e.target === e.currentTarget) handler = onActivate
        else if (e.key === 'Escape') handler = onCancelButton
        else if (e.key === 'y' && !typing) handler = onOptionsButton
        if (!handler) return
        e.preventDefault()
        e.stopPropagation()
        handler()
      }}
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

/** The frame of a custom modal; showModal passes closeModal, and Escape calls it as B does. */
export function ModalRoot({
  children,
  closeModal,
}: {
  children?: ReactNode
  closeModal?: () => void
}) {
  return (
    <div
      onKeyDown={(e) => {
        if (e.key === 'Escape') closeModal?.()
      }}
      style={{
        background: '#23262e',
        padding: 24,
        maxWidth: 'calc(100vw - 64px)',
        maxHeight: 'calc(100vh - 64px)',
        overflowY: 'auto',
        borderRadius: 4,
        color: '#dcdedf',
      }}
    >
      {children}
    </div>
  )
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
