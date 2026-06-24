type ShortcutEvent = Pick<KeyboardEvent, 'altKey' | 'ctrlKey' | 'metaKey' | 'shiftKey' | 'key' | 'code'>

export function isSuggestionDismissKey(event: ShortcutEvent) {
  return event.key === 'Escape'
}

export function isSuggestionDeleteKey(event: ShortcutEvent) {
  return event.key === 'Delete' && !event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey
}

export function directSuggestionShortcutIndex(event: ShortcutEvent, count: number) {
  if (count < 1 || !event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) {
    return -1
  }
  const digit = eventDigit(event)
  if (digit < 1) return -1
  const index = digit - 1
  return index < count ? index : -1
}

export function nextSuggestionIndex(current: number, count: number, direction: -1 | 1) {
  if (count < 1) return 0
  return (current + direction + count) % count
}

function eventDigit(event: ShortcutEvent) {
  if (/^Digit\d$/.test(event.code)) {
    return Number(event.code.slice('Digit'.length))
  }
  if (/^Numpad\d$/.test(event.code)) {
    return Number(event.code.slice('Numpad'.length))
  }
  if (/^\d$/.test(event.key)) {
    return Number(event.key)
  }
  return -1
}
