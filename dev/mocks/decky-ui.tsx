// Browser stand-in for @decky/ui, whose real components hook into Steam's webpack chunks and fail
// outside the Steam client. These render plain HTML in roughly Steam's QAM style: good for layout
// and state, not pixel-accurate. Add components here as src/ starts using them.

import type { CSSProperties, ReactNode } from 'react'

const muted = '#8b929a'

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
