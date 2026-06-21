import { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'

export type ContextMenuItem = {
  label: string
  danger?: boolean
  disabled?: boolean
  onSelect: () => void | Promise<void>
}

function portalTarget() {
  if (typeof document === 'undefined') return null
  return document.querySelector('.app-shell') ?? document.body
}

export function ContextMenu({ x, y, items, onClose }: {
  x: number
  y: number
  items: ContextMenuItem[]
  onClose: () => void
}) {
  const menu = useRef<HTMLDivElement>(null)
  const target = portalTarget()

  useEffect(() => {
    const closeOnPointerDown = (event: PointerEvent) => {
      const targetNode = event.target as Node | null
      if (menu.current?.contains(targetNode)) return
      onClose()
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('pointerdown', closeOnPointerDown, true)
    window.addEventListener('keydown', closeOnEscape)
    window.addEventListener('resize', onClose)
    window.addEventListener('scroll', onClose, true)
    return () => {
      window.removeEventListener('pointerdown', closeOnPointerDown, true)
      window.removeEventListener('keydown', closeOnEscape)
      window.removeEventListener('resize', onClose)
      window.removeEventListener('scroll', onClose, true)
    }
  }, [onClose])

  if (!target || typeof window === 'undefined' || items.length === 0) return null
  const estimatedWidth = 180
  const estimatedHeight = items.length * 34 + 14
  const left = Math.max(12, Math.min(x, window.innerWidth - estimatedWidth - 12))
  const top = Math.max(12, Math.min(y, window.innerHeight - estimatedHeight - 12))

  return createPortal(
    <div
      className="context-menu"
      ref={menu}
      style={{ left, top }}
      onContextMenu={event => event.preventDefault()}
    >
      {items.map((item, index) => (
        <button
          key={`${item.label}-${index}`}
          type="button"
          className={item.danger ? 'danger' : ''}
          disabled={item.disabled}
          onClick={() => {
            onClose()
            void item.onSelect()
          }}
        >
          {item.label}
        </button>
      ))}
    </div>,
    target,
  )
}
