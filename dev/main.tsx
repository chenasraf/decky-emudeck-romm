import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import definePlugin from '../src/index'

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

function Shell() {
  return (
    <div style={{ display: 'flex', gap: 24, padding: 24, alignItems: 'flex-start' }}>
      {/* The Quick Access Menu is about this wide on a Deck. */}
      <div style={{ width: 340, background: '#1a1d23', borderRadius: 4, overflow: 'hidden' }}>
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
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Shell />
  </StrictMode>,
)
