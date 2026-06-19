import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ZmodemAdapter, ZmodemHeaderDetector } from './zmodem'

const encoder = new TextEncoder()

describe('ZmodemHeaderDetector', () => {
  it('detects a receive header split across websocket frames', () => {
    const detector = new ZmodemHeaderDetector()
    const first = detector.consume(encoder.encode('prompt\r\n**\x18'))
    expect(first.mode).toBeUndefined()
    const second = detector.consume(encoder.encode('B00payload'))
    expect(new TextDecoder().decode(concat(first.terminal, second.terminal))).toBe('prompt\r\n')
    expect(second.mode).toBe('receive')
    expect(Array.from(second.protocol ?? []).slice(0, 6))
      .toEqual(Array.from(encoder.encode('**\x18B00')))
  })

  it('passes ordinary terminal data immediately', () => {
    const detector = new ZmodemHeaderDetector()
    const result = detector.consume(encoder.encode('ordinary terminal output'))
    expect(new TextDecoder().decode(result.terminal)).toBe('ordinary terminal output')
    expect(result.mode).toBeUndefined()
  })

  it('does not hold back normal chunks that do not match the header prefix', () => {
    const detector = new ZmodemHeaderDetector()
    const first = detector.consume(encoder.encode('ord'))
    const second = detector.consume(encoder.encode('inary'))
    expect(new TextDecoder().decode(concat(first.terminal, second.terminal))).toBe('ordinary')
    expect(first.mode).toBeUndefined()
    expect(second.mode).toBeUndefined()
  })

  it('detects the send direction', () => {
    const detector = new ZmodemHeaderDetector()
    const result = detector.consume(encoder.encode('**\x18B01'))
    expect(result.mode).toBe('send')
  })
})

describe('ZmodemAdapter', () => {
  beforeEach(() => {
    installFakeDom()
  })

  it('cancels a pending send session when file selection is canceled', async () => {
    const send = vi.fn()
    const toTerminal = vi.fn()
    const onStatus = vi.fn()
    const onActive = vi.fn()
    const adapter = new ZmodemAdapter({ send, toTerminal, onStatus, onActive })

    adapter.consume(encoder.encode('**\x18B01payload'))
    const input = getCreatedInput()
    input.oncancel?.(new Event('cancel'))
    await Promise.resolve()

    expect(send).toHaveBeenCalledTimes(1)
    expect(Array.from(send.mock.calls[0][0] as Uint8Array)).toEqual(new Array(8).fill(0x18))
    expect(onStatus).toHaveBeenLastCalledWith('ZMODEM 传输已取消')
    expect(onActive).toHaveBeenLastCalledWith(false)
    expect(toTerminal).not.toHaveBeenCalled()
  })
})

function concat(left: Uint8Array, right: Uint8Array) {
  const value = new Uint8Array(left.length + right.length)
  value.set(left)
  value.set(right, left.length)
  return value
}

type FakeInput = {
  type: string
  multiple: boolean
  hidden: boolean
  files?: FileList | null
  onchange?: ((event: Event) => void) | null
  oncancel?: ((event: Event) => void) | null
  click: () => void
  remove: () => void
}

let createdInput: FakeInput

function installFakeDom() {
  createdInput = {
    type: '',
    multiple: false,
    hidden: false,
    files: null,
    onchange: null,
    oncancel: null,
    click: () => undefined,
    remove: () => undefined
  }
  Object.assign(globalThis, {
    document: {
      body: { appendChild: vi.fn() },
      createElement: vi.fn(() => createdInput)
    },
    window: {
      addEventListener: vi.fn(),
      removeEventListener: vi.fn()
    }
  })
}

function getCreatedInput() {
  return createdInput
}
