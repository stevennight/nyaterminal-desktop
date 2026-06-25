import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { CanvasAddon } from '@xterm/addon-canvas'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { WebglAddon } from '@xterm/addon-webgl'
import { ClipboardCopy, ClipboardPaste, TextSelect, Trash2 } from 'lucide-react'
import '@xterm/xterm/css/xterm.css'
import { api } from './bridge'
import { ZmodemAdapter } from './zmodem'
import { chunkTerminalInput, clampTerminalMenuPosition, getTerminalClipboardAction } from './terminalClipboard'
import { TerminalEchoGuard } from './terminalEchoGuard'
import {
  directSuggestionShortcutIndex,
  isSuggestionDeleteKey,
  isSuggestionDismissKey,
  nextSuggestionIndex,
} from './terminalSuggestions'
import { resolveTerminalThemeColors, terminalChromeVariables, terminalXtermTheme } from './terminalThemes'
import type { Connection, Settings } from './types'
import type { CommandHistory } from './types'

type Props = {
  connection: Connection
  settings: Settings
  active: boolean
  privateSession: boolean
  reconnectMessage?: string
  credentialOverride?: {
    name?: string
    type?: Connection['authentication']
    password?: string
    privateKeyPem?: string
    passphrase?: string
  }
  onReady: (sessionId: string) => void
  onRetryableDisconnect: (reason: { message: string; retryable: boolean }) => void
  onHostKey: (pending: NonNullable<Awaited<ReturnType<typeof api.StartSSH>>['hostKey']>) => void
  onAuthPrompt: (prompt: NonNullable<Awaited<ReturnType<typeof api.StartSSH>>['authPrompt']>) => void
  onActivity?: () => void
  onZmodemActiveChange?: (active: boolean) => void
  onClose: () => void
}

