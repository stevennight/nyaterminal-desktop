import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'

export function useVerticalSplit({
  storageKey,
  initialHeight,
  minHeight,
}: {
  storageKey: string
  initialHeight: number
  minHeight: number
}) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const cleanupRef = useRef<(() => void) | null>(null)
  const [height, setHeight] = useState(() => readStoredHeight(storageKey, initialHeight, minHeight))

  useEffect(() => {
    try {
      window.localStorage.setItem(storageKey, String(height))
    } catch {
      // Ignore storage failures.
    }
  }, [height, storageKey])

  useEffect(() => () => {
    cleanupRef.current?.()
    cleanupRef.current = null
  }, [])

  const beginResize = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    if (event.button !== 0) return
    event.preventDefault()
    cleanupRef.current?.()
    const pointerId = event.pointerId
    const container = containerRef.current
    if (!container) return

    const update = (clientY: number) => {
      const bounds = container.getBoundingClientRect()
      setHeight(Math.max(minHeight, Math.round(bounds.bottom - clientY)))
    }

    const move = (pointerEvent: PointerEvent) => {
      if (pointerEvent.pointerId !== pointerId) return
      update(pointerEvent.clientY)
    }

    const stop = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', stop)
      window.removeEventListener('pointercancel', stop)
      window.removeEventListener('blur', stop)
      if (cleanupRef.current === stop) cleanupRef.current = null
    }

    cleanupRef.current = stop
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop)
    window.addEventListener('pointercancel', stop)
    window.addEventListener('blur', stop)
    update(event.clientY)
  }, [minHeight])

  return { containerRef, height, beginResize }
}

function readStoredHeight(key: string, fallback: number, minHeight: number) {
  if (typeof window === 'undefined') return Math.max(minHeight, fallback)
  try {
    const stored = window.localStorage.getItem(key)
    if (stored !== null) {
      const parsed = Number(stored)
      if (Number.isFinite(parsed)) return Math.max(minHeight, Math.round(parsed))
    }
  } catch {
    // Ignore storage failures.
  }
  return Math.max(minHeight, fallback)
}
