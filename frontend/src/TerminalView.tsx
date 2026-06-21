import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { CanvasAddon } from '@xterm/addon-canvas'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { WebglAddon } from '@xterm/addon-webgl'
import { ClipboardCopy, ClipboardPaste, TextSelect } from 'lucide-react'
import '@xterm/xterm/css/xterm.css'
import { api } from './bridge'
import { ZmodemAdapter } from './zmodem'
import { clampTerminalMenuPosition, getTerminalClipboardAction } from './terminalClipboard'
import { resolveTerminalThemeColors, terminalChromeVariables, terminalXtermTheme } from './terminalThemes'
import type { Connection, Settings } from './types'
import type { CommandHistory } from './types'

type Props = {
  connection: Connection
  settings: Settings
  active: boolean
  privateSession: boolean
  credentialOverride?: {
    name?: string
    type?: Connection['authentication']
    password?: string
    privateKeyPem?: string
    passphrase?: string
  }
  onReady: (sessionId: string) => void
  onHostKey: (pending: NonNullable<Awaited<ReturnType<typeof api.StartSSH>>['hostKey']>) => void
  onAuthPrompt: (prompt: NonNullable<Awaited<ReturnType<typeof api.StartSSH>>['authPrompt']>) => void
  onClose: () => void
}