export function TerminalView({
  connection, settings, active, privateSession, reconnectMessage, credentialOverride, onReady, onRetryableDisconnect, onHostKey, onAuthPrompt, onActivity, onZmodemActiveChange, onClose
}: Props) {
  const paneRef = useRef<HTMLElement>(null)
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
  const [suggestionIndex, setSuggestionIndex] = useState(0)
  const [suggestionPosition, setSuggestionPosition] = useState<{ left: number; top: number; width: number } | null>(null)
  const suggestionsRef = useRef<CommandHistory[]>([])
  const suggestionIndexRef = useRef(0)
  const lineRef = useRef('')
  const socketRef = useRef<WebSocket | undefined>(undefined)
  const zmodemRef = useRef<ZmodemAdapter | undefined>(undefined)
  const searchRef = useRef<SearchAddon | undefined>(undefined)
  const applySuggestionRef = useRef<(command: string) => void>(() => undefined)
  const deleteSuggestionRef = useRef<(index: number) => void>(() => undefined)
  const zmodemActiveRef = useRef(false)
  const colors = resolveTerminalThemeColors(settings)
  const onReadyRef = useRef(onReady)
  const onRetryableDisconnectRef = useRef(onRetryableDisconnect)
  const onHostKeyRef = useRef(onHostKey)
  const onAuthPromptRef = useRef(onAuthPrompt)
  const onActivityRef = useRef(onActivity)
  const onZmodemActiveChangeRef = useRef(onZmodemActiveChange)
  const onCloseRef = useRef(onClose)

  onReadyRef.current = onReady
  onRetryableDisconnectRef.current = onRetryableDisconnect
  onHostKeyRef.current = onHostKey
  onAuthPromptRef.current = onAuthPrompt
  onActivityRef.current = onActivity
  onZmodemActiveChangeRef.current = onZmodemActiveChange
  onCloseRef.current = onClose

  useEffect(() => {
    suggestionsRef.current = suggestions
    if (!suggestions.length) {
      suggestionIndexRef.current = 0
      setSuggestionIndex(0)
      setSuggestionPosition(null)
    } else if (suggestionIndexRef.current >= suggestions.length) {
      suggestionIndexRef.current = 0
      setSuggestionIndex(0)
    }
  }, [suggestions])
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
    if (!reconnectMessage) return
    setStatus(reconnectMessage)
  }, [reconnectMessage])

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
    let suggestionTimer = 0
    let suggestionRequest = 0
    let disposed = false
    const hiddenSuggestionCommands = new Set<string>()
    const echoGuard = new TerminalEchoGuard()
    const outputDecoder = connection.encoding && connection.encoding.toLowerCase() !== 'utf-8'
      ? new TextDecoder(connection.encoding)
      : undefined
    const terminalDecoder = outputDecoder ?? new TextDecoder()
    const addCommandHistory = (command: string) => {
      void api.AddCommandHistory(connection.id, command, privateSession)
    }
    const updateSuggestionPosition = (count = suggestionsRef.current.length) => {
      const pane = paneRef.current
      const hostElement = host.current
      const terminalElement = terminal.element
      if (!pane || !hostElement || !terminalElement || count < 1) {
        setSuggestionPosition(null)
        return
      }
      const paneRect = pane.getBoundingClientRect()
      const screen = terminalElement.querySelector('.xterm-screen') as HTMLElement | null
      const screenRect = (screen ?? hostElement).getBoundingClientRect()
      if (screenRect.width <= 0 || screenRect.height <= 0 || paneRect.width <= 0 || paneRect.height <= 0) {
        return
      }
      const width = suggestionPopupWidth(paneRect.width, suggestionsRef.current)
      const height = Math.min(count, 5) * 30 + 10
      const cellWidth = screenRect.width / Math.max(terminal.cols, 1)
      const cellHeight = screenRect.height / Math.max(terminal.rows, 1)
      const cursorX = terminal.buffer.active.cursorX
      const cursorY = terminal.buffer.active.cursorY
      const rawLeft = screenRect.left - paneRect.left + cursorX * cellWidth
      const belowTop = screenRect.top - paneRect.top + (cursorY + 1) * cellHeight + 4
      const aboveTop = screenRect.top - paneRect.top + cursorY * cellHeight - height - 4
      const maxBottom = paneRect.height - 28
      const top = belowTop + height <= maxBottom ? belowTop : Math.max(6, aboveTop)
      const maxLeft = Math.max(6, paneRect.width - width - 6)
      const left = Math.max(6, Math.min(rawLeft, maxLeft))
      setSuggestionPosition(current => {
        if (current && current.left === left && current.top === top && current.width === width) {
          return current
        }
        return { left, top, width }
      })
    }
    const setActiveSuggestion = (index: number, count = suggestionsRef.current.length) => {
      const next = count < 1 ? 0 : ((index % count) + count) % count
      suggestionIndexRef.current = next
      setSuggestionIndex(next)
    }
    const clearSuggestions = () => {
      suggestionRequest++
      window.clearTimeout(suggestionTimer)
      suggestionTimer = 0
      suggestionsRef.current = []
      setActiveSuggestion(0, 0)
      setSuggestionPosition(null)
      setSuggestions(current => current.length ? [] : current)
    }
    const deleteSuggestion = (index: number) => {
      const target = suggestionsRef.current[index]
      if (!target) return
      hiddenSuggestionCommands.add(target.command)
      suggestionRequest++
      window.clearTimeout(suggestionTimer)
      suggestionTimer = 0
      const next = suggestionsRef.current.filter(suggestion => suggestion.command !== target.command)
      suggestionsRef.current = next
      setSuggestions(next)
      setActiveSuggestion(Math.min(index, Math.max(next.length - 1, 0)), next.length)
      if (next.length) updateSuggestionPosition(next.length)
      else setSuggestionPosition(null)
      void api.DeleteCommandHistory(connection.id, target.command).catch(error => {
        hiddenSuggestionCommands.delete(target.command)
        const message = error instanceof Error ? error.message : String(error)
        setStatus(`Delete history failed: ${message}`)
      })
    }
    const scheduleSuggestions = (prefix: string) => {
      window.clearTimeout(suggestionTimer)
      if (!echoGuard.canSuggest() || prefix.trim().length < 2) {
        clearSuggestions()
        return
      }
      const request = ++suggestionRequest
      suggestionTimer = window.setTimeout(() => {
        void api.SuggestCommands(connection.id, prefix)
          .then(values => {
            if (disposed || request !== suggestionRequest || lineRef.current !== prefix) return
            const next = values
              .filter(value => !hiddenSuggestionCommands.has(value.command))
              .slice(0, 5)
            suggestionsRef.current = next
            setActiveSuggestion(0, next.length)
            setSuggestions(next)
            updateSuggestionPosition(next.length)
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
        onHostKeyRef.current(result.hostKey)
        return
      }
      if (result.authPrompt) {
        setStatus(result.authPrompt.message)
        onAuthPromptRef.current(result.authPrompt)
        return
      }
      if (!result.session) throw new Error('SSH session was not created')
      sessionId = result.session.sessionId
      sessionIdRef.current = sessionId
      onReadyRef.current(sessionId)
      socket = new WebSocket(result.session.url)
      socketRef.current = socket
      socket.binaryType = 'arraybuffer'
      const zmodem = new ZmodemAdapter({
        toTerminal: data => {
          const text = terminalDecoder.decode(data, { stream: true })
          for (const command of echoGuard.observeOutput(text)) addCommandHistory(command)
          terminal.write(text)
          lineRef.current = echoGuard.line
          updateSuggestionPosition()
          if (echoGuard.canSuggest()) scheduleSuggestions(echoGuard.line)
          else clearSuggestions()
        },
        send: data => {
          if (socket?.readyState === WebSocket.OPEN) socket.send(data)
        },
        waitForSendBuffer: () => waitForWebSocketBuffer(socket),
        onStatus: setStatus,
        onActive: value => {
          zmodemActiveRef.current = value
          setZmodemActive(value)
          onZmodemActiveChangeRef.current?.(value)
        },
        onTransferActivity: () => onActivityRef.current?.()
      })
      zmodemRef.current = zmodem
      socket.onopen = () => setStatus('Connected')
      socket.onmessage = event => zmodem.consume(new Uint8Array(event.data as ArrayBuffer))
      socket.onerror = () => setStatus('Connection error')
      socket.onclose = () => {
        setStatus('Connection closed')
        terminal.write('\r\n\x1b[38;5;244m[Connection closed]\x1b[0m\r\n')
        onRetryableDisconnectRef.current({ message: 'Connection closed', retryable: true })
      }
      applySuggestionRef.current = command => {
        const suffix = command.slice(echoGuard.line.length)
        if (suffix) socket?.send(suffix)
        echoGuard.appendInput(suffix)
        lineRef.current = command
        setActiveSuggestion(0, 0)
        clearSuggestions()
      }
      deleteSuggestionRef.current = deleteSuggestion
      terminal.onData(data => {
        for (const chunk of chunkTerminalInput(data)) socket?.send(chunk)
        for (const char of data) {
          if (char === '\r') {
            echoGuard.submit()
          } else if (char === '\x7f') {
            echoGuard.backspace()
          } else if (char === '\t') {
            // Tab completion changes the remote shell buffer in a way the
            // terminal cannot reconstruct reliably.
            echoGuard.markUnreliable()
          } else if (char < ' ' || char === '\x1b') {
            echoGuard.markUnreliable()
          } else if (char >= ' ') {
            echoGuard.appendInput(char)
          }
        }
        lineRef.current = echoGuard.line
        updateSuggestionPosition()
        if (echoGuard.canSuggest()) scheduleSuggestions(echoGuard.line)
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
        if (isSuggestionDismissKey(event)) {
          clearSuggestions()
          event.preventDefault()
          return false
        }
        if (isSuggestionDeleteKey(event)) {
          deleteSuggestion(suggestionIndexRef.current)
          event.preventDefault()
          return false
        }
        if (!event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey &&
          (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
          setActiveSuggestion(nextSuggestionIndex(
            suggestionIndexRef.current,
            suggestionsRef.current.length,
            event.key === 'ArrowDown' ? 1 : -1
          ))
          event.preventDefault()
          return false
        }
        let index = -1
        if (event.key === 'Tab') {
          index = suggestionIndexRef.current
        } else {
          index = directSuggestionShortcutIndex(event, suggestionsRef.current.length)
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
        updateSuggestionPosition()
      })
    }

    connect().catch(error => {
      const message = error instanceof Error ? error.message : String(error)
      const retryable = !/host key|password is required|private key is required|rejected by the server|invalid|cancelled|timed out/i.test(message)
      setStatus(message)
      terminal.write(`\r\n\x1b[31m${message}\x1b[0m\r\n`)
      onRetryableDisconnectRef.current({ message, retryable })
    })
    const observer = new ResizeObserver(() => {
      if (active) {
        fit.fit()
        updateSuggestionPosition()
      }
    })
    observer.observe(host.current)
    return () => {
      disposed = true
      observer.disconnect()
      if (zmodemActiveRef.current) onZmodemActiveChangeRef.current?.(false)
      zmodemActiveRef.current = false
      socket?.close()
      terminalRef.current = null
      socketRef.current = undefined
      zmodemRef.current = undefined
      searchRef.current = undefined
      fitRef.current = null
      sessionIdRef.current = ''
      applySuggestionRef.current = () => undefined
      deleteSuggestionRef.current = () => undefined
      window.clearTimeout(suggestionTimer)
      if (sessionId) void api.CloseSSH(sessionId)
      terminal.dispose()
      onCloseRef.current()
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
      ref={paneRef}
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
      {!!suggestions.length && <div className="command-suggestions"
        style={suggestionPosition ?? { left: 12, bottom: 30 }}>
        {suggestions.map((suggestion, index) => <div key={suggestion.id}
          className="command-suggestion-row"
          data-active={index === suggestionIndex ? 'true' : 'false'}
          onMouseEnter={() => {
            setSuggestionIndex(index)
            suggestionIndexRef.current = index
          }}>
          <button type="button" className="command-suggestion-apply"
            onMouseDown={event => {
              event.preventDefault()
              setSuggestionIndex(index)
              suggestionIndexRef.current = index
              applySuggestionRef.current(suggestion.command)
            }}>
            <kbd>{index === suggestionIndex ? 'Tab' : `Alt+${index + 1}`}</kbd><span>{suggestion.command}</span>
            <small>{suggestion.useCount}x</small>
          </button>
          <button type="button" className="command-suggestion-delete" title="删除历史"
            onMouseDown={event => {
              event.preventDefault()
              event.stopPropagation()
              setSuggestionIndex(index)
              suggestionIndexRef.current = index
              deleteSuggestionRef.current(index)
            }}>
            <Trash2 size={12} />
          </button>
        </div>)}
      </div>}
      <div className="terminal-status">
        {status}
        {privateSession ? ' · private session' : ''}
        {zmodemActive && <button onClick={() => void zmodemRef.current?.cancel()}>Cancel ZMODEM</button>}
      </div>
    </section>
  )
}

const zmodemBufferedAmountLimit = 2 * 1024 * 1024
const zmodemBufferedAmountPollMs = 16

function waitForWebSocketBuffer(socket?: WebSocket) {
  if (!socket || socket.readyState !== WebSocket.OPEN ||
    socket.bufferedAmount <= zmodemBufferedAmountLimit) {
    return Promise.resolve()
  }
  return new Promise<void>(resolve => {
    const poll = () => {
      if (!socket || socket.readyState !== WebSocket.OPEN ||
        socket.bufferedAmount <= zmodemBufferedAmountLimit) {
        resolve()
        return
      }
      window.setTimeout(poll, zmodemBufferedAmountPollMs)
    }
    window.setTimeout(poll, zmodemBufferedAmountPollMs)
  })
}

function suggestionPopupWidth(paneWidth: number, suggestions: CommandHistory[]) {
  const longest = suggestions.reduce((value, suggestion) =>
    Math.max(value, textWidthUnits(suggestion.command)), 0)
  return Math.max(220, Math.min(420, paneWidth - 12, Math.ceil(longest * 7 + 140)))
}

function textWidthUnits(value: string) {
  return Array.from(value).reduce((sum, char) =>
    sum + ((char.codePointAt(0) ?? 0) > 0x7f ? 2 : 1), 0)
}
