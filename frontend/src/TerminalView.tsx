import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { WebglAddon } from '@xterm/addon-webgl'
import '@xterm/xterm/css/xterm.css'
import { api } from './bridge'
import { ZmodemAdapter } from './zmodem'
import type { Connection, Settings } from './types'
import type { CommandHistory } from './types'

type Props = {
  connection: Connection
  settings: Settings
  active: boolean
  privateSession: boolean
  onReady: (sessionId: string) => void
  onHostKey: (pending: NonNullable<Awaited<ReturnType<typeof api.StartSSH>>['hostKey']>) => void
  onClose: () => void
}

export function TerminalView({
  connection, settings, active, privateSession, onReady, onHostKey, onClose
}: Props) {
  const host = useRef<HTMLDivElement>(null)
  const [status, setStatus] = useState('正在连接…')
  const [suggestions, setSuggestions] = useState<CommandHistory[]>([])
  const suggestionsRef = useRef<CommandHistory[]>([])
  const lineRef = useRef('')
  const socketRef = useRef<WebSocket | undefined>(undefined)
  const applySuggestionRef = useRef<(command: string) => void>(() => undefined)

  useEffect(() => { suggestionsRef.current = suggestions }, [suggestions])

  useEffect(() => {
    if (!host.current) return
    const terminal = new Terminal({
      cursorBlink: true,
      allowProposedApi: false,
      convertEol: false,
      fontFamily: settings.fontFamily,
      fontSize: settings.fontSize,
      scrollback: 10000,
      theme: {
        background: '#0a0e16',
        foreground: '#dce3ee',
        cursor: '#77e4d4',
        selectionBackground: '#325a6a88',
        black: '#111827',
        brightBlack: '#64748b',
        green: '#70d6a1',
        cyan: '#72d7e6',
        brightCyan: '#9aeaf2'
      }
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.loadAddon(new SearchAddon())
    terminal.loadAddon(new WebLinksAddon())
    terminal.open(host.current)
    try {
      terminal.loadAddon(new WebglAddon())
    } catch {
      // xterm automatically keeps its DOM renderer when WebGL is unavailable.
    }
    fit.fit()

    let socket: WebSocket | undefined
    let sessionId = ''
    let line = ''
    let suggestionTimer = 0
    let disposed = false

    const connect = async () => {
      const result = await api.StartSSH({
        connectionId: connection.id,
        columns: terminal.cols,
        rows: terminal.rows,
        interactionResponses: []
      })
      if (disposed) return
      if (result.hostKey) {
        setStatus(result.hostKey.changed ? '主机密钥已变化' : '等待确认主机密钥')
        onHostKey(result.hostKey)
        return
      }
      if (!result.session) throw new Error('SSH session was not created')
      sessionId = result.session.sessionId
      onReady(sessionId)
      socket = new WebSocket(result.session.url)
      socketRef.current = socket
      socket.binaryType = 'arraybuffer'
      const zmodem = new ZmodemAdapter({
        toTerminal: data => terminal.write(data),
        send: data => socket?.send(data),
        onStatus: setStatus
      })
      socket.onopen = () => setStatus('已连接')
      socket.onmessage = event => zmodem.consume(new Uint8Array(event.data as ArrayBuffer))
      socket.onerror = () => setStatus('连接发生错误')
      socket.onclose = () => {
        setStatus('连接已关闭')
        terminal.write('\r\n\x1b[38;5;244m[连接已关闭]\x1b[0m\r\n')
      }
      applySuggestionRef.current = command => {
        const suffix = command.slice(line.length)
        if (suffix) socket?.send(new TextEncoder().encode(suffix))
        line = command
        lineRef.current = command
        setSuggestions([])
      }
      terminal.onData(data => {
        socket?.send(new TextEncoder().encode(data))
        for (const char of data) {
          if (char === '\r') {
            if (line.trim()) void api.AddCommandHistory(connection.id, line, privateSession)
            line = ''
            lineRef.current = ''
            setSuggestions([])
          } else if (char === '\x7f') {
            line = line.slice(0, -1)
            lineRef.current = line
          } else if (char >= ' ') {
            line += char
            lineRef.current = line
          }
        }
        window.clearTimeout(suggestionTimer)
        if (line.trim().length >= 2) {
          suggestionTimer = window.setTimeout(() => {
            void api.SuggestCommands(connection.id, line).then(values => setSuggestions(values.slice(0, 5)))
          }, 90)
        } else {
          setSuggestions([])
        }
      })
      terminal.attachCustomKeyEventHandler(event => {
        if (event.type !== 'keydown' || event.key !== 'Tab' || !suggestionsRef.current.length) return true
        const command = suggestionsRef.current[0].command
        applySuggestionRef.current(command)
        event.preventDefault()
        return false
      })
      terminal.onResize(size => {
        if (sessionId) void api.ResizeSSH(sessionId, size.cols, size.rows)
      })
    }

    connect().catch(error => {
      setStatus(String(error))
      terminal.write(`\r\n\x1b[31m${String(error)}\x1b[0m\r\n`)
    })
    const observer = new ResizeObserver(() => {
      if (active) fit.fit()
    })
    observer.observe(host.current)
    return () => {
      disposed = true
      observer.disconnect()
      socket?.close()
      socketRef.current = undefined
      applySuggestionRef.current = () => undefined
      window.clearTimeout(suggestionTimer)
      if (sessionId) void api.CloseSSH(sessionId)
      terminal.dispose()
      onClose()
    }
  }, [connection.id])

  useEffect(() => {
    if (active) window.setTimeout(() => window.dispatchEvent(new Event('resize')), 0)
  }, [active])

  return (
    <section className={`terminal-pane ${active ? 'active' : ''}`}>
      <div ref={host} className="terminal-host" />
      {!!suggestions.length && <div className="command-suggestions">
        {suggestions.map((suggestion, index) => <button key={suggestion.id}
          onMouseDown={event => {
            event.preventDefault()
            applySuggestionRef.current(suggestion.command)
          }}>
          <kbd>{index === 0 ? 'Tab' : index + 1}</kbd><span>{suggestion.command}</span>
          <small>{suggestion.useCount} 次</small>
        </button>)}
      </div>}
      <div className="terminal-status">{status}{privateSession ? ' · 隐私会话（不记录命令）' : ''}</div>
    </section>
  )
}