export function TerminalView({
  connection, settings, active, privateSession, credentialOverride, onReady, onHostKey, onAuthPrompt, onClose
}: Props) {
  const host = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const sessionIdRef = useRef('')
  const [status, setStatus] = useState('Connecting...')
  const [zmodemActive, setZmodemActive] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [selectionText, setSelectionText] = useState('')
  const [terminalMenu, setTerminalMenu] = useState<{ left: number; top: number } | null>(null)
  const [terminalMenuReady, setTerminalMenuReady] = useState(false)
  const [suggestions, setSuggestions] = useState<CommandHistory[]>([])
  const suggestionsRef = useRef<CommandHistory[]>([])
  const lineRef = useRef('')
  const socketRef = useRef<WebSocket | undefined>(undefined)
  const zmodemRef = useRef<ZmodemAdapter | undefined>(undefined)
  const searchRef = useRef<SearchAddon | undefined>(undefined)
  const applySuggestionRef = useRef<(command: string) => void>(() => undefined)
  const colors = resolveTerminalThemeColors(settings)

  useEffect(() => { suggestionsRef.current = suggestions }, [suggestions])
  useEffect(() => {
    if (!active) setTerminalMenu(null)
  }, [active])
  useEffect(() => {
    setTerminalMenuReady(false)
  }, [terminalMenu])

  const closeTerminalMenu = () => setTerminalMenu(null)
  const syncSelection = () => {
    const terminal = terminalRef.current
    if (!terminal) return
    setSelectionText(terminal.hasSelection() ? terminal.getSelection() : '')
  }
  const copySelection = async () => {
    const terminal = terminalRef.current
    if (!terminal) return
    const text = terminal.getSelection()
    if (!text) return
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // Clipboard access can be blocked by the host.
    }
  }
  const pasteClipboard = async () => {
    const terminal = terminalRef.current
    if (!terminal) return
    terminal.focus()
    try {
      const text = await navigator.clipboard.readText()
      if (text) terminal.paste(text)
    } catch {
      // Ignore clipboard read failures and keep the terminal usable.
    }
  }
  const selectAllInTerminal = () => {
    const terminal = terminalRef.current
    if (!terminal) return
    terminal.focus()
    terminal.selectAll()
    syncSelection()
  }

  useEffect(() => {
    if (!host.current) return
    const terminal = new Terminal({
      cursorBlink: true,
      allowProposedApi: false,
      convertEol: false,
      fontFamily: settings.fontFamily,
      fontSize: settings.fontSize,
      scrollback: 10000,
      theme: terminalXtermTheme(colors)
    })
    terminalRef.current = terminal
    const fit = new FitAddon()
    fitRef.current = fit
    terminal.loadAddon(fit)
    const search = new SearchAddon()
    searchRef.current = search
    terminal.loadAddon(search)
    terminal.loadAddon(new WebLinksAddon((_, uri) => {
      try {
        const parsed = new URL(uri)
        if (parsed.protocol === 'https:' || parsed.protocol === 'http:') {
          window.runtime?.BrowserOpenURL?.(parsed.toString())
        }
      } catch {
        // Ignore malformed or unsafe terminal links.
      }
    }))
    terminal.open(host.current)
    try {
      terminal.loadAddon(new WebglAddon())
    } catch {
      try {
        terminal.loadAddon(new CanvasAddon())
      } catch {
        // xterm automatically keeps its DOM renderer when Canvas is unavailable.
      }
    }
    fit.fit()

    let socket: WebSocket | undefined
    let sessionId = ''
    let line = ''
    let lineReliable = true
    let suggestionTimer = 0
    let suggestionRequest = 0
    let disposed = false
    const outputDecoder = connection.encoding && connection.encoding.toLowerCase() !== 'utf-8'
      ? new TextDecoder(connection.encoding)
      : undefined
    const clearSuggestions = () => {
      suggestionRequest++
      window.clearTimeout(suggestionTimer)
      suggestionTimer = 0
      setSuggestions(current => current.length ? [] : current)
    }
    const scheduleSuggestions = (prefix: string) => {
      window.clearTimeout(suggestionTimer)
      if (!lineReliable || prefix.trim().length < 2) {
        clearSuggestions()
        return
      }
      const request = ++suggestionRequest
      suggestionTimer = window.setTimeout(() => {
        void api.SuggestCommands(connection.id, prefix)
          .then(values => {
            if (disposed || request !== suggestionRequest || lineRef.current !== prefix) return
            setSuggestions(values.slice(0, 5))
          })
          .catch(() => {
            if (!disposed && request === suggestionRequest) clearSuggestions()
          })
      }, 180)
    }

    const connect = async () => {
      const result = await api.StartSSH({
        connectionId: connection.id,
        columns: terminal.cols,
        rows: terminal.rows,
        interactionResponses: [],
        credentialOverride
      })
      if (disposed) return
      if (result.hostKey) {
        setStatus(result.hostKey.changed ? 'Host key changed' : 'Host key confirmed')
        onHostKey(result.hostKey)
        return
      }
      if (result.authPrompt) {
        setStatus(result.authPrompt.message)
        onAuthPrompt(result.authPrompt)
        return
      }
      if (!result.session) throw new Error('SSH session was not created')
      sessionId = result.session.sessionId
      sessionIdRef.current = sessionId
      onReady(sessionId)
      socket = new WebSocket(result.session.url)
      socketRef.current = socket
      socket.binaryType = 'arraybuffer'
      const zmodem = new ZmodemAdapter({
        toTerminal: data => terminal.write(outputDecoder
          ? outputDecoder.decode(data, { stream: true })
          : data),
        send: data => socket?.send(data),
        onStatus: setStatus,
        onActive: setZmodemActive
      })
      zmodemRef.current = zmodem
      socket.onopen = () => setStatus('Connected')
      socket.onmessage = event => zmodem.consume(new Uint8Array(event.data as ArrayBuffer))
      socket.onerror = () => setStatus('Connection error')
      socket.onclose = () => {
        setStatus('Connection closed')
        terminal.write('\r\n\x1b[38;5;244m[Connection closed]\x1b[0m\r\n')
      }
      applySuggestionRef.current = command => {
        const suffix = command.slice(line.length)
        if (suffix) socket?.send(suffix)
        line = command
        lineRef.current = command
        clearSuggestions()
      }
      terminal.onData(data => {
        socket?.send(data)
        for (const char of data) {
          if (char === '\r') {
            if (lineReliable && line.trim()) {
              void api.AddCommandHistory(connection.id, line, privateSession)
            }
            line = ''
            lineReliable = true
            lineRef.current = ''
          } else if (char === '\x7f') {
            line = line.slice(0, -1)
            lineRef.current = line
          } else if (char === '\t') {
            // Tab completion changes the remote shell buffer in a way the
            // terminal cannot reconstruct reliably.
            lineReliable = false
          } else if (char < ' ' || char === '\x1b') {
            lineReliable = false
          } else if (char >= ' ') {
            line += char
            lineRef.current = line
          }
        }
        if (lineReliable) scheduleSuggestions(line)
        else clearSuggestions()
      })
      terminal.attachCustomKeyEventHandler(event => {
        if (event.type !== 'keydown') return true
        const clipboardAction = getTerminalClipboardAction(event)
        if (clipboardAction === 'copy') {
          event.preventDefault()
          void copySelection()
          return false
        }
        if (clipboardAction === 'paste') {
          event.preventDefault()
          void pasteClipboard()
          return false
        }
        if (!suggestionsRef.current.length) return true
        let index = -1
        if (event.key === 'Tab') {
          index = 0
        } else if (event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey) {
          const digit = Number(event.key)
          if (digit >= 2 && digit <= 5) index = digit - 1
        }
        if (index < 0 || !suggestionsRef.current[index]) return true
        const command = suggestionsRef.current[index].command
        applySuggestionRef.current(command)
        event.preventDefault()
        return false
      })
      terminal.onSelectionChange(syncSelection)
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
      terminalRef.current = null
      socketRef.current = undefined
      zmodemRef.current = undefined
      searchRef.current = undefined
      fitRef.current = null
      sessionIdRef.current = ''
      applySuggestionRef.current = () => undefined
      window.clearTimeout(suggestionTimer)
      if (sessionId) void api.CloseSSH(sessionId)
      terminal.dispose()
      onClose()
    }
  }, [
    connection.id,
    credentialOverride?.password,
    credentialOverride?.privateKeyPem,
    credentialOverride?.passphrase,
  ])

  useEffect(() => {
    const terminal = terminalRef.current
    if (!terminal) return
    terminal.options.fontFamily = settings.fontFamily
    terminal.options.fontSize = settings.fontSize
    terminal.options.theme = terminalXtermTheme(colors)
    fitRef.current?.fit()
    if (sessionIdRef.current) void api.ResizeSSH(sessionIdRef.current, terminal.cols, terminal.rows)
  }, [
    colors,
    settings.fontFamily,
    settings.fontSize,
  ])

  useEffect(() => {
    if (active) window.setTimeout(() => window.dispatchEvent(new Event('resize')), 0)
  }, [active])

  useEffect(() => {
    if (!active) return
    const shortcut = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setSearchOpen(false)
        closeTerminalMenu()
      } else if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'f') {
        event.preventDefault()
        setSearchOpen(true)
      }
    }
    window.addEventListener('keydown', shortcut)
    return () => window.removeEventListener('keydown', shortcut)
  }, [active])

  return (
    <section
      className={`terminal-pane ${active ? 'active' : ''}`}
      style={terminalChromeVariables(colors) as React.CSSProperties}>
      <div ref={host} className="terminal-host" onContextMenu={event => {
        event.preventDefault()
        setTerminalMenuReady(false)
        const { left, top } = clampTerminalMenuPosition(
          event.clientX, event.clientY, window.innerWidth, window.innerHeight
        )
        syncSelection()
        setTerminalMenu({ left, top })
      }} />
      {terminalMenu && <div className="terminal-menu-backdrop" onMouseDown={closeTerminalMenu}>
        <div className="terminal-menu" data-ready={terminalMenuReady ? 'true' : 'false'}
          ref={node => {
            if (!node) return
            const rect = node.getBoundingClientRect()
            const { left, top } = clampTerminalMenuPosition(
              terminalMenu.left,
              terminalMenu.top,
              window.innerWidth,
              window.innerHeight,
              rect.width,
              rect.height
            )
            if (left !== terminalMenu.left || top !== terminalMenu.top) {
              setTerminalMenu({ left, top })
              return
            }
            if (!terminalMenuReady) window.requestAnimationFrame(() => setTerminalMenuReady(true))
          }}
          style={{ left: terminalMenu.left, top: terminalMenu.top, visibility: terminalMenuReady ? 'visible' : 'hidden' }}
          onMouseDown={event => event.stopPropagation()}>
          <button type="button" disabled={!selectionText} onClick={() => {
            closeTerminalMenu()
            void copySelection()
          }}>
            <ClipboardCopy size={14} />
            <span>复制</span>
          </button>
          <button type="button" onClick={() => {
            closeTerminalMenu()
            void pasteClipboard()
          }}>
            <ClipboardPaste size={14} />
            <span>粘贴</span>
          </button>
          <button type="button" onClick={() => {
            closeTerminalMenu()
            selectAllInTerminal()
          }}>
            <TextSelect size={14} />
            <span>全选</span>
          </button>
        </div>
      </div>}
      {searchOpen && <div className="terminal-search">
        <input autoFocus value={searchQuery} placeholder="Search"
          onChange={event => {
            setSearchQuery(event.target.value)
            searchRef.current?.findNext(event.target.value, { incremental: true })
          }}
          onKeyDown={event => {
            if (event.key === 'Enter') {
              if (event.shiftKey) searchRef.current?.findPrevious(searchQuery)
              else searchRef.current?.findNext(searchQuery)
            }
          }} />
        <button onClick={() => searchRef.current?.findPrevious(searchQuery)}>Prev</button>
        <button onClick={() => searchRef.current?.findNext(searchQuery)}>Next</button>
        <button onClick={() => setSearchOpen(false)}>Close</button>
      </div>}
      {!!suggestions.length && <div className="command-suggestions">
        {suggestions.map((suggestion, index) => <button key={suggestion.id}
          onMouseDown={event => {
            event.preventDefault()
            applySuggestionRef.current(suggestion.command)
          }}>
          <kbd>{index === 0 ? 'Tab' : `Alt+${index + 1}`}</kbd><span>{suggestion.command}</span>
          <small>{suggestion.useCount}x</small>
        </button>)}
      </div>}
      <div className="terminal-status">
        {status}
        {privateSession ? ' · private session' : ''}
        {zmodemActive && <button onClick={() => void zmodemRef.current?.cancel()}>Cancel ZMODEM</button>}
      </div>
    </section>
  )
}
