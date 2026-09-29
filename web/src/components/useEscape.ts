import { useEffect, useRef } from 'react'

// Open layers, innermost last. Escape only closes the top one, so a dropdown
// inside a sheet closes the dropdown, not the sheet.
const layers: symbol[] = []

export function useEscape(onClose: () => void) {
  const latest = useRef(onClose)
  useEffect(() => {
    latest.current = onClose
  })
  useEffect(() => {
    const id = Symbol('layer')
    layers.push(id)
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented || layers.at(-1) !== id) return
      e.preventDefault()
      latest.current()
    }
    window.addEventListener('keydown', onKey)
    return () => {
      layers.splice(layers.indexOf(id), 1)
      window.removeEventListener('keydown', onKey)
    }
  }, [])
}
