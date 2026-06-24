import { describe, expect, it } from 'vitest'
import { TerminalEchoGuard } from './terminalEchoGuard'

describe('TerminalEchoGuard', () => {
  it('allows suggestions after visible echo confirms the current line', () => {
    const guard = new TerminalEchoGuard()

    guard.appendInput('l')
    expect(guard.canSuggest()).toBe(false)
    expect(guard.observeOutput('l')).toEqual([])
    expect(guard.canSuggest()).toBe(true)
  })

  it('confirms normal command history when echo arrives after enter', () => {
    const guard = new TerminalEchoGuard()

    guard.appendInput('ls -la')
    expect(guard.submit()).toBeUndefined()

    expect(guard.observeOutput('ls -la\r\n')).toEqual(['ls -la'])
  })

  it('waits for the remote newline after a fully echoed Chinese command', () => {
    const guard = new TerminalEchoGuard()

    guard.appendInput('查看日志')
    expect(guard.observeOutput('查看日志')).toEqual([])
    expect(guard.submit()).toBeUndefined()
    expect(guard.observeOutput('\r\n')).toEqual(['查看日志'])
  })

  it('can confirm a command even when the last echo arrives after enter', () => {
    const guard = new TerminalEchoGuard()

    guard.appendInput('git sta')
    expect(guard.observeOutput('git ')).toEqual([])
    expect(guard.submit()).toBeUndefined()

    expect(guard.observeOutput('sta\r\n')).toEqual(['git sta'])
  })

  it('does not confirm hidden input that never echoes back', () => {
    const guard = new TerminalEchoGuard()

    guard.appendInput('s3cr3t!')
    expect(guard.canSuggest()).toBe(false)
    expect(guard.submit()).toBeUndefined()

    expect(guard.observeOutput('\r\n欢迎回来\r\n')).toEqual([])
  })

  it('does not use later script output to rescue a hidden command', () => {
    const guard = new TerminalEchoGuard()

    guard.appendInput('s3cr3t!')
    expect(guard.submit()).toBeUndefined()

    expect(guard.observeOutput('\r\ns3cr3t!\r\n')).toEqual([])
  })

  it('does not confirm a command when visible output interrupts the echo before newline', () => {
    const guard = new TerminalEchoGuard()

    guard.appendInput('echo hi')
    expect(guard.observeOutput('echo hi')).toEqual([])
    expect(guard.submit()).toBeUndefined()

    expect(guard.observeOutput('done\r\n')).toEqual([])
  })

  it('ignores ANSI control sequences inside echoed input', () => {
    const guard = new TerminalEchoGuard()

    guard.appendInput('sudo -v')
    expect(guard.submit()).toBeUndefined()

    expect(guard.observeOutput('sudo \x1b[32m-v\x1b[0m\r\n')).toEqual(['sudo -v'])
  })

  it('drops unreliable lines after tab completion or cursor control', () => {
    const guard = new TerminalEchoGuard()

    guard.appendInput('git sta')
    guard.markUnreliable()
    expect(guard.submit()).toBeUndefined()
    expect(guard.observeOutput('git status\r\n')).toEqual([])
  })
})
