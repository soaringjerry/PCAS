import { useCallback, useEffect, useState } from 'react'

function typing(): boolean {
  const el = document.activeElement as HTMLElement | null
  if (!el) return false
  return ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || el.isContentEditable || Boolean(el.closest('[role="dialog"]'))
}

interface Options {
  onOpen: (id: string) => void
  /** Return true when the key was handled. `ids` is the selection, or the row under the cursor. */
  onKey?: (key: string, ids: string[]) => boolean
  onCursor?: (id?: string) => void
}

/** J/K cursor, X to pick rows, Enter to open, plus view-specific keys. */
export function useListNav(ids: string[], { onOpen, onKey, onCursor }: Options) {
  const [cursorId, setCursorId] = useState<string | undefined>(undefined)
  const [picked, setPicked] = useState<string[]>([])

  // Keep the cursor on a row that still exists.
  const cursor = cursorId && ids.includes(cursorId) ? cursorId : ids[0]
  const pickedLive = picked.filter((id) => ids.includes(id))

  useEffect(() => {
    onCursor?.(cursor)
  }, [cursor, onCursor])

  const toggle = useCallback((id: string) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id])), [])
  const clear = useCallback(() => setPicked([]), [])

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || typing() || ids.length === 0) return
      const index = cursor ? ids.indexOf(cursor) : -1
      const key = e.key.length === 1 ? e.key.toLowerCase() : e.key
      if (key === 'j' || key === 'ArrowDown') {
        e.preventDefault()
        setCursorId(ids[Math.min(index + 1, ids.length - 1)])
        return
      }
      if (key === 'k' || key === 'ArrowUp') {
        e.preventDefault()
        setCursorId(ids[Math.max(index - 1, 0)])
        return
      }
      if (key === 'Enter' && cursor) {
        e.preventDefault()
        onOpen(cursor)
        return
      }
      if (key === 'x' && cursor) {
        e.preventDefault()
        toggle(cursor)
        return
      }
      if (key === 'Escape' && pickedLive.length) {
        clear()
        return
      }
      const targets = pickedLive.length ? pickedLive : cursor ? [cursor] : []
      if (targets.length && onKey?.(key, targets)) {
        e.preventDefault()
        if (pickedLive.length) clear()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [ids, cursor, pickedLive, onOpen, onKey, toggle, clear])

  return { cursor, setCursor: setCursorId, picked: pickedLive, toggle, clear }
}
