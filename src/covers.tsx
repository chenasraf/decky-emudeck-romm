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

/** A hue of its own for each name, so text covers side by side tell apart at a glance. */
function hueOf(name: string): number {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0
  return h % 360
}

/**
 * Stands in for a cover a game has none of: its name over a gradient of its own hue. Too small for
 * the name to read, it shows the first letter.
 */
function TextCover({
  name,
  initial,
  style,
}: {
  name: string
  initial: boolean
  style: CSSProperties
}) {
  const hue = hueOf(name)
  return (
    <div
      style={{
        ...style,
        // Container units scale the title with whatever box the cover fills.
        containerType: 'size',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        overflow: 'hidden',
        background: `linear-gradient(160deg, hsl(${hue}, 45%, 32%), hsl(${(hue + 40) % 360}, 50%, 16%))`,
        color: '#fff',
        fontWeight: 700,
        textAlign: 'center',
        textShadow: '0 1px 3px rgba(0, 0, 0, 0.6)',
      }}
    >
      {initial ? (
        <span style={{ fontSize: '60cqh', lineHeight: 1 }}>{[...name.trim()][0] ?? '?'}</span>
      ) : (
        <span
          style={{
            padding: '8%',
            fontSize: 'clamp(10px, 12cqw, 24px)',
            lineHeight: 1.2,
            overflowWrap: 'anywhere',
            display: '-webkit-box',
            WebkitBoxOrient: 'vertical',
            WebkitLineClamp: 6,
            overflow: 'hidden',
          }}
        >
          {name}
        </span>
      )}
    </div>
  )
}

/**
 * A game's cover: a blank of the same shape while it loads, and, given the game's name, a text
 * cover when it has none or it couldn't load. style overrides the size's box, e.g. to fill a tile.
 */
export function Cover({
  id,
  size,
  hasCover,
  name,
  style: override,
}: {
  id: number
  size: CoverSize
  hasCover: boolean
  name?: string
  style?: CSSProperties
}) {
  const key = `${size}/${id}`
  // The cover loaded last, with the game and size it's for, so a reused component never shows the
  // previous game's cover while the new one loads. done is set once the load settled, found or not.
  const [shown, setShown] = useState<{ key: string; url: string | null; done: boolean }>(() => {
    const url = loaded.get(key) ?? null
    return { key, url, done: url !== null }
  })

  useEffect(() => {
    if (!hasCover) return
    let current = true
    void loadCover(id, size).then((url) => {
      if (current) setShown({ key: `${size}/${id}`, url, done: true })
    })
    return () => {
      current = false
    }
  }, [id, size, hasCover])

  const mine = shown.key === key
  const url = hasCover && mine ? shown.url : null
  const missing = !hasCover || (mine && shown.done && !url)

  const style: CSSProperties = {
    ...SIZES[size],
    flexShrink: 0,
    borderRadius: 2,
    objectFit: 'cover',
    background: 'rgba(255, 255, 255, 0.08)',
    ...override,
  }
  if (url) return <img src={url} style={style} alt="" />
  if (missing && name)
    return <TextCover name={name} initial={size === 'small' && !override} style={style} />
  return <div style={style} />
}
