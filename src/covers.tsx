import { type CSSProperties, useEffect, useState } from 'react'

import { call } from './backend'
import type { CoverSize } from './rpc/schema.gen'

/**
 * How many covers stay in memory. The backend keeps every cover on disk; this only spares a round
 * trip when a list is opened again.
 */
const MEMORY_LIMIT = 200

/** Covers by "<size>/<id>", oldest use first. */
const loaded = new Map<string, string>()
const pending = new Map<string, Promise<string | null>>()

function remember(key: string, url: string) {
  loaded.delete(key)
  loaded.set(key, url)
  if (loaded.size > MEMORY_LIMIT) {
    const oldest = loaded.keys().next().value
    if (oldest !== undefined) loaded.delete(oldest)
  }
}

/**
 * Loads a game's cover as a data URL. A cover the backend couldn't get (offline, before it was
 * ever cached) isn't remembered, so the next showing asks again.
 */
function loadCover(id: number, size: CoverSize): Promise<string | null> {
  const key = `${size}/${id}`
  const hit = loaded.get(key)
  if (hit) {
    remember(key, hit)
    return Promise.resolve(hit)
  }
  let p = pending.get(key)
  if (!p) {
    p = call('get_cover', { id, size })
      .then(
        (c) => {
          if (c.dataUrl) remember(key, c.dataUrl)
          return c.dataUrl ?? null
        },
        () => null,
      )
      .finally(() => pending.delete(key))
    pending.set(key, p)
  }
  return p
}

const SIZES: Record<CoverSize, CSSProperties> = {
  small: { width: 30, height: 40 },
  large: { width: 120, height: 160 },
}

/** A game's cover, or a blank of the same shape while it loads or when there's none. */
export function Cover({ id, size, hasCover }: { id: number; size: CoverSize; hasCover: boolean }) {
  const key = `${size}/${id}`
  // The cover loaded last, with the game and size it's for, so a reused component never shows the
  // previous game's cover while the new one loads.
  const [shown, setShown] = useState<{ key: string; url: string | null }>(() => ({
    key,
    url: loaded.get(key) ?? null,
  }))

  useEffect(() => {
    if (!hasCover) return
    let current = true
    void loadCover(id, size).then((url) => {
      if (current) setShown({ key: `${size}/${id}`, url })
    })
    return () => {
      current = false
    }
  }, [id, size, hasCover])

  const url = hasCover && shown.key === key ? shown.url : null

  const style: CSSProperties = {
    ...SIZES[size],
    flexShrink: 0,
    borderRadius: 2,
    objectFit: 'cover',
    background: 'rgba(255, 255, 255, 0.08)',
  }
  return url ? <img src={url} style={style} alt="" /> : <div style={style} />
}
