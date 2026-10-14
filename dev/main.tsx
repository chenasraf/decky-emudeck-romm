import { createElement, StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import definePlugin from '../src/index'
import { navigate, routes, useRoute } from './mocks/router'

const plugin = definePlugin()

const devButton = {
  padding: '6px 10px',
  background: '#23262e',
  color: '#dcdedf',
  border: '1px solid #3d4450',
  borderRadius: 2,
  cursor: 'pointer',
}

function post(path: string) {
  void fetch(path, { method: 'POST' })
}

/**
 * Stands in for a full-screen route, which on a Deck replaces the whole Steam UI: the size of the
 * Deck's screen in CSS pixels, so a route lays out and scrolls inside itself as it does there.
 */
function RouteView() {
  const path = useRoute()
  if (!path) return null
  const Page = routes.get(path)
  return (
    <div
      style={{
        flex: '0 0 auto',
        position: 'relative',
        width: 854,
        height: 454,
        background: '#0e141b',
        borderRadius: 4,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          position: 'absolute',
          top: 8,
          right: 12,
          display: 'flex',
          gap: 8,
          alignItems: 'center',
          color: '#8b929a',
          fontSize: 12,
        }}
      >
        <code>{path}</code>
        <button style={devButton} onClick={() => navigate(null)}>
          Close
        </button>
      </div>
      {Page ? (
        createElement(Page)
      ) : (
        <div style={{ padding: 56 }}>No route registered at {path}</div>
      )}
    </div>
  )
}

function Shell() {
  return (
    <div style={{ display: 'flex', gap: 24, padding: 24, alignItems: 'flex-start' }}>
      {/* A Deck's Quick Access panel: 300px, rows inset 16px either side to 268px of content. */}
      <div
        style={{
          width: 300,
          flexShrink: 0,
          background: '#1a1d23',
          borderRadius: 4,
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            padding: '12px 16px',
            borderBottom: '1px solid #23262e',
          }}
        >
          {plugin.icon}
          {plugin.titleView ?? <span className="dev-title">{plugin.name}</span>}
        </div>
        {plugin.content}
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <div style={{ color: '#8b929a', fontSize: 12, textTransform: 'uppercase' }}>Dev</div>
        <button style={devButton} onClick={() => post('/dev/kill')}>
          Kill backend
        </button>
        <button style={devButton} onClick={() => post('/dev/rebuild')}>
          Rebuild backend
        </button>
      </div>
      <RouteView />
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Shell />
  </StrictMode>,
)
