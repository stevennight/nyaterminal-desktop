import { describe, expect, it } from 'vitest'
import { ZmodemHeaderDetector } from './zmodem'

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

function concat(left: Uint8Array, right: Uint8Array) {
  const value = new Uint8Array(left.length + right.length)
  value.set(left)
  value.set(right, left.length)
  return value
}
