import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { CSSProperties } from 'react'
import {
  ChevronDown, ChevronRight, Folder, FolderPlus, LockKeyhole, Monitor,
  Moon, MoreHorizontal, Paintbrush, Pencil, Plus, Search, Settings as SettingsIcon,
  Shield, SlidersHorizontal, Sun, TerminalSquare, X
} from 'lucide-react'
import { createPortal } from 'react-dom'
import { api } from './bridge'
import { SftpPanel } from './SftpPanel'
import { SftpWorkspace } from './SftpWorkspace'
import { TerminalView } from './TerminalView'
import {
  TERMINAL_THEME_GROUPS, TERMINAL_THEME_PRESETS, cloneTerminalThemeColors,
  resolveTerminalThemeColors, terminalChromeVariables,
} from './terminalThemes'
import type {
  AccountSummary, Bootstrap, Connection, Credential, Group, InteractiveChallenge,
  PendingHostKey, Settings, SyncSummary, Tag, TerminalThemeColors
} from './types'

type SessionTab = {
  id: string
  connection: Connection
  title?: string
  attempt: number
  sshSessionId?: string
  sftp: boolean
  privateSession: boolean
  credentialOverride?: {
    name?: string
    type?: Connection['authentication']
    password?: string
    privateKeyPem?: string
    passphrase?: string
  }
}

type SSHAuthPrompt = {
  tabId: string
  connection: Connection
  value: NonNullable<Awaited<ReturnType<typeof api.StartSSH>>['authPrompt']>
}

type ThemeName = 'dark' | 'light'
type RenameTabState = { id: string; value: string }
type TerminalThemeField = keyof TerminalThemeColors

const THEME_STORAGE_KEY = 'nyaterminal.theme'

const emptyConnection: Connection = {
  id: '', name: '', remark: '', host: '', port: 22, username: 'root',
  authentication: 'password', tags: [], encoding: 'utf-8',
  sortOrder: 0,
  keepAliveSeconds: 30, connectTimeoutSeconds: 15,
  legacyAlgorithms: false, commandHistory: true
}

function isThemeName(value: string | null | undefined): value is ThemeName {
  return value === 'dark' || value === 'light'
}

function getPreferredTheme(): ThemeName {
  if (typeof window === 'undefined') return 'dark'
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
    if (isThemeName(stored)) return stored
  } catch {
    // Ignore storage failures and fall back to system preference.
  }
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

function formatDateTime(value?: string) {
  return value ? new Date(value).toLocaleString() : '暂无'
}

function decodeBase64Url(value: string) {
  const normalized = value.trim().replace(/-/g, '+').replace(/_/g, '/')
  return atob(normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '='))
}

function localizeError(value: unknown) {
  const raw = value instanceof Error ? value.message : String(value)
  const message = raw.replace(/^Error:\s*/, '').trim()
  if (!message) return '发生未知错误。'

  const codeMap: Record<string, string> = {
    authentication_required: '需要先完成身份验证。',
    invalid_token: '登录状态已失效，请重新登录。',
    invalid_credentials: '账号或密码不正确。',
    invalid_recovery_code: '恢复码无效。',
    invalid_totp_code: 'TOTP 验证码无效。',
    invalid_request: '请求参数无效。',
    invalid_json: '请求数据格式无效。',
    invalid_backup: '备份数据无效。',
    invalid_batch: '批量数据无效。',
    invalid_record: '同步记录无效。',
    invalid_record_hash: '同步记录校验失败。',
    invalid_pairing: '配对信息无效。',
    invalid_pairing_package: '配对包无效。',
    invalid_pairing_signature: '配对签名无效。',
    invalid_claim_token: '配对令牌无效。',
    invalid_cursor: '同步游标无效。',
    invalid_limit: '分页大小无效。',
    invalid_device: '设备信息无效。',
    invalid_session: '会话无效。',
    invalid_recovery_bundle: '恢复包无效。',
    restore_confirmation_required: '请先确认恢复操作。',
    reset_confirmation_required: '请先确认重置操作。',
    approved_device_required: '需要已批准的设备。',
    pairing_not_found: '未找到配对请求。',
    pairing_expired: '配对请求已过期。',
    pairing_already_approved: '配对请求已经批准。',
    pairing_already_claimed: '配对请求已经被领取。',
    device_not_found: '未找到该设备。',
    device_exists: '设备已存在。',
    device_required: '需要先绑定设备。',
    device_not_bound: '当前账号还未绑定设备。',
    session_not_found: '会话不存在。',
    conflict: '当前操作发生冲突，请刷新后重试。',
    stale_generation: '数据版本已变化，请刷新后重试。',
    sync_conflict: '同步发生冲突。',
    sync_already_initialized: '同步保险库已经初始化。',
    sync_initialize_failed: '同步初始化失败。',
    backup_not_supported: '当前环境不支持备份。',
    recovery_not_configured: '尚未配置恢复信息。',
    recovery_conflict: '恢复信息保存冲突，请重试。',
    restore_failed: '恢复失败。',
    try_again_later: '请求过于频繁，请稍后再试。',
    origin_required: '缺少来源信息。',
    origin_not_allowed: '来源不被允许。',
    request_timestamp_invalid: '请求时间戳无效。',
    replay_protection_required: '需要重放保护。',
    request_replayed: '请求已被重放拦截。',
    server_not_initialized: '服务端尚未初始化。',
    totp_already_enabled: 'TOTP 已经启用。',
    internal_error: '服务器发生内部错误。',
    already_initialized: '已经初始化过了。',
    cannot_revoke_current_device: '不能撤销当前设备。',
    device_name: '设备名称无效。',
    invalid_pairing_approval_code: '批准串无效。',
    invalid_recovery_generation: '恢复代数无效。',
  }

  const directMap: Array<[string, string]> = [
    ['synchronization requires login', '同步需要先登录。'],
    ['synchronization is not configured', '同步尚未配置。'],
    ['recovery bundle changed during recovery', '恢复过程中恢复包已变化。'],
    ['recovery code is invalid', '恢复码无效。'],
    ['invalid pairing approval code', '批准串无效。'],
    ['pairing request belongs to a different server', '批准信息来自其他服务端。'],
    ['pairing request changed after approval code generation', '批准请求在生成批准串后发生变化。'],
    ['there is no pending pairing request', '没有待处理的配对请求。'],
    ['pairing request has expired', '配对请求已过期。'],
    ['pairing package signature is invalid', '配对包签名无效。'],
    ['pairing package authentication failed', '配对包认证失败。'],
    ['pairing package contents are invalid', '配对包内容无效。'],
    ['device name is too long', '设备名称过长。'],
    ['synchronization ciphertext authentication failed', '同步数据认证失败。'],
    ['synchronization payload is invalid', '同步载荷无效。'],
    ['invalid synchronization server url', '同步服务器地址无效。'],
    ['synchronization requires https outside localhost', '本地地址之外的同步服务器必须使用 HTTPS。'],
    ['password is not configured', '未配置密码。'],
    ['private key is not configured', '未配置私钥。'],
    ['invalid private key or passphrase', '私钥或密码短语无效。'],
    ['unsupported authentication type', '不支持的认证方式。'],
    ['interactive authentication response count mismatch', '交互认证响应数量不匹配。'],
    ['interactive authentication requires more responses', '交互认证缺少响应。'],
    ['interactive authentication cancelled', 'SSH 交互认证已取消。'],
    ['interactive authentication timed out', 'SSH 交互认证超时。'],
    ['ssh authentication challenge has expired', 'SSH 交互认证已过期。'],
    ['an unlock attempt is already in progress', '已有一个解锁请求正在进行。'],
    ['too many unlock attempts; try again later', '解锁尝试过多，请稍后再试。'],
    ['windows hello verification was cancelled', '已取消 Windows Hello 验证。'],
    ['windows hello could not start a window-bound verification prompt:', 'Windows Hello 无法以附着到当前窗口的方式启动验证：'],
    ['zmodem chunk is too large', 'ZMODEM 分片过大。'],
    ['terminal input cannot be represented in selected encoding', '当前编码无法表示终端输入。'],
    ['application is not initialized', '应用尚未初始化。'],
    ['ssh host key has changed', 'SSH 主机密钥已变化。'],
    ['ssh host key is not trusted', 'SSH 主机密钥尚未信任。'],
    ['connect SSH agent:', '连接 SSH Agent 失败：'],
    ['unable to authenticate', 'SSH 认证失败。'],
    ['invalid credentials', '账号或密码不正确。'],
  ]

  const serverMatch = message.match(/^sync server returned (\d+): (.+)$/i)
  if (serverMatch) {
    const status = Number(serverMatch[1])
    const body = serverMatch[2].trim()
    try {
      const parsed = JSON.parse(body) as { error?: unknown }
      if (typeof parsed.error === 'string') {
        const localized = codeMap[parsed.error] ?? parsed.error
        return `同步服务器返回 ${status}：${localized}`
      }
    } catch {
      // Fallback to the raw body below.
    }
    return `同步服务器返回 ${status}：${body}`
  }

  const lower = message.toLowerCase()
  for (const [needle, replacement] of directMap) {
    const idx = lower.indexOf(needle.toLowerCase())
    if (idx >= 0) {
      if (needle.endsWith(':')) {
        return replacement + message.slice(idx + needle.length)
      }
      return replacement
    }
  }

  return codeMap[message] ?? message
}

function syncHeadline(syncSummary?: SyncSummary) {
  if (!syncSummary?.loggedIn) return '未登录'
  if (!syncSummary.syncInitialized) return '等待初始化'
  if (!syncSummary.configured) return '等待加入'
  return '已加入同步'
}

function syncSummaryLabel(syncSummary?: SyncSummary) {
  if (!syncSummary?.serverUrl) return '请先登录服务端账号'
  if (!syncSummary.syncInitialized) {
    return `${syncSummary.serverUrl} · ${syncSummary.username || '未填写账号'}`
  }
  const deviceLabel = displayDeviceLabel(syncSummary.deviceName, syncSummary.deviceId)
  return `${syncSummary.serverUrl} · ${syncSummary.username}${deviceLabel ? ` · ${deviceLabel}` : ''}`
}

function displayDeviceLabel(deviceName?: string, deviceId?: string) {
  return deviceName || deviceId || ''
}

function connectionLabel(connection: Connection) {
  return connection.name || connection.host || '未命名终端'
}

function sessionLabel(session: Pick<SessionTab, 'title' | 'connection'>) {
  return session.title?.trim() || connectionLabel(session.connection)
}

function sortGroups(groups: Group[]) {
  return [...groups].sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name))
}

function buildGroupIndex(groups: Group[]) {
  const ordered = sortGroups(groups)
  const byId = new Map(ordered.map(group => [group.id, group]))
  const childrenByParent = new Map<string, Group[]>()
  for (const group of ordered) {
    const parentId = group.parentId ?? ''
    const siblings = childrenByParent.get(parentId)
    if (siblings) siblings.push(group)
    else childrenByParent.set(parentId, [group])
  }
  return { byId, childrenByParent }
}

function groupPathIds(groupId: string, byId: Map<string, Group>) {
  const path: string[] = []
  const visited = new Set<string>()
  let current = byId.get(groupId)
  while (current && !visited.has(current.id)) {
    path.unshift(current.id)
    visited.add(current.id)
    current = current.parentId ? byId.get(current.parentId) : undefined
  }
  return path
}

function groupPathLabel(groupId: string | undefined, byId: Map<string, Group>) {
  if (!groupId) return ''
  return groupPathIds(groupId, byId)
    .map(id => byId.get(id)?.name)
    .filter((value): value is string => Boolean(value))
    .join(' / ')
}

function nextGroupSortOrder(groups: Group[], parentId: string) {
  return groups.filter(group => (group.parentId ?? '') === parentId)
    .reduce((maximum, group) => Math.max(maximum, group.sortOrder), -1) + 1
}

function normalizeHexColorInput(value: string) {
  const trimmed = value.trim()
  return /^#[0-9a-fA-F]{6}$/.test(trimmed) ? trimmed.toUpperCase() : undefined
}

export function App() {
  const [bootstrap, setBootstrap] = useState<Bootstrap>()
  const [error, setError] = useState('')
  const [theme, setTheme] = useState<ThemeName>(() => getPreferredTheme())
  const [connectionEditor, setConnectionEditor] = useState<Connection>()
  const [groupEditor, setGroupEditor] = useState(false)
  const [tagEditor, setTagEditor] = useState(false)
  const [accountManagerOpen, setAccountManagerOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [sftpWorkspace, setSftpWorkspace] = useState<Connection>()
  const [sessions, setSessions] = useState<SessionTab[]>([])
  const [activeSession, setActiveSession] = useState('')
  const [tabMenuOpen, setTabMenuOpen] = useState(false)
  const [renamingTab, setRenamingTab] = useState<RenameTabState>()
  const [hostKey, setHostKey] = useState<{ tabId: string; value: PendingHostKey }>()
  const [sshAuthPrompt, setSSHAuthPrompt] = useState<SSHAuthPrompt>()
  const [interactiveChallenge, setInteractiveChallenge] = useState<InteractiveChallenge>()
  const [query, setQuery] = useState('')
  const [activeTag, setActiveTag] = useState('')
  const [syncBusy, setSyncBusy] = useState(false)
  const activityTimer = useRef<number | undefined>(undefined)
  const searchInput = useRef<HTMLInputElement>(null)
  const tabScroll = useRef<HTMLDivElement>(null)
  const tabMenu = useRef<HTMLDivElement>(null)
  const tabMenuButton = useRef<HTMLButtonElement>(null)
  const tabButtons = useRef<Record<string, HTMLButtonElement | null>>({})

  const reload = useCallback(async () => {
    try {
      setError('')
      setBootstrap(await api.Bootstrap())
    } catch (value) {
      setError(localizeError(value))
    }
  }, [])

  useEffect(() => { void reload() }, [reload])

  useEffect(() => {
    const nextTheme = bootstrap?.settings?.theme
    if (!isThemeName(nextTheme)) return
    setTheme(nextTheme)
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, nextTheme)
    } catch {
      // Ignore storage failures; the in-memory theme still updates.
    }
  }, [bootstrap?.settings?.theme])

  useEffect(() => {
    return window.runtime?.EventsOn?.('ssh:interactive-challenge', value => {
      setInteractiveChallenge(value as InteractiveChallenge)
    })
  }, [])

  useEffect(() => {
    return window.runtime?.EventsOn?.('sync:logged-out', () => {
      void reload()
    })
  }, [reload])

  useEffect(() => {
    if (!tabMenuOpen) return
    const closeOnOutside = (event: PointerEvent) => {
      const target = event.target as Node | null
      if (!target) return
      if (tabMenu.current?.contains(target) || tabMenuButton.current?.contains(target)) return
      setTabMenuOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setTabMenuOpen(false)
    }
    window.addEventListener('pointerdown', closeOnOutside)
    window.addEventListener('keydown', closeOnEscape)
    return () => {
      window.removeEventListener('pointerdown', closeOnOutside)
      window.removeEventListener('keydown', closeOnEscape)
    }
  }, [tabMenuOpen])

  useEffect(() => {
    const target = activeSession ? tabButtons.current[activeSession] : null
    target?.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'nearest' })
  }, [activeSession])

  useEffect(() => {
    if (sessions.length) return
    setTabMenuOpen(false)
  }, [sessions.length])

  const resetActivity = useCallback(() => {
    if (activityTimer.current) window.clearTimeout(activityTimer.current)
    const minutes = bootstrap?.settings?.lockAfterMinutes ?? 0
    if (minutes > 0 && !bootstrap?.vault.locked) {
      activityTimer.current = window.setTimeout(() => void lock(), minutes * 60_000)
    }
  }, [bootstrap])

  useEffect(() => {
    const events = ['pointerdown', 'keydown', 'wheel']
    events.forEach(name => window.addEventListener(name, resetActivity, { passive: true }))
    resetActivity()
    return () => {
      events.forEach(name => window.removeEventListener(name, resetActivity))
      if (activityTimer.current) window.clearTimeout(activityTimer.current)
    }
  }, [resetActivity])

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey)) return
      const key = event.key.toLowerCase()
      if (key === 'k') {
        event.preventDefault()
        searchInput.current?.focus()
      } else if (key === 'l') {
        event.preventDefault()
        void lock()
      } else if (key === 'n') {
        event.preventDefault()
        setConnectionEditor({ ...emptyConnection })
      }
    }
    window.addEventListener('keydown', shortcut)
    return () => window.removeEventListener('keydown', shortcut)
  }, [bootstrap?.settings?.disconnectOnLock])

  const lock = async () => {
    const disconnect = bootstrap?.settings?.disconnectOnLock ?? true
    await api.Lock()
    void navigator.clipboard?.writeText('').catch(() => undefined)
    if (disconnect) {
      setSessions([])
      setActiveSession('')
    }
    setBootstrap(current => current ? {
      ...current,
      vault: { ...current.vault, locked: true }
    } : current)
  }

  useEffect(() => {
    if (bootstrap?.vault.locked) return
    let lastTick = Date.now()
    const timer = window.setInterval(() => {
      const now = Date.now()
      if (now - lastTick > 30_000) void lock()
      lastTick = now
    }, 5_000)
    let hiddenTimer = 0
    const visibility = () => {
      if (document.visibilityState === 'hidden') {
        hiddenTimer = window.setTimeout(() => void lock(), 30_000)
      } else if (hiddenTimer) {
        window.clearTimeout(hiddenTimer)
      }
    }
    document.addEventListener('visibilitychange', visibility)
    return () => {
      window.clearInterval(timer)
      if (hiddenTimer) window.clearTimeout(hiddenTimer)
      document.removeEventListener('visibilitychange', visibility)
    }
  }, [bootstrap?.vault.locked, bootstrap?.settings?.disconnectOnLock])

  const openConnection = (connection: Connection, privateSession = false) => {
    const id = crypto.randomUUID()
    setSessions(current => [...current, {
      id, connection, title: undefined, attempt: 0, sftp: false, privateSession, credentialOverride: undefined
    }])
    setActiveSession(id)
  }

  const closeSession = (id: string) => {
    setSessions(current => {
      const tab = current.find(item => item.id === id)
      if (tab?.sshSessionId) void api.CloseSSH(tab.sshSessionId)
      const next = current.filter(item => item.id !== id)
      if (activeSession === id) setActiveSession(next.at(-1)?.id ?? '')
      return next
    })
  }

  const openRenameTab = (tab: SessionTab) => {
    setRenamingTab({ id: tab.id, value: tab.title ?? '' })
    setTabMenuOpen(false)
  }

  const saveTabTitle = (id: string, value: string) => {
    const nextTitle = value.trim()
    setSessions(current => current.map(item =>
      item.id === id ? { ...item, title: nextTitle || undefined } : item
    ))
    setRenamingTab(undefined)
  }

  const handleTabListWheel = useCallback((event: React.WheelEvent<HTMLDivElement>) => {
    const container = event.currentTarget
    if (Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return
    if (container.scrollWidth <= container.clientWidth) return
    container.scrollLeft += event.deltaY
    event.preventDefault()
  }, [])

  const filteredConnections = useMemo(() => {
    const value = query.trim().toLowerCase()
    return (bootstrap?.connections ?? []).filter(connection =>
      (!activeTag || connection.tags.includes(activeTag)) &&
      (!value || `${connection.name} ${connection.host} ${connection.username}`.toLowerCase().includes(value))
    )
  }, [bootstrap?.connections, query, activeTag])

  if (!bootstrap) {
    return <CenteredCard theme={theme} title="NyaTerminal" subtitle="正在准备安全保险库…"><Spinner /></CenteredCard>
  }

  if (!bootstrap.vault.initialized) {
    return <VaultSetup theme={theme} onComplete={reload} />
  }

  if (bootstrap.vault.locked && (!bootstrap.settings || sessions.length === 0)) {
    return <Unlock theme={theme} quickUnlock={bootstrap.vault.quickUnlock} onComplete={reload} />
  }

  const settings = bootstrap.settings!
  const activeTab = sessions.find(session => session.id === activeSession)

  return (
    <div className={`app-shell theme-${settings.theme}`}>
      <aside className="sidebar">
        <div className="brand-row">
          <div className="brand-mark">N</div>
          <div><strong>NyaTerminal</strong><small>Secure workspace</small></div>
          <button className="icon-button" onClick={() => setAccountManagerOpen(true)} title="账号管理">
            <Shield size={17} />
          </button>
          <button className="icon-button" onClick={() => void lock()} title="锁屏"><LockKeyhole size={17} /></button>
        </div>
        <div className="search-box"><Search size={16} />
          <input ref={searchInput} placeholder="搜索连接" value={query}
            onChange={event => setQuery(event.target.value)} />
          <kbd>⌘K</kbd>
        </div>
        <div className="section-heading">
          <span>连接</span>
          <div>
            <button title="新建分组" onClick={() => setGroupEditor(true)}><FolderPlus size={15} /></button>
            <button title="新建连接" onClick={() => setConnectionEditor({ ...emptyConnection })}><Plus size={16} /></button>
          </div>
        </div>
        <nav className="connection-tree">
          <GroupTree groups={bootstrap.groups ?? []} connections={filteredConnections}
            onOpen={openConnection} onEdit={setConnectionEditor} onChanged={reload} />
          {!filteredConnections.length && <div className="empty-tree">还没有连接</div>}
        </nav>
        <div className="tag-section">
          <div className="section-heading"><span>标签</span>
            <button title="新建标签" onClick={() => setTagEditor(true)}><Plus size={14} /></button></div>
          <div className="tag-list">
            {(bootstrap.tags ?? []).map(tag => <button key={tag.id}
              className={activeTag === tag.id ? 'active' : ''}
              onClick={() => setActiveTag(current => current === tag.id ? '' : tag.id)}
              onContextMenu={event => {
                event.preventDefault()
                const name = window.prompt('修改标签名称；留空将删除标签', tag.name)
                if (name === null) return
                if (!name.trim()) {
                  if (window.confirm(`确定删除标签 ${tag.name}？`)) {
                    void api.DeleteTag(tag.id).then(reload).catch(reason => setError(localizeError(reason)))
                  }
                } else {
                  void api.SaveTag({ ...tag, name: name.trim() }).then(reload)
                    .catch(reason => setError(localizeError(reason)))
                }
              }}>
              <i style={{ background: tag.color }} /><span>{tag.name}</span>
            </button>)}
          </div>
        </div>
        <div className="sidebar-footer">
          <button onClick={() => setSettingsOpen(true)}><SettingsIcon size={17} />设置</button>
          <button><MoreHorizontal size={17} /></button>
        </div>
      </aside>

      <main className="workspace">
        <div className="tabbar">
          <div className="tab-scroll" ref={tabScroll} onWheel={handleTabListWheel}>
            <div className="tab-strip">
              {sessions.map(tab => (
                <button
                  key={tab.id}
                  ref={node => { tabButtons.current[tab.id] = node }}
                  className={`session-tab ${tab.id === activeSession ? 'active' : ''}`}
                  onClick={() => setActiveSession(tab.id)}
                  onDoubleClick={() => openRenameTab(tab)}
                  title={sessionLabel(tab)}
                >
                  <TerminalSquare size={15} /><span>{sessionLabel(tab)}</span>
                  <i
                    onClick={event => {
                      event.stopPropagation()
                      closeSession(tab.id)
                    }}
                    title="关闭标签页"
                  >
                    <X size={13} />
                  </i>
                </button>
              ))}
            </div>
          </div>
          <div className="tabbar-actions">
            <button
              ref={tabMenuButton}
              className={`tab-menu-toggle ${tabMenuOpen ? 'active' : ''}`}
              onClick={() => setTabMenuOpen(current => !current)}
              title="终端标签列表"
            >
              <ChevronDown size={16} />
            </button>
            {tabMenuOpen && (
              <div className="tab-switcher-menu" ref={tabMenu}>
                <div className="tab-switcher-list">
                  {sessions.length ? sessions.map(tab => (
                    <div key={tab.id} className={`tab-switcher-item ${tab.id === activeSession ? 'active' : ''}`}>
                      <button
                        className="tab-switcher-select"
                        onClick={() => {
                          setActiveSession(tab.id)
                          setTabMenuOpen(false)
                        }}
                        title={sessionLabel(tab)}
                      >
                        <TerminalSquare size={15} />
                        <span className="tab-switcher-copy">
                          <strong>{sessionLabel(tab)}</strong>
                          <small>{tab.connection.username}@{tab.connection.host}:{tab.connection.port}</small>
                        </span>
                      </button>
                      <button
                        className="tab-switcher-action"
                        onClick={() => openRenameTab(tab)}
                        title="重命名标签页"
                      >
                        <Pencil size={14} />
                      </button>
                      <button
                        className="tab-switcher-action danger"
                        onClick={() => closeSession(tab.id)}
                        title="关闭标签页"
                      >
                        <X size={14} />
                      </button>
                    </div>
                  )) : (
                    <div className="tab-switcher-empty">暂无打开的标签页</div>
                  )}
                </div>
              </div>
            )}
            <div className="window-drag" />
          </div>
          {activeTab && (
            <button className={`sftp-toggle ${activeTab.sftp ? 'active' : ''}`}
              onClick={() => setSessions(current => current.map(item =>
                item.id === activeTab.id ? { ...item, sftp: !item.sftp } : item
              ))}>
              <Folder size={15} />SFTP
            </button>
          )}
        </div>
        {!sessions.length && <Welcome onCreate={() => setConnectionEditor({ ...emptyConnection })} />}
        <div className="terminal-stack">
          {sessions.map(tab => (
            <TerminalView key={`${tab.id}:${tab.attempt}`} connection={tab.connection}
              settings={settings} active={tab.id === activeSession}
              privateSession={tab.privateSession}
              credentialOverride={tab.credentialOverride}
              onReady={sessionId => setSessions(current => current.map(item =>
                item.id === tab.id
                  ? { ...item, sshSessionId: sessionId, credentialOverride: undefined }
                  : item
              ))}
              onHostKey={value => setHostKey({ tabId: tab.id, value })}
              onAuthPrompt={value => setSSHAuthPrompt({ tabId: tab.id, connection: tab.connection, value })}
              onClose={() => undefined} />
          ))}
        </div>
      </main>

      {activeTab?.sftp && (
        <SftpPanel connection={activeTab.connection}
          onOpenWorkspace={() => setSftpWorkspace(activeTab.connection)}
          onClose={() => setSessions(current => current.map(item =>
            item.id === activeTab.id ? { ...item, sftp: false } : item
          ))} />
      )}
      {sftpWorkspace && <SftpWorkspace connection={sftpWorkspace}
        onClose={() => setSftpWorkspace(undefined)} />}

      {connectionEditor && (
        <ConnectionEditor initial={connectionEditor} groups={bootstrap.groups ?? []}
          tags={bootstrap.tags ?? []}
          onGroupsUpdated={groups => setBootstrap(current => current ? { ...current, groups } : current)}
          onClose={() => setConnectionEditor(undefined)}
          onDeleted={async id => {
            await api.DeleteConnection(id)
            setConnectionEditor(undefined)
            await reload()
          }}
          onSaved={async (connection, connect) => {
            setConnectionEditor(undefined)
            await reload()
            if (connect) openConnection(connection)
          }} />
      )}
      {groupEditor && <GroupEditor groups={bootstrap.groups ?? []} onClose={() => setGroupEditor(false)}
        onSaved={async _group => { setGroupEditor(false); await reload() }} />}
      {tagEditor && <TagEditor onClose={() => setTagEditor(false)}
        onSaved={async () => { setTagEditor(false); await reload() }} />}
      {renamingTab && <RenameTabDialog
        value={renamingTab.value}
        placeholder={sessionLabel(sessions.find(item => item.id === renamingTab.id) ?? {
          connection: emptyConnection,
          title: undefined
        })}
        onClose={() => setRenamingTab(undefined)}
        onSave={value => saveTabTitle(renamingTab.id, value)}
      />}
      {accountManagerOpen && <AccountManagerDialog
        account={bootstrap.account}
        onClose={() => setAccountManagerOpen(false)}
        onReload={reload}
      />}
      {settingsOpen && <SettingsDialog value={settings} vault={bootstrap.vault}
        syncSummary={bootstrap.syncSummary}
        syncBusy={syncBusy}
        onSyncBusyChange={setSyncBusy}
        onClose={() => setSettingsOpen(false)}
        onReload={reload}
        onSaved={async () => { setSettingsOpen(false); await reload() }} />}
      {hostKey && (
        <HostKeyDialog value={hostKey.value} onCancel={() => {
          closeSession(hostKey.tabId)
          setHostKey(undefined)
        }} onAccept={async () => {
          await api.AcceptHostKey(hostKey.value.id)
          setSessions(current => current.map(item =>
            item.id === hostKey.tabId ? { ...item, attempt: item.attempt + 1 } : item
          ))
          setHostKey(undefined)
        }} />
      )}
      {sshAuthPrompt && (
        <SSHAuthPromptDialog
          connection={sshAuthPrompt.connection}
          value={sshAuthPrompt.value}
          onCancel={() => {
            closeSession(sshAuthPrompt.tabId)
            setSSHAuthPrompt(undefined)
          }}
          onSubmit={async payload => {
            let credentialId = sshAuthPrompt.connection.credentialId
            const credentialOverride = sshAuthPrompt.connection.authentication === 'password'
              ? {
                type: sshAuthPrompt.connection.authentication,
                password: payload.password
              }
              : {
                type: sshAuthPrompt.connection.authentication,
                privateKeyPem: payload.privateKeyPem,
                passphrase: payload.passphrase
              }
            if (payload.save) {
              const credential: Credential = await api.SaveCredential({
                id: credentialId ?? '',
                name: `${sshAuthPrompt.connection.name || sshAuthPrompt.connection.host} credential`,
                type: sshAuthPrompt.connection.authentication,
                password: sshAuthPrompt.connection.authentication === 'password' ? payload.password : undefined,
                privateKeyPem: sshAuthPrompt.connection.authentication === 'private_key'
                  ? payload.privateKeyPem
                  : undefined,
                passphrase: sshAuthPrompt.connection.authentication === 'private_key'
                  ? payload.passphrase
                  : undefined
              })
              credentialId = credential.id
            }
            const nextConnection = await api.SaveConnection({
              ...sshAuthPrompt.connection,
              credentialId
            })
            setSessions(current => current.map(item =>
              item.id === sshAuthPrompt.tabId
                ? {
                  ...item,
                  connection: nextConnection,
                  credentialOverride: payload.save ? undefined : credentialOverride,
                  attempt: item.attempt + 1
                }
                : item
            ))
            setSSHAuthPrompt(undefined)
            await reload()
          }}
        />
      )}
      {interactiveChallenge && (
        <InteractiveChallengeDialog value={interactiveChallenge}
          onSubmit={async answers => {
            await api.AnswerSSHChallenge(interactiveChallenge.id, answers, false)
            setInteractiveChallenge(undefined)
          }}
          onCancel={async () => {
            await api.AnswerSSHChallenge(interactiveChallenge.id, [], true)
            setInteractiveChallenge(undefined)
          }} />
      )}
      {error && <div className="toast-error" onClick={() => setError('')}>{error}</div>}
      {bootstrap.vault.locked && (
        <div className="lock-overlay">
          <Unlock theme={theme} quickUnlock={bootstrap.vault.quickUnlock} onComplete={reload} />
        </div>
      )}
    </div>
  )
}

function GroupTree({ groups, connections, onOpen, onEdit, onChanged }: {
  groups: Group[]
  connections: Connection[]
  onOpen: (connection: Connection, privateSession?: boolean) => void
  onEdit: (connection: Connection) => void
  onChanged: () => Promise<void>
}) {
  const orderedGroups = [...groups].sort((a, b) =>
    a.sortOrder - b.sortOrder || a.name.localeCompare(b.name))
  const roots = orderedGroups.filter(group => !group.parentId)
  const ungrouped = connections.filter(connection => !connection.groupId)
  const dropInto = async (event: React.DragEvent, parentId: string) => {
    event.preventDefault()
    const connectionId = event.dataTransfer.getData('application/x-nya-connection')
    const groupId = event.dataTransfer.getData('application/x-nya-group')
    if (connectionId) {
      const connection = connections.find(value => value.id === connectionId)
      const nextOrder = connections.filter(value => (value.groupId ?? '') === parentId)
        .reduce((maximum, value) => Math.max(maximum, value.sortOrder), -1) + 1
      if (connection) await api.SaveConnection({
        ...connection, groupId: parentId, sortOrder: nextOrder
      })
    } else if (groupId && groupId !== parentId) {
      const group = groups.find(value => value.id === groupId)
      const nextOrder = groups.filter(value => (value.parentId ?? '') === parentId)
        .reduce((maximum, value) => Math.max(maximum, value.sortOrder), -1) + 1
      if (group) await api.SaveGroup({ ...group, parentId, sortOrder: nextOrder })
    }
    await onChanged()
  }
  return <>
    {roots.map(group => <GroupNode key={group.id} group={group} groups={groups}
      connections={connections} onOpen={onOpen} onEdit={onEdit}
      onChanged={onChanged} onDropInto={dropInto} />)}
    <div className="ungrouped-drop" onDragOver={event => event.preventDefault()}
      onDrop={event => void dropInto(event, '')}>
    {ungrouped.map(connection => <ConnectionRow key={connection.id} value={connection}
      onOpen={onOpen} onEdit={onEdit} />)}
    </div>
  </>
}

function GroupNode({ group, groups, connections, onOpen, onEdit, onChanged, onDropInto }: {
  group: Group; groups: Group[]; connections: Connection[]
  onOpen: (connection: Connection, privateSession?: boolean) => void; onEdit: (connection: Connection) => void
  onChanged: () => Promise<void>
  onDropInto: (event: React.DragEvent, parentId: string) => Promise<void>
}) {
  const [open, setOpen] = useState(true)
  const children = groups.filter(value => value.parentId === group.id)
    .sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name))
  const items = connections.filter(value => value.groupId === group.id)
    .sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name))
  return <div className="group-node">
    <button className="group-row" draggable
      onDragStart={event => event.dataTransfer.setData('application/x-nya-group', group.id)}
      onDragOver={event => event.preventDefault()}
      onDrop={event => { event.stopPropagation(); void onDropInto(event, group.id) }}
      onClick={() => setOpen(value => !value)}
      onContextMenu={event => {
        event.preventDefault()
        const name = window.prompt('修改分组名称；留空将删除分组', group.name)
        if (name === null) return
        if (!name.trim()) {
          if (window.confirm(`确定删除空分组 ${group.name}？`)) {
            void api.DeleteGroup(group.id).then(onChanged)
          }
        } else {
          void api.SaveGroup({ ...group, name: name.trim() }).then(onChanged)
        }
      }}>
      {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
      <Folder size={15} /><span>{group.name}</span>
      <small>{items.length}</small>
    </button>
    {open && <div className="group-children">
      {children.map(child => <GroupNode key={child.id} group={child} groups={groups}
        connections={connections} onOpen={onOpen} onEdit={onEdit}
        onChanged={onChanged} onDropInto={onDropInto} />)}
      {items.map(connection => <ConnectionRow key={connection.id} value={connection}
        onOpen={onOpen} onEdit={onEdit} />)}
    </div>}
  </div>
}

function ConnectionRow({ value, onOpen, onEdit }: {
  value: Connection; onOpen: (value: Connection, privateSession?: boolean) => void; onEdit: (value: Connection) => void
}) {
  return <button className="connection-row" draggable
    onDragStart={event => event.dataTransfer.setData('application/x-nya-connection', value.id)}
    title="双击连接；右键打开不记录历史的隐私会话"
    onDoubleClick={() => onOpen(value)}
    onContextMenu={event => { event.preventDefault(); onOpen(value, true) }}>
    <span className="status-dot" /><span className="connection-copy">
      <strong>{value.name}</strong><small>{value.username}@{value.host}:{value.port}</small>
    </span>
    <i onClick={event => { event.stopPropagation(); onEdit(value) }}><MoreHorizontal size={15} /></i>
  </button>
}

function Welcome({ onCreate }: { onCreate: () => void }) {
  return <div className="welcome">
    <div className="welcome-art"><span>&gt;_</span></div>
    <h1>连接你的下一台主机</h1>
    <p>安全地管理 SSH 会话、SFTP 文件和多端加密同步。</p>
    <button className="primary" onClick={onCreate}><Plus size={17} />新建 SSH 连接</button>
    <div className="shortcut-grid">
      <span><kbd>Ctrl</kbd><kbd>K</kbd>搜索连接</span>
      <span><kbd>Ctrl</kbd><kbd>L</kbd>锁定保险库</span>
    </div>
  </div>
}

function VaultSetup({ theme, onComplete }: { theme: ThemeName; onComplete: () => Promise<void> }) {
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const submit = async () => {
    if (password !== confirm) return setError('两次输入的密码不一致')
    try {
      await api.InitializeVault(password)
      await onComplete()
    } catch (value) { setError(String(value)) }
  }
  return <CenteredCard theme={theme} title="创建安全保险库" subtitle="主密码用于保护本机保存的连接和凭据。">
    <label>主密码<input autoFocus type="password" value={password}
      onChange={event => setPassword(event.target.value)} /></label>
    <label>确认主密码<input type="password" value={confirm}
      onChange={event => setConfirm(event.target.value)}
      onKeyDown={event => event.key === 'Enter' && void submit()} /></label>
    <small className="hint">至少 12 个字符。忘记主密码后，本地数据无法恢复。</small>
    {error && <div className="form-error">{error}</div>}
    <button className="primary wide" onClick={() => void submit()}>创建保险库</button>
  </CenteredCard>
}

function Unlock({ theme, quickUnlock, onComplete }: { theme: ThemeName; quickUnlock: boolean; onComplete: () => Promise<void> }) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [unlockBusy, setUnlockBusy] = useState(false)
  const [systemUnlockBusy, setSystemUnlockBusy] = useState(false)
  const submit = async () => {
    if (unlockBusy || systemUnlockBusy) return
    setUnlockBusy(true)
    try {
      setError('')
      await api.Unlock(password)
      await onComplete()
    } catch (reason) { setError(localizeError(reason)) }
    finally { setUnlockBusy(false) }
  }
  const unlockWithSystem = async () => {
    if (unlockBusy || systemUnlockBusy) return
    setSystemUnlockBusy(true)
    try {
      setError('')
      await api.UnlockWithSystem()
      await onComplete()
    } catch (reason) {
      setError(localizeError(reason))
    } finally {
      setSystemUnlockBusy(false)
    }
  }
  return <CenteredCard theme={theme} title="欢迎回来" subtitle="保险库已锁定，请验证后继续。">
    <div className="unlock-icon"><LockKeyhole /></div>
    <label>主密码<input autoFocus type="password" value={password}
      onChange={event => setPassword(event.target.value)}
      onKeyDown={event => event.key === 'Enter' && void submit()} /></label>
    {error && <div className="form-error">{error}</div>}
    <button className="primary wide" disabled={unlockBusy || systemUnlockBusy} onClick={() => void submit()}>
      {unlockBusy ? '解锁中…' : '解锁'}
    </button>
    {quickUnlock && <button
      className="system-unlock wide"
      disabled={unlockBusy || systemUnlockBusy}
      onClick={() => void unlockWithSystem()}
    >{systemUnlockBusy ? '等待系统验证…' : '使用系统凭据解锁'}</button>}
  </CenteredCard>
}

function CenteredCard({ theme, title, subtitle, children }: {
  theme: ThemeName; title: string; subtitle: string; children: React.ReactNode
}) {
  return <div className={`auth-screen theme-${theme}`}><div className="auth-card">
    <div className="auth-brand"><div className="brand-mark large">N</div></div>
    <h1>{title}</h1><p>{subtitle}</p>{children}
  </div></div>
}

function Spinner() { return <div className="spinner" /> }

function modalPortalTarget() {
  if (typeof document === 'undefined') return null
  return document.querySelector('.app-shell') ?? document.body
}

function Modal({ title, children, onClose, width = '520px', footer, bodyClassName }: {
  title: string; children: React.ReactNode; onClose: () => void; width?: string
  footer?: React.ReactNode; bodyClassName?: string
}) {
  const target = modalPortalTarget()
  if (!target) return null
  return createPortal(
    <div className="modal-backdrop" onMouseDown={event => event.target === event.currentTarget && onClose()}>
      <section className="modal" style={{ width }}>
        <header><h2>{title}</h2><button onClick={onClose}><X size={18} /></button></header>
        <div className={bodyClassName ? `modal-body ${bodyClassName}` : 'modal-body'}>{children}</div>
        {footer && <footer className="modal-actions">{footer}</footer>}
      </section>
    </div>,
    target,
  )
}

function GroupCascader({
  groups,
  value,
  onChange,
  rootLabel,
  allowAdd = false,
  disabledGroupIds = [],
  onAddGroup,
}: {
  groups: Group[]
  value?: string
  onChange: (value: string) => void
  rootLabel: string
  allowAdd?: boolean
  disabledGroupIds?: string[]
  onAddGroup?: (parentId: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [activePath, setActivePath] = useState<string[]>([])
  const host = useRef<HTMLDivElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const [panelStyle, setPanelStyle] = useState<CSSProperties>()
  const { byId, childrenByParent } = useMemo(() => buildGroupIndex(groups), [groups])
  const selectedLabel = groupPathLabel(value, byId)
  const disabled = useMemo(() => new Set(disabledGroupIds), [disabledGroupIds])
  const target = modalPortalTarget()

  useEffect(() => {
    if (!open) return
    setActivePath(value ? groupPathIds(value, byId) : [])
  }, [open, value, byId])

  useEffect(() => {
    if (!open) return
    const updatePanelPosition = () => {
      const trigger = host.current?.querySelector('.group-cascader-trigger')
      if (!(trigger instanceof HTMLElement)) return
      const rect = trigger.getBoundingClientRect()
      const viewportWidth = window.innerWidth
      const panelWidth = Math.min(560, viewportWidth - 32)
      const left = Math.min(rect.left, Math.max(16, viewportWidth - panelWidth - 16))
      setPanelStyle({
        position: 'fixed',
        top: rect.bottom + 6,
        left,
        width: panelWidth,
      })
    }
    updatePanelPosition()
    const closeOnOutside = (event: PointerEvent) => {
      const targetNode = event.target as Node | null
      if (host.current?.contains(targetNode) || panel.current?.contains(targetNode)) return
      setOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    const closeOnScroll = () => updatePanelPosition()
    window.addEventListener('resize', updatePanelPosition)
    window.addEventListener('scroll', closeOnScroll, true)
    window.addEventListener('pointerdown', closeOnOutside)
    window.addEventListener('keydown', closeOnEscape)
    return () => {
      window.removeEventListener('resize', updatePanelPosition)
      window.removeEventListener('scroll', closeOnScroll, true)
      window.removeEventListener('pointerdown', closeOnOutside)
      window.removeEventListener('keydown', closeOnEscape)
    }
  }, [open])

  const columns = useMemo(() => {
    const result: Group[][] = [childrenByParent.get('') ?? []]
    for (const parentId of activePath) {
      const children = childrenByParent.get(parentId) ?? []
      if (!children.length) break
      result.push(children)
    }
    return result
  }, [activePath, childrenByParent])

  return <div className="group-cascader" ref={host}>
    <button type="button" className={`group-cascader-trigger${open ? ' open' : ''}`}
      onClick={() => setOpen(current => !current)}>
      <span>{selectedLabel || rootLabel}</span>
      <ChevronDown size={16} />
    </button>
    {open && target && createPortal(<div className="group-cascader-panel" style={panelStyle} ref={panel}>
      <div className="group-cascader-toolbar">
        <button type="button" className={`group-cascader-root${!value ? ' selected' : ''}`}
          onClick={() => {
            onChange('')
            setActivePath([])
          }}>
          {rootLabel}
        </button>
        {allowAdd && onAddGroup && <button type="button" className="group-cascader-add"
          onClick={() => {
            setOpen(false)
            onAddGroup(activePath[activePath.length - 1] ?? value ?? '')
          }}>
          <FolderPlus size={14} />添加分组
        </button>}
      </div>
      <div className="group-cascader-columns">
        {!columns[0].length && <div className="group-cascader-empty">暂无分组</div>}
        {columns.map((column, columnIndex) => <div key={columnIndex} className="group-cascader-column">
          {column.map(group => {
            const hasChildren = Boolean((childrenByParent.get(group.id) ?? []).length)
            const path = groupPathIds(group.id, byId)
            const isActive = activePath[columnIndex] === group.id
            const isSelected = value === group.id
            const isDisabled = disabled.has(group.id)
            return <button key={group.id} type="button"
              className={[
                'group-cascader-option',
                isActive ? 'active' : '',
                isSelected ? 'selected' : '',
              ].filter(Boolean).join(' ')}
              disabled={isDisabled}
              onClick={() => {
                onChange(group.id)
                setActivePath(path)
              }}>
              <span>{group.name}</span>
              {hasChildren && <ChevronRight size={14} />}
            </button>
          })}
        </div>)}
      </div>
    </div>, target)}
  </div>
}

function ConnectionEditor({ initial, groups, tags, onGroupsUpdated, onClose, onSaved, onDeleted }: {
  initial: Connection; groups: Group[]; tags: Tag[]; onGroupsUpdated?: (groups: Group[]) => void; onClose: () => void
  onSaved: (value: Connection, connect: boolean) => Promise<void>
  onDeleted: (id: string) => Promise<void>
}) {
  const [value, setValue] = useState(initial)
  const [availableGroups, setAvailableGroups] = useState(groups)
  const [groupEditorParentId, setGroupEditorParentId] = useState<string>()
  const [secret, setSecret] = useState('')
  const [passphrase, setPassphrase] = useState('')
  const [error, setError] = useState('')
  useEffect(() => { setAvailableGroups(groups) }, [groups])
  const update = <K extends keyof Connection>(key: K, next: Connection[K]) =>
    setValue(current => ({ ...current, [key]: next }))
  const syncGroups = (nextGroups: Group[]) => {
    setAvailableGroups(nextGroups)
    onGroupsUpdated?.(nextGroups)
  }
  const save = async (connect: boolean) => {
    try {
      let credentialId = value.credentialId
      if (value.authentication !== 'agent' && (secret || !credentialId)) {
        const credential: Credential = await api.SaveCredential({
          id: credentialId ?? '',
          name: `${value.name || value.host} credential`,
          type: value.authentication,
          password: value.authentication === 'password' ? secret : undefined,
          privateKeyPem: value.authentication === 'private_key' ? secret : undefined,
          passphrase: value.authentication === 'private_key' ? passphrase : undefined
        })
        credentialId = credential.id
      }
      await onSaved(await api.SaveConnection({ ...value, credentialId }), connect)
    } catch (reason) { setError(localizeError(reason)) }
  }
  return <>
    <Modal title={value.id ? '编辑 SSH 连接' : '新建 SSH 连接'} onClose={onClose} width="680px"
      footer={<>
        {value.id && <button className="danger-button modal-action-leading" onClick={() => {
          if (window.confirm(`确定删除连接 ${value.name}？`)) void onDeleted(value.id)
        }}>删除</button>}
        <button onClick={onClose}>取消</button>
        <button onClick={() => void save(false)}>保存</button>
        <button className="primary" onClick={() => void save(true)}>保存并连接</button>
      </>}>
      <div className="form-grid">
      <label className="full">名称<input value={value.name} onChange={e => update('name', e.target.value)} /></label>
      <label className="full">备注<textarea rows={3} value={value.remark ?? ''}
        onChange={e => update('remark', e.target.value)} /></label>
      <label className="wide">主机<input value={value.host} onChange={e => update('host', e.target.value)} /></label>
      <label>端口<input type="number" value={value.port} onChange={e => update('port', Number(e.target.value))} /></label>
      <label className="full">分组<GroupCascader groups={availableGroups} value={value.groupId} rootLabel="未分组"
        onChange={groupId => update('groupId', groupId || undefined)}
        allowAdd onAddGroup={parentId => setGroupEditorParentId(parentId)} /></label>
      <div className="full connection-tags"><span>标签</span><div>
        {tags.map(tag => <button key={tag.id} type="button"
          className={value.tags.includes(tag.id) ? 'selected' : ''}
          onClick={() => update('tags', value.tags.includes(tag.id)
            ? value.tags.filter(id => id !== tag.id) : [...value.tags, tag.id])}>
          <i style={{ background: tag.color }} />{tag.name}
        </button>)}
        {!tags.length && <small>可先在左侧创建标签</small>}
      </div></div>
      <label className="full">用户名<input value={value.username} onChange={e => update('username', e.target.value)} /></label>
      <label className="full">认证方式<select value={value.authentication}
        onChange={e => update('authentication', e.target.value as Connection['authentication'])}>
        <option value="password">密码</option><option value="private_key">私钥</option>
        <option value="agent">SSH Agent</option><option value="interactive">键盘交互</option>
      </select></label>
      {value.authentication === 'password' && <label className="full">密码
        <input type="password" value={secret} placeholder={value.credentialId ? '留空则保持不变' : ''}
          onChange={e => setSecret(e.target.value)} /></label>}
      {value.authentication === 'private_key' && <>
        <label className="full">私钥 PEM<textarea rows={6} value={secret}
          placeholder={value.credentialId ? '留空则保持不变' : '-----BEGIN OPENSSH PRIVATE KEY-----'}
          onChange={e => setSecret(e.target.value)} /></label>
        <label className="full">私钥密码（可选）<input type="password" value={passphrase}
          onChange={e => setPassphrase(e.target.value)} /></label>
      </>}
      <label>KeepAlive（秒）<input type="number" value={value.keepAliveSeconds}
        onChange={e => update('keepAliveSeconds', Number(e.target.value))} /></label>
      <label>连接超时（秒）<input type="number" value={value.connectTimeoutSeconds}
        onChange={e => update('connectTimeoutSeconds', Number(e.target.value))} /></label>
      <label className="full">终端编码<select value={value.encoding}
        onChange={e => update('encoding', e.target.value)}>
        <option value="utf-8">UTF-8</option>
        <option value="gbk">GBK</option>
        <option value="big5">Big5</option>
        <option value="shift_jis">Shift-JIS</option>
        <option value="euc-kr">EUC-KR</option>
      </select></label>
      <label className="check full"><input type="checkbox" checked={value.commandHistory}
        onChange={e => update('commandHistory', e.target.checked)} />记录并提示此连接的历史命令</label>
      <label className="full">密码/私钥同步策略<select
        value={value.syncSecrets === undefined ? 'default' : value.syncSecrets ? 'yes' : 'no'}
        onChange={event => setValue(current => {
          const next = { ...current }
          if (event.target.value === 'default') delete next.syncSecrets
          else next.syncSecrets = event.target.value === 'yes'
          return next
        })}>
        <option value="default">跟随全局设置</option>
        <option value="yes">始终同步此连接的密码或私钥</option>
        <option value="no">永不同步此连接的密码或私钥</option>
      </select></label>
      <label className="check full"><input type="checkbox" checked={value.legacyAlgorithms}
        onChange={e => {
          const enabled = e.target.checked
          if (enabled && !window.confirm(
            '旧版兼容模式会允许已不推荐的 SSH 算法，仅应在无法升级的可信旧服务器上单独开启。确定启用吗？'
          )) return
          update('legacyAlgorithms', enabled)
        }} />允许旧版弱算法（不推荐，仅当前连接生效）</label>
    </div>
    {error && <div className="form-error">{error}</div>}
    </Modal>
    {groupEditorParentId !== undefined && <GroupEditor groups={availableGroups}
      initialParentId={groupEditorParentId}
      onClose={() => setGroupEditorParentId(undefined)}
      onSaved={async group => {
        const nextGroups = await api.ListGroups()
        syncGroups(nextGroups)
        update('groupId', group.id)
        setGroupEditorParentId(undefined)
      }} />}
  </>
}

function GroupEditor({ groups, initialParentId = '', onClose, onSaved }: {
  groups: Group[]; initialParentId?: string; onClose: () => void; onSaved: (group: Group) => Promise<void> | void
}) {
  const [name, setName] = useState('')
  const [parentId, setParentId] = useState(initialParentId)
  const [error, setError] = useState('')
  const create = async () => {
    try {
      const result = await api.SaveGroup({
        id: '', name, parentId, sortOrder: nextGroupSortOrder(groups, parentId)
      })
      await onSaved(result)
    } catch (reason) { setError(localizeError(reason)) }
  }
  return <Modal title="新建分组" onClose={onClose} footer={<>
    <button onClick={onClose}>取消</button>
    <button className="primary" onClick={() => void create()}>创建</button>
  </>}>
    <div className="form-grid">
      <label className="full">名称<input autoFocus value={name} onChange={e => setName(e.target.value)} /></label>
      <label className="full">上级分组<GroupCascader groups={groups} value={parentId}
        onChange={setParentId} rootLabel="无（顶级分组）" /></label>
    </div>
    {error && <div className="form-error">{error}</div>}
  </Modal>
}

function TagEditor({ onClose, onSaved }: {
  onClose: () => void; onSaved: () => Promise<void>
}) {
  const [name, setName] = useState('')
  const [color, setColor] = useState('#62d9ca')
  return <Modal title="新建标签" onClose={onClose} footer={<>
    <button onClick={onClose}>取消</button>
    <button className="primary" onClick={() => void api.SaveTag({
      id: '', name, color
    }).then(onSaved)}>创建</button>
  </>}>
    <div className="form-grid">
      <label className="wide">名称<input autoFocus value={name} onChange={e => setName(e.target.value)} /></label>
      <label>颜色<input type="color" value={color} onChange={e => setColor(e.target.value)} /></label>
    </div>
  </Modal>
}

function SettingsDialog({ value, vault, syncSummary, syncBusy, onSyncBusyChange, onClose, onSaved, onReload }: {
  value: Settings; vault: Bootstrap['vault']; syncSummary?: SyncSummary
  syncBusy: boolean; onSyncBusyChange: (value: boolean) => void
  onClose: () => void; onSaved: () => Promise<void>; onReload: () => Promise<void>
}) {
  const [next, setNext] = useState<Settings>(() => ({
    ...value,
    terminalThemeColors: cloneTerminalThemeColors(resolveTerminalThemeColors(value)),
  }))
  const [autoSyncEnabled, setAutoSyncEnabled] = useState(syncSummary?.autoSyncEnabled ?? true)
  const [lockPassword, setLockPassword] = useState('')
  const [quickUnlock, setQuickUnlock] = useState(vault.quickUnlock)
  const [notice, setNotice] = useState<{ title: string; message: string }>()
  const [oldMasterPassword, setOldMasterPassword] = useState('')
  const [newMasterPassword, setNewMasterPassword] = useState('')
  const [sensitiveRules, setSensitiveRules] = useState(value.sensitiveCommandRules.join('\n'))
  const [activeSection, setActiveSection] = useState<'appearance' | 'terminal' | 'security' | 'sync'>('appearance')
  const [recoveryCodeModal, setRecoveryCodeModal] = useState<{ title: string; code: string }>()
  const [joinPassword, setJoinPassword] = useState('')
  const [joinTotpCode, setJoinTotpCode] = useState('')
  const [syncRecoveryCode, setSyncRecoveryCode] = useState('')
  const [pairing, setPairing] = useState<{
    pairingId: string; deviceId: string; shortCode: string; approvalCode: string; expiresAt: string
  }>()
  const [approveCode, setApproveCode] = useState('')
  const [approveShortCode, setApproveShortCode] = useState('')
  const [showJoinModal, setShowJoinModal] = useState(false)
  const [showApproveModal, setShowApproveModal] = useState(false)
  const [showRotateRecoveryModal, setShowRotateRecoveryModal] = useState(false)
  const [rotatePassword, setRotatePassword] = useState('')
  const [rotateTotpCode, setRotateTotpCode] = useState('')
  const [showLeaveSyncModal, setShowLeaveSyncModal] = useState(false)
  const [leavePassword, setLeavePassword] = useState('')
  const [leaveTotpCode, setLeaveTotpCode] = useState('')
  const [resetPassword, setResetPassword] = useState('')
  const [resetTotpCode, setResetTotpCode] = useState('')
  const [resetConfirmText, setResetConfirmText] = useState('')
  const [showResetForm, setShowResetForm] = useState(false)

  useEffect(() => {
    setAutoSyncEnabled(syncSummary?.autoSyncEnabled ?? true)
  }, [syncSummary?.autoSyncEnabled])

  useEffect(() => {
    setNext({
      ...value,
      terminalThemeColors: cloneTerminalThemeColors(resolveTerminalThemeColors(value)),
    })
    setSensitiveRules(value.sensitiveCommandRules.join('\n'))
  }, [value])

  const sections = [
    { id: 'appearance', label: '外观', icon: Paintbrush },
    { id: 'terminal', label: '终端', icon: Monitor },
    { id: 'security', label: '安全', icon: Shield },
    { id: 'sync', label: '同步', icon: SlidersHorizontal }
  ] as const

  const showNotice = (title: string, message: string) => setNotice({ title, message })
  const terminalPreviewColors = resolveTerminalThemeColors(next)

  const updateTerminalPreset = (presetId: string) => {
    const preset = TERMINAL_THEME_PRESETS.find(item => item.id === presetId)
    if (!preset) return
    setNext(current => ({
      ...current,
      terminalThemePreset: presetId,
      terminalThemeColors: cloneTerminalThemeColors(preset.colors),
    }))
  }

  const updateTerminalColor = (key: TerminalThemeField, value: string) => {
    const normalized = normalizeHexColorInput(value)
    setNext(current => ({
      ...current,
      terminalThemePreset: 'custom',
      terminalThemeColors: {
        ...current.terminalThemeColors,
        [key]: normalized ?? resolveTerminalThemeColors(current)[key],
      },
    }))
  }

  const editTerminalColorInput = (key: TerminalThemeField, value: string) => {
    setNext(current => ({
      ...current,
      terminalThemePreset: 'custom',
      terminalThemeColors: {
        ...current.terminalThemeColors,
        [key]: value,
      },
    }))
  }

  const toggleSystemUnlock = async (enabled: boolean) => {
    try {
      if (enabled) await api.EnableSystemUnlock()
      else await api.DisableSystemUnlock()
      setQuickUnlock(enabled)
      showNotice('系统快速解锁', enabled ? '系统快速解锁已启用。' : '系统快速解锁已关闭。')
    } catch (error) {
      showNotice('系统快速解锁', localizeError(error))
    }
  }

  const persist = async () => {
    try {
      if (lockPassword) await api.SetLockPassword(lockPassword)
      if (oldMasterPassword || newMasterPassword) {
        await api.ChangeMasterPassword(oldMasterPassword, newMasterPassword)
      }
      const terminalThemeColors = resolveTerminalThemeColors(next)
      await api.SaveSettings({
        ...next,
        terminalThemeColors,
        sensitiveCommandRules: sensitiveRules.split('\n').map(rule => rule.trim()).filter(Boolean)
      })
      await onSaved()
    } catch (error) {
      showNotice('保存设置失败', localizeError(error))
    }
  }

  const syncNow = async () => {
    onSyncBusyChange(true)
    try {
      const result = await api.SyncNow(next.syncSecretsByDefault, next.syncCommandHistory)
      showNotice('同步完成', `上传 ${result.pushed}，下载 ${result.pulled}，冲突 ${result.conflicts}。`)
      await onReload()
    } catch (error) {
      showNotice('同步失败', localizeError(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const setAutoSync = async (enabled: boolean) => {
    setAutoSyncEnabled(enabled)
    try {
      await api.SetSyncAutoEnabled(enabled)
      await onReload()
    } catch (error) {
      showNotice('自动同步', localizeError(error))
      setAutoSyncEnabled(syncSummary?.autoSyncEnabled ?? true)
    }
  }

  const initializeSync = async () => {
    onSyncBusyChange(true)
    try {
      const result = await api.InitializeSync(syncSummary?.deviceName ?? '')
      setRecoveryCodeModal({
        title: '同步初始化完成',
        code: result.recoveryCode
      })
      await onReload()
    } catch (error) {
      showNotice('同步初始化失败', localizeError(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const recoverSync = async () => {
    if (!syncSummary?.serverUrl || !syncSummary?.username) {
      showNotice('设备加入', '请先登录服务端账号。')
      return
    }
    onSyncBusyChange(true)
    try {
      await api.RecoverSync(
        syncSummary.serverUrl,
        syncSummary.username,
        joinPassword,
        joinTotpCode.trim(),
        syncSummary.deviceName ?? '',
        syncRecoveryCode.trim()
      )
      setSyncRecoveryCode('')
      setJoinPassword('')
      setJoinTotpCode('')
      setShowJoinModal(false)
      setPairing(undefined)
      showNotice('设备加入', '本机已通过恢复码加入同步保险库。')
      await onReload()
    } catch (error) {
      showNotice('设备加入', localizeError(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const beginPairing = async () => {
    if (!syncSummary?.serverUrl) {
      showNotice('设备加入', '请先登录服务端账号。')
      return
    }
    onSyncBusyChange(true)
    try {
      setPairing(await api.BeginDevicePairing(syncSummary.serverUrl, syncSummary.deviceName ?? ''))
      setShowJoinModal(true)
      showNotice('设备加入', '请在已加入同步的设备上输入批准串并核对短码。')
    } catch (error) {
      showNotice('设备加入', localizeError(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const claimPairing = async () => {
    if (!syncSummary?.username) {
      showNotice('设备加入', '请先登录服务端账号。')
      return
    }
    onSyncBusyChange(true)
    try {
      const result = await api.ClaimDevicePairing(
        syncSummary.username,
        joinPassword,
        joinTotpCode.trim()
      )
      if (!result.approved) {
        showNotice('设备加入', '设备加入请求还未批准。')
        return
      }
      setPairing(undefined)
      setJoinPassword('')
      setJoinTotpCode('')
      setShowJoinModal(false)
      showNotice('设备加入', '本机已通过已授权设备批准加入同步。')
      await onReload()
    } catch (error) {
      showNotice('设备加入', localizeError(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const approvePairing = async () => {
    let parsedShortCode = ''
    try {
      parsedShortCode = JSON.parse(decodeBase64Url(approveCode)).shortCode ?? ''
    } catch {
      parsedShortCode = ''
    }
    if (!parsedShortCode || parsedShortCode !== approveShortCode) {
      showNotice('批准新设备', '手动输入的短码与批准串中的短码不一致。')
      return
    }
    onSyncBusyChange(true)
    try {
      await api.ApproveDevicePairing(approveCode.trim())
      setApproveCode('')
      setApproveShortCode('')
      setShowApproveModal(false)
      showNotice('批准新设备', '已批准新的设备加入请求。')
    } catch (error) {
      showNotice('批准新设备', localizeError(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const rotateRecoveryCode = async () => {
    onSyncBusyChange(true)
    try {
      const code = await api.RotateSyncRecoveryCode(rotatePassword, rotateTotpCode.trim())
      setRotatePassword('')
      setRotateTotpCode('')
      setShowRotateRecoveryModal(false)
      setRecoveryCodeModal({
        title: '同步恢复码已刷新',
        code
      })
      await onReload()
    } catch (error) {
      showNotice('刷新同步恢复码失败', localizeError(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const leaveSync = async () => {
    if (!window.confirm('这会让本机退出同步保险库，并撤销当前同步设备。确认继续？')) return
    onSyncBusyChange(true)
    try {
      await api.LeaveSync(leavePassword, leaveTotpCode.trim())
      setLeavePassword('')
      setLeaveTotpCode('')
      setPairing(undefined)
      setShowLeaveSyncModal(false)
      showNotice('已退出同步保险库', '本机已恢复到未加入同步保险库的状态。')
      await onReload()
    } catch (error) {
      showNotice('退出同步失败', localizeError(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const resetSync = async () => {
    if (!window.confirm('这会清空服务端同步保险库、设备和同步记录。确认继续？')) return
    onSyncBusyChange(true)
    try {
      await api.ResetSync(resetPassword, resetTotpCode.trim())
      setShowResetForm(false)
      setResetPassword('')
      setResetTotpCode('')
      setResetConfirmText('')
      setRecoveryCodeModal(undefined)
      setSyncRecoveryCode('')
      setJoinPassword('')
      setJoinTotpCode('')
      setPairing(undefined)
      showNotice('同步保险库已重置', '本机已退出同步配置，可重新初始化或使用恢复码加入。')
      await onReload()
    } catch (error) {
      showNotice('重置同步失败', localizeError(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const loggedIn = !!syncSummary?.loggedIn
  const syncInitialized = !!syncSummary?.syncInitialized
  const syncConfigured = !!syncSummary?.configured
  const showSyncHistory = syncInitialized && syncConfigured

  let content
  if (activeSection === 'appearance') {
    content = <div className="settings-page">
      <h3>外观</h3>
      <div className="form-grid">
        <label className="full">主题
          <div className="theme-picker">
            <button type="button" className={next.theme === 'dark' ? 'selected' : ''}
              onClick={() => setNext({ ...next, theme: 'dark' })}><Moon size={16} />深色</button>
            <button type="button" className={next.theme === 'light' ? 'selected' : ''}
              onClick={() => setNext({ ...next, theme: 'light' })}><Sun size={16} />浅色</button>
          </div>
        </label>
        <label>终端字号<input type="number" value={next.fontSize}
          onChange={e => setNext({ ...next, fontSize: Number(e.target.value) })} /></label>
        <label className="full">终端字体<input value={next.fontFamily}
          onChange={e => setNext({ ...next, fontFamily: e.target.value })} /></label>
      </div>
      <div className="settings-divider" />
      <div className="terminal-theme-section">
        <div className="terminal-theme-header">
          <div>
            <strong>终端配色</strong>
            <span>应用主题和终端配色彼此独立，方便单独调整。</span>
          </div>
          <button className="secondary" type="button" onClick={() => updateTerminalPreset('default')}>
            恢复默认
          </button>
        </div>
        <div className="terminal-theme-presets">
          {TERMINAL_THEME_PRESETS.map(preset => <button
            key={preset.id}
            type="button"
            className={next.terminalThemePreset === preset.id ? 'selected' : ''}
            onClick={() => updateTerminalPreset(preset.id)}>
            <div className="terminal-theme-preset-copy">
              <strong>{preset.label}</strong>
              <span>{preset.description}</span>
            </div>
            <div className="terminal-theme-swatches" aria-hidden="true">
              {[preset.colors.background, preset.colors.blue, preset.colors.green, preset.colors.magenta, preset.colors.yellow].map(color =>
                <i key={`${preset.id}:${color}`} style={{ background: color }} />
              )}
            </div>
          </button>)}
          <button
            type="button"
            className={next.terminalThemePreset === 'custom' ? 'selected custom' : 'custom'}
            onClick={() => setNext(current => ({ ...current, terminalThemePreset: 'custom' }))}>
            <div className="terminal-theme-preset-copy">
              <strong>自定义</strong>
              <span>按界面元素和 ANSI 颜色逐项编辑。</span>
            </div>
          </button>
        </div>
        <div
          className="terminal-theme-preview"
          style={terminalChromeVariables(terminalPreviewColors) as CSSProperties}>
          <div className="terminal-theme-preview-toolbar">
            <span />
            <span />
            <span />
          </div>
          <div className="terminal-theme-preview-body">
            <code><span style={{ color: terminalPreviewColors.green }}>$</span> ssh root@example.com</code>
            <code><span style={{ color: terminalPreviewColors.blue }}>root@example.com</span>:<span style={{ color: terminalPreviewColors.yellow }}>~</span>$ ls</code>
            <code>
              <span style={{ color: terminalPreviewColors.blue }}>logs</span>{' '}
              <span style={{ color: terminalPreviewColors.magenta }}>configs</span>{' '}
              <span style={{ color: terminalPreviewColors.cyan }}>deploy.sh</span>
            </code>
            <code><span style={{ color: terminalPreviewColors.brightBlack }}>tail -f /var/log/app.log</span></code>
          </div>
        </div>
        <div className="terminal-theme-groups">
          {TERMINAL_THEME_GROUPS.map(group => <section key={group.title} className="terminal-theme-group">
            <header>{group.title}</header>
            <div className="terminal-theme-color-grid">
              {group.fields.map(field => <label key={field.key} className="terminal-color-field">
                <span>{field.label}</span>
                <div>
                  <input
                    type="color"
                    value={terminalPreviewColors[field.key]}
                    onChange={event => updateTerminalColor(field.key, event.target.value)}
                  />
                  <input
                    value={next.terminalThemeColors[field.key]}
                    onChange={event => editTerminalColorInput(field.key, event.target.value)}
                    onBlur={event => updateTerminalColor(field.key, event.target.value)}
                    placeholder="#RRGGBB"
                  />
                </div>
              </label>)}
            </div>
          </section>)}
        </div>
      </div>
    </div>
  } else if (activeSection === 'terminal') {
    content = <div className="settings-page">
      <h3>终端</h3>
      <small className="hint">这里保留连接与会话行为设置；配色与字体放在“外观”里。</small>
      <div className="form-grid">
        <label>自动锁屏（分钟）<input type="number" value={next.lockAfterMinutes}
          onChange={e => setNext({ ...next, lockAfterMinutes: Number(e.target.value) })} /></label>
        <label className="check full"><input type="checkbox" checked={next.disconnectOnLock}
          onChange={e => setNext({ ...next, disconnectOnLock: e.target.checked })} />锁屏时断开 SSH 会话</label>
      </div>
    </div>
  } else if (activeSection === 'security') {
    content = <div className="settings-page">
      <h3>安全</h3>
      <div className="form-grid">
        <label className="check full"><input type="checkbox" checked={quickUnlock}
          onChange={e => void toggleSystemUnlock(e.target.checked)} />系统快速解锁</label>
        <small className="hint full">
          {quickUnlock
            ? `${vault.quickUnlockMethod} 快速解锁已启用，解锁时需要操作系统用户验证。`
            : `${vault.quickUnlockMethod} 快速解锁未启用。`}
        </small>
        <label className="full">独立锁屏密码（可选）
          <input type="password" value={lockPassword} placeholder="至少 8 个字符；留空不修改"
            onChange={event => setLockPassword(event.target.value)} /></label>
        {vault.customLockPassword && <button className="secondary full" type="button"
          onClick={() => void api.ClearLockPassword()
            .then(() => showNotice('独立锁屏密码', '独立锁屏密码已清除。'))
            .catch(error => showNotice('独立锁屏密码', localizeError(error)))}>
          清除独立锁屏密码
        </button>}
        <label className="wide">当前主密码<input type="password" value={oldMasterPassword}
          onChange={event => setOldMasterPassword(event.target.value)} /></label>
        <label>新主密码<input type="password" value={newMasterPassword}
          placeholder="至少 12 个字符"
          onChange={event => setNewMasterPassword(event.target.value)} /></label>
        <label className="full">敏感命令过滤规则（每行一个正则）
          <textarea rows={4} value={sensitiveRules}
            onChange={event => setSensitiveRules(event.target.value)} />
        </label>
      </div>
    </div>
  } else {
    content = <div className="settings-page">
      <h3>同步</h3>
      <div className="sync-summary">
        <div>
          <strong>{syncHeadline(syncSummary)}</strong>
          <span>{syncSummaryLabel(syncSummary)}</span>
        </div>
        <div>
          <strong>{syncSummary?.running ? '同步中' : syncConfigured && syncSummary?.autoSyncEnabled === false ? '自动同步已关闭' : '同步待命'}</strong>
          <span>{showSyncHistory && syncSummary?.lastSyncedAt ? `上次同步：${formatDateTime(syncSummary.lastSyncedAt)}` : '还没有同步记录'}</span>
        </div>
        <div>
          <strong>{syncSummary?.lastError ? '最近失败' : '最近结果'}</strong>
          <span>{showSyncHistory
            ? syncSummary?.lastError ?? (syncSummary?.lastAttemptAt ? `上次尝试：${formatDateTime(syncSummary.lastAttemptAt)}` : '暂无')
            : '暂无'}</span>
        </div>
      </div>
      {!loggedIn && <div className="pairing-approval">
        <small className="hint full">登录后才能查看远端同步状态、初始化同步保险库或加入现有同步。</small>
      </div>}
      {loggedIn && !syncInitialized && <div className="pairing-approval">
        <small className="hint full">当前账号在服务端还没有同步保险库。初始化后会生成恢复码，并将本机加入同步。</small>
        <div className="sync-action-grid single">
          <button className="secondary" type="button" disabled={syncBusy}
            onClick={() => void initializeSync()}>{syncBusy ? '处理中…' : '初始化同步保险库'}</button>
        </div>
      </div>}
      {loggedIn && syncInitialized && !syncConfigured && <div className="pairing-approval">
        <small className="hint full">服务端已有同步保险库，但本机还没加入。可以使用恢复码直接加入，或请求一台已加入的设备批准。</small>
        <div className="sync-action-grid">
          <button className="secondary" type="button" disabled={syncBusy}
            onClick={() => setShowJoinModal(true)}>用恢复码加入</button>
          <button className="secondary" type="button" disabled={syncBusy}
            onClick={() => void beginPairing()}>{syncBusy ? '处理中…' : '请求已有设备批准'}</button>
        </div>
      </div>}
      {syncConfigured && <div className="form-grid">
        <label className="check full"><input type="checkbox" checked={next.syncCommandHistory}
          onChange={e => setNext({ ...next, syncCommandHistory: e.target.checked })} />允许同步命令历史</label>
        <label className="check full"><input type="checkbox" checked={next.syncSecretsByDefault}
          onChange={e => setNext({ ...next, syncSecretsByDefault: e.target.checked })} />默认同步密码和私钥</label>
        <label className="check full"><input type="checkbox" checked={autoSyncEnabled}
          onChange={e => void setAutoSync(e.target.checked)} />自动同步</label>
        <div className="sync-action-grid sync-action-grid-configured full">
          <button className="secondary sync-action-button" type="button" disabled={syncBusy}
            onClick={() => void syncNow()}>{syncBusy ? '同步中…' : '立即同步'}</button>
          <button className="secondary sync-action-button" type="button" disabled={syncBusy}
            onClick={() => setShowRotateRecoveryModal(true)}>刷新同步恢复码</button>
          <button className="secondary sync-action-button" type="button" disabled={syncBusy}
            onClick={() => setShowApproveModal(true)}>批准新设备加入</button>
          <button className="danger-button sync-action-button" type="button" disabled={syncBusy}
            onClick={() => setShowLeaveSyncModal(true)}>退出同步保险库</button>
        </div>
      </div>}
      {syncInitialized && <details className="pairing-approval" open={showResetForm}>
        <summary onClick={() => setShowResetForm(value => !value)}>重置同步保险库</summary>
        <small className="hint full">这会清空服务端同步保险库、设备列表、同步记录和待批准加入请求。</small>
        <label>确认短语<input value={resetConfirmText} onChange={e => setResetConfirmText(e.target.value)}
          placeholder="输入 RESET SYNC" /></label>
        <label>账号密码<input type="password" value={resetPassword} onChange={e => setResetPassword(e.target.value)} /></label>
        <label>TOTP（如已开启）<input value={resetTotpCode}
          onChange={e => setResetTotpCode(e.target.value.trim())} /></label>
        <button className="danger-button wide" type="button"
          disabled={syncBusy || !resetPassword || resetConfirmText.trim() !== 'RESET SYNC'}
          onClick={() => void resetSync()}>{syncBusy ? '处理中…' : '重置同步保险库'}</button>
      </details>}
    </div>
  }

  return <Modal title="设置" onClose={onClose} width="920px" bodyClassName="settings-modal-body" footer={<>
    <button onClick={onClose}>取消</button>
    <button className="primary" onClick={() => void persist()}>保存</button>
  </>}>
    <div className="settings-layout">
      <aside className="settings-nav">
        {sections.map(section => {
          const Icon = section.icon
          return <button key={section.id} type="button"
            className={activeSection === section.id ? 'active' : ''}
            onClick={() => setActiveSection(section.id)}>
            <Icon size={16} /><span>{section.label}</span>
          </button>
        })}
      </aside>
      <section className="settings-content">
        {content}
      </section>
    </div>
    {notice && <NoticeDialog title={notice.title} message={notice.message} onClose={() => setNotice(undefined)} />}
    {recoveryCodeModal && <RecoveryCodeDialog title={recoveryCodeModal.title} code={recoveryCodeModal.code}
      onClose={() => setRecoveryCodeModal(undefined)} />}
    {showJoinModal && <JoinSyncDialog
      syncBusy={syncBusy}
      joinPassword={joinPassword}
      joinTotpCode={joinTotpCode}
      syncRecoveryCode={syncRecoveryCode}
      pairing={pairing}
      onJoinPasswordChange={setJoinPassword}
      onJoinTotpCodeChange={setJoinTotpCode}
      onRecoveryCodeChange={setSyncRecoveryCode}
      onClose={() => setShowJoinModal(false)}
      onRecover={() => void recoverSync()}
      onClaim={() => void claimPairing()}
      onCopyApprovalCode={async () => {
        if (!pairing?.approvalCode) return
        await navigator.clipboard?.writeText(pairing.approvalCode)
      }}
    />}
    {showApproveModal && <ApprovePairingDialog
      syncBusy={syncBusy}
      approvalCode={approveCode}
      shortCode={approveShortCode}
      onApprovalCodeChange={setApproveCode}
      onShortCodeChange={setApproveShortCode}
      onClose={() => setShowApproveModal(false)}
      onApprove={() => void approvePairing()}
    />}
    {showRotateRecoveryModal && <RotateRecoveryDialog
      syncBusy={syncBusy}
      password={rotatePassword}
      totpCode={rotateTotpCode}
      onPasswordChange={setRotatePassword}
      onTotpCodeChange={setRotateTotpCode}
      onClose={() => setShowRotateRecoveryModal(false)}
      onRotate={() => void rotateRecoveryCode()}
    />}
    {showLeaveSyncModal && <LeaveSyncDialog
      syncBusy={syncBusy}
      password={leavePassword}
      totpCode={leaveTotpCode}
      onPasswordChange={setLeavePassword}
      onTotpCodeChange={setLeaveTotpCode}
      onClose={() => setShowLeaveSyncModal(false)}
      onLeave={() => void leaveSync()}
    />}
  </Modal>
}

function AccountManagerDialog({ account, onClose, onReload }: {
  account?: AccountSummary; onClose: () => void; onReload: () => Promise<void>
}) {
  const [serverUrl, setServerUrl] = useState(account?.serverUrl ?? 'https://')
  const [username, setUsername] = useState(account?.username ?? '')
  const [password, setPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [notice, setNotice] = useState<{ title: string; message: string }>()
  const [loggedIn, setLoggedIn] = useState(account?.loggedIn ?? false)
  const [deviceId, setDeviceId] = useState(account?.deviceId ?? '')
  const [deviceName, setDeviceName] = useState(account?.deviceName ?? '')
  const [accessExpiresAt, setAccessExpiresAt] = useState(account?.accessExpiresAt ?? '')
  const [refreshExpiresAt, setRefreshExpiresAt] = useState(account?.refreshExpiresAt ?? '')
  const [devices, setDevices] = useState<Array<{
    id: string; name: string; approved: boolean; revoked: boolean
    createdAt: string; lastSeenAt: string
  }>>([])
  const [totpSetup, setTotpSetup] = useState<{ secret: string; setupToken: string; uri: string }>()
  const [accountRecoveryCodes, setAccountRecoveryCodes] = useState<string[]>([])
  const [totpEnabled, setTotpEnabled] = useState(account?.totpEnabled ?? false)
  const syncInitialized = !!account?.syncInitialized
  useEffect(() => {
    if (account?.serverUrl) setServerUrl(account.serverUrl)
    if (account?.username) setUsername(account.username)
    setLoggedIn(account?.loggedIn ?? false)
    setDeviceId(account?.deviceId ?? '')
    setDeviceName(account?.deviceName ?? '')
    setAccessExpiresAt(account?.accessExpiresAt ?? '')
    setRefreshExpiresAt(account?.refreshExpiresAt ?? '')
    setTotpEnabled(account?.totpEnabled ?? false)
  }, [account?.serverUrl, account?.username, account?.loggedIn, account?.deviceId, account?.deviceName, account?.accessExpiresAt, account?.refreshExpiresAt, account?.totpEnabled])
  useEffect(() => {
    if (!loggedIn || !syncInitialized) {
      setDevices([])
      return
    }
    void loadDevices()
  }, [loggedIn, syncInitialized])
  const showNotice = (title: string, message: string) => setNotice({ title, message })
  const login = async () => {
    try {
      const resolvedDeviceId = deviceId || crypto.randomUUID()
      await api.LoginAccount(
        serverUrl, username, password, resolvedDeviceId, totpCode.trim()
      )
      showNotice('账号管理', '已登录服务端。')
      setLoggedIn(true)
      setDeviceId(resolvedDeviceId)
      setDeviceName(account?.deviceName ?? '')
      await onReload()
    } catch (error) { showNotice('账号管理', localizeError(error)) }
  }
  const logout = async () => {
    try {
      await api.LogoutAccount()
      showNotice('账号管理', '已退出登录。')
      setLoggedIn(false)
      setTotpSetup(undefined)
      setAccountRecoveryCodes([])
      await onReload()
    } catch (error) { showNotice('账号管理', localizeError(error)) }
  }
  const loadDevices = async () => {
    try {
      const values = await api.ListSyncDevices()
      setDevices(Array.isArray(values) ? values : [])
    } catch (error) { showNotice('设备管理', localizeError(error)) }
  }
  const beginTOTP = async () => {
    try {
      setTotpSetup(await api.BeginSyncTOTPSetup())
      showNotice('TOTP 设置', '请把密钥添加到验证器，再输入六位验证码确认。')
    } catch (error) { showNotice('TOTP 设置', localizeError(error)) }
  }
  const confirmTOTP = async () => {
    if (!totpSetup) return
    try {
      setAccountRecoveryCodes(await api.ConfirmSyncTOTPSetup(totpSetup.setupToken, totpCode))
      setTotpEnabled(true)
      setTotpSetup(undefined)
      setTotpCode('')
      showNotice('TOTP 设置', 'TOTP 已启用。请离线保存账号恢复码。')
    } catch (error) { showNotice('TOTP 设置', localizeError(error)) }
  }
  const disableTOTP = async () => {
    try {
      await api.DisableSyncTOTP(password, totpCode.trim())
      setTotpEnabled(false)
      setTotpSetup(undefined)
      setAccountRecoveryCodes([])
      setTotpCode('')
      showNotice('TOTP 设置', 'TOTP 已关闭。')
      await onReload()
    } catch (error) { showNotice('TOTP 设置', localizeError(error)) }
  }
  const saveDeviceName = async () => {
    try {
      await api.SetDeviceName(deviceName.trim())
      showNotice('设备名称', '当前设备名称已更新。')
      await onReload()
    } catch (error) { showNotice('设备名称', localizeError(error)) }
  }
  if (!loggedIn) {
    return <Modal title="账号管理" onClose={onClose} width="720px">
      <div className="sync-summary">
        <div>
          <strong>未登录</strong>
          <span>{serverUrl ? `${serverUrl} · ${username || '未填写账号'}` : '同步信息尚未初始化'}</span>
        </div>
      </div>
      <div className="form-grid">
        <label className="wide">服务端地址<input value={serverUrl} onChange={e => setServerUrl(e.target.value)} /></label>
        <label className="wide">账号<input value={username} onChange={e => setUsername(e.target.value)} /></label>
        <label className="wide">密码<input type="password" value={password} onChange={e => setPassword(e.target.value)} /></label>
        <label className="wide">TOTP / 恢复码<input value={totpCode}
          onChange={e => setTotpCode(e.target.value.trim())} /></label>
      </div>
      <small className="hint full">启用 TOTP 后，这里填写 6 位验证码；没有验证器时，可改填一条恢复码。</small>
      <footer className="modal-actions">
        <button onClick={onClose}>关闭</button>
        <button className="secondary" onClick={() => void login()}>登录</button>
      </footer>
      {notice && <NoticeDialog title={notice.title} message={notice.message} onClose={() => setNotice(undefined)} />}
    </Modal>
  }
  return <Modal title="账号管理" onClose={onClose} width="920px">
    <section className="account-panel">
    <header className="account-panel-header">
      <div>
        <strong>账号管理</strong>
        <span>{serverUrl ? `${serverUrl} · ${username}` : '同步信息尚未初始化'}</span>
      </div>
      <div className="account-panel-actions">
        {syncInitialized && <button className="secondary" onClick={() => void loadDevices()}>刷新设备</button>}
        <button className="danger-button" onClick={() => void logout()}>退出登录</button>
      </div>
    </header>
    <div className="account-panel-body">
      <div className="sync-summary">
        <div>
          <strong>{syncInitialized && deviceId ? '当前同步设备' : '当前设备'}</strong>
          <span>{syncInitialized ? (deviceName || deviceId || '设备编号会在首次登录时生成并保存。') : '服务端同步保险库未初始化时，本机不会显示旧的同步设备信息。'}</span>
        </div>
        <div>
          <strong>{account?.syncInitialized ? '远端同步已初始化' : '远端同步未初始化'}</strong>
          <span>{account?.syncInitialized ? '服务端已经存在同步保险库。' : '服务端还没有同步保险库。'}</span>
        </div>
        <div>
          <strong>{accessExpiresAt ? '访问令牌已存' : '访问令牌未显示'}</strong>
          <span>{accessExpiresAt ? `访问令牌到期：${new Date(accessExpiresAt).toLocaleString()}` : '密码不会保存在本地。'}</span>
        </div>
      </div>
      {syncInitialized && <div className="pairing-approval">
        <div className="form-grid">
          <label className="wide">当前设备名称<input
            value={deviceName}
            placeholder={deviceId || '未设置时显示设备 ID'}
            onChange={e => setDeviceName(e.target.value)} /></label>
          <div className="field-actions">
            <button className="secondary" onClick={() => void saveDeviceName()}>保存名称</button>
          </div>
        </div>
        <small className="hint full">未设置时，设备列表和同步状态会默认显示设备 ID。</small>
      </div>}
      {syncInitialized && <div className="pairing-approval">
        <div className="device-list">
          {devices.map(device => <div key={device.id}>
            <span><strong>{device.name || device.id}</strong><small>{device.id}</small></span>
            <button onClick={() => {
              if (!window.confirm(`确定撤销设备「${device.name || device.id}」？`)) return
              void api.RevokeSyncDevice(device.id)
                .then(loadDevices).catch(error => showNotice('设备管理', localizeError(error)))
            }}>撤销</button>
          </div>)}
          {!devices.length && <small>暂无设备，或尚未刷新。</small>}
        </div>
      </div>}
      <details className="pairing-approval">
        <summary>账号二次验证</summary>
        {!totpEnabled && !totpSetup && <button className="secondary wide" onClick={() => void beginTOTP()}>启用 TOTP</button>}
        {totpSetup && <>
          <label>验证器密钥<input readOnly value={totpSetup.secret} /></label>
          <label>六位验证码<input inputMode="numeric" value={totpCode}
            onChange={e => setTotpCode(e.target.value.replace(/\D/g, ''))} /></label>
          <button className="secondary wide" onClick={() => void confirmTOTP()}>验证并启用</button>
        </>}
        {totpEnabled && !totpSetup && <>
          <label>账号密码<input type="password" value={password} onChange={e => setPassword(e.target.value)} /></label>
          <label>TOTP 验证码<input inputMode="numeric" value={totpCode}
            onChange={e => setTotpCode(e.target.value.replace(/\D/g, ''))} /></label>
          <button className="secondary wide" disabled={!password || !totpCode.trim()} onClick={() => void disableTOTP()}>
            关闭 TOTP
          </button>
        </>}
        {!!accountRecoveryCodes.length && <label>账号恢复码<textarea readOnly rows={6}
          value={accountRecoveryCodes.join('\n')} /></label>}
        {!!accountRecoveryCodes.length && <small className="hint full">
          每条恢复码只能使用一次，可在登录时填到 “TOTP / 恢复码” 输入框里代替 6 位验证码。
        </small>}
      </details>
    </div>
    {notice && <NoticeDialog title={notice.title} message={notice.message} onClose={() => setNotice(undefined)} />}
    </section>
  </Modal>
}

function NoticeDialog({ title, message, onClose }: {
  title: string; message: string; onClose: () => void
}) {
  const target = modalPortalTarget()
  if (!target) return null
  return createPortal(
    <div className="modal-backdrop notice-backdrop" onMouseDown={event => event.target === event.currentTarget && onClose()}>
      <section className="modal notice-modal">
        <header><h2>{title}</h2><button onClick={onClose}><X size={18} /></button></header>
        <div className="modal-body notice-body">{message}</div>
        <footer className="modal-actions"><button className="primary" onClick={onClose}>确定</button></footer>
      </section>
    </div>,
    target,
  )
}

function RecoveryCodeDialog({ title, code, onClose }: {
  title: string; code: string; onClose: () => void
}) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    await navigator.clipboard?.writeText(code)
    setCopied(true)
  }
  return <Modal title={title} onClose={onClose} width="620px">
    <div className="pairing-approval">
      <label>同步恢复码<textarea readOnly rows={4} value={code} /></label>
      <small className="hint full">请立即离线保存这条恢复码。关闭此窗口后将不会再次显示明文。</small>
    </div>
    <footer className="modal-actions">
      <button onClick={() => void copy()}>{copied ? '已复制' : '复制恢复码'}</button>
      <button className="primary" onClick={onClose}>我已保存</button>
    </footer>
  </Modal>
}

function RenameTabDialog({ value, placeholder, onClose, onSave }: {
  value: string
  placeholder: string
  onClose: () => void
  onSave: (value: string) => void
}) {
  const [next, setNext] = useState(value)
  return <Modal title="重命名标签页" onClose={onClose} width="420px">
    <div className="form-grid rename-tab-form">
      <label className="full">标签名称
        <input
          autoFocus
          value={next}
          placeholder={placeholder}
          onChange={event => setNext(event.target.value)}
          onKeyDown={event => {
            if (event.key === 'Enter') onSave(next)
          }}
        />
      </label>
      <small className="hint full">留空则恢复显示连接名称。</small>
    </div>
    <footer className="modal-actions">
      <button onClick={onClose}>取消</button>
      <button className="primary" onClick={() => onSave(next)}>保存</button>
    </footer>
  </Modal>
}

function JoinSyncDialog({ syncBusy, joinPassword, joinTotpCode, syncRecoveryCode, pairing, onJoinPasswordChange, onJoinTotpCodeChange, onRecoveryCodeChange, onClose, onRecover, onClaim, onCopyApprovalCode }: {
  syncBusy: boolean
  joinPassword: string
  joinTotpCode: string
  syncRecoveryCode: string
  pairing?: { pairingId: string; deviceId: string; shortCode: string; approvalCode: string; expiresAt: string }
  onJoinPasswordChange: (value: string) => void
  onJoinTotpCodeChange: (value: string) => void
  onRecoveryCodeChange: (value: string) => void
  onClose: () => void
  onRecover: () => void
  onClaim: () => void
  onCopyApprovalCode: () => Promise<void>
}) {
  return <Modal title="加入同步保险库" onClose={onClose} width="720px">
    <div className="form-grid">
      <label className="wide">账号密码<input type="password" value={joinPassword} onChange={e => onJoinPasswordChange(e.target.value)} /></label>
      <label>TOTP / 恢复码<input value={joinTotpCode} onChange={e => onJoinTotpCodeChange(e.target.value.trim())} /></label>
      <label className="full">同步恢复码<input value={syncRecoveryCode} onChange={e => onRecoveryCodeChange(e.target.value)} /></label>
    </div>
    <small className="hint full">你可以直接用恢复码加入，或先发起请求，再由已加入同步的设备输入批准串并核对短码。</small>
    <div className="sync-action-grid">
      <button className="secondary" disabled={syncBusy || !joinPassword || !syncRecoveryCode.trim()} onClick={onRecover}>
        {syncBusy ? '处理中…' : '用恢复码加入'}
      </button>
      {!pairing && <button className="secondary" disabled={syncBusy} onClick={onClose}>关闭</button>}
      {!!pairing && <button disabled={syncBusy || !joinPassword} onClick={onClaim}>
        {syncBusy ? '处理中…' : '我已获批，继续加入'}
      </button>}
    </div>
    {!!pairing && <div className="pairing-card textual">
      <div>
        <small>批准短码</small>
        <strong>{pairing.shortCode}</strong>
        <p>请在已加入同步的设备上输入下方批准串，并核对这个短码是否一致。</p>
      </div>
      <div className="pairing-code-block">
        <label>批准串<textarea readOnly rows={7} value={pairing.approvalCode} /></label>
        <div className="sync-action-grid">
          <button className="secondary" onClick={() => void onCopyApprovalCode()}>复制批准串</button>
          <button className="secondary" onClick={onClaim} disabled={syncBusy || !joinPassword}>轮询批准结果</button>
        </div>
      </div>
    </div>}
  </Modal>
}

function ApprovePairingDialog({ syncBusy, approvalCode, shortCode, onApprovalCodeChange, onShortCodeChange, onClose, onApprove }: {
  syncBusy: boolean
  approvalCode: string
  shortCode: string
  onApprovalCodeChange: (value: string) => void
  onShortCodeChange: (value: string) => void
  onClose: () => void
  onApprove: () => void
}) {
  return <Modal title="批准新设备加入" onClose={onClose} width="720px">
    <div className="form-grid">
      <label className="full">批准串<textarea rows={7} value={approvalCode} onChange={e => onApprovalCodeChange(e.target.value)} /></label>
      <label className="full">短码（人工核对）<input value={shortCode} onChange={e => onShortCodeChange(e.target.value.replace(/\D/g, '').slice(0, 6))} /></label>
    </div>
    <small className="hint full">请先和请求加入的设备核对 6 位短码，再批准。短码目前只用于人工确认，不会改变密钥交换内容。</small>
    <footer className="modal-actions">
      <button onClick={onClose}>取消</button>
      <button className="primary" disabled={syncBusy || !approvalCode.trim() || shortCode.length !== 6} onClick={onApprove}>
        {syncBusy ? '处理中…' : '批准加入'}
      </button>
    </footer>
  </Modal>
}

function RotateRecoveryDialog({ syncBusy, password, totpCode, onPasswordChange, onTotpCodeChange, onClose, onRotate }: {
  syncBusy: boolean
  password: string
  totpCode: string
  onPasswordChange: (value: string) => void
  onTotpCodeChange: (value: string) => void
  onClose: () => void
  onRotate: () => void
}) {
  return <Modal title="刷新同步恢复码" onClose={onClose}>
    <div className="form-grid">
      <label className="full">账号密码<input type="password" value={password} onChange={e => onPasswordChange(e.target.value)} /></label>
      <label className="full">TOTP（如已开启）<input value={totpCode} onChange={e => onTotpCodeChange(e.target.value.trim())} /></label>
    </div>
    <small className="hint full">刷新后旧的同步恢复码将失效，新的恢复码只会显示一次。</small>
    <footer className="modal-actions">
      <button onClick={onClose}>取消</button>
      <button className="primary" disabled={syncBusy || !password} onClick={onRotate}>
        {syncBusy ? '处理中…' : '刷新恢复码'}
      </button>
    </footer>
  </Modal>
}

function LeaveSyncDialog({ syncBusy, password, totpCode, onPasswordChange, onTotpCodeChange, onClose, onLeave }: {
  syncBusy: boolean
  password: string
  totpCode: string
  onPasswordChange: (value: string) => void
  onTotpCodeChange: (value: string) => void
  onClose: () => void
  onLeave: () => void
}) {
  return <Modal title="退出同步保险库" onClose={onClose}>
    <div className="form-grid">
      <label className="full">账号密码<input type="password" value={password} onChange={e => onPasswordChange(e.target.value)} /></label>
      <label className="full">TOTP（如已开启）<input value={totpCode} onChange={e => onTotpCodeChange(e.target.value.trim())} /></label>
    </div>
    <small className="hint full">退出后，本机不再持有同步根密钥，并撤销当前同步设备。服务端同步保险库本身不会被删除。</small>
    <footer className="modal-actions">
      <button onClick={onClose}>取消</button>
      <button className="danger-button" disabled={syncBusy || !password} onClick={onLeave}>
        {syncBusy ? '处理中…' : '退出同步保险库'}
      </button>
    </footer>
  </Modal>
}

function HostKeyDialog({ value, onCancel, onAccept }: {
  value: PendingHostKey; onCancel: () => void; onAccept: () => Promise<void>
}) {
  return <Modal title={value.changed ? '主机密钥发生变化' : '确认主机身份'} onClose={onCancel}>
    <div className={`security-notice ${value.changed ? 'danger' : ''}`}>
      <Monitor size={28} /><div>
        <strong>{value.hostPort}</strong>
        <p>{value.changed
          ? '保存的主机密钥与服务器返回值不同。可能是服务器重装，也可能存在中间人攻击。'
          : '这是首次连接。请通过可信渠道核对以下指纹。'}</p>
      </div>
    </div>
    <label>算法<input readOnly value={value.algorithm} /></label>
    <label>SHA-256 指纹<textarea readOnly rows={2} value={value.fingerprint} /></label>
    <footer className="modal-actions"><button onClick={onCancel}>取消连接</button>
      <button className={value.changed ? 'danger-button' : 'primary'} onClick={() => void onAccept()}>
        {value.changed ? '我已核对，替换密钥' : '信任并继续'}
      </button></footer>
  </Modal>
}

function InteractiveChallengeDialog({ value, onSubmit, onCancel }: {
  value: InteractiveChallenge
  onSubmit: (answers: string[]) => Promise<void>
  onCancel: () => Promise<void>
}) {
  const [answers, setAnswers] = useState(() => value.questions.map(() => ''))
  return <Modal title="SSH 交互认证" onClose={() => void onCancel()}>
    <div className="security-notice">
      <LockKeyhole size={26} /><div>
        <strong>{value.user || 'SSH Server'}</strong>
        <p>{value.instruction || '服务器要求提供额外的身份验证信息。'}</p>
      </div>
    </div>
    {value.questions.map((question, index) => <label key={`${question}:${index}`}>
      {question || `验证信息 ${index + 1}`}
      <input autoFocus={index === 0} type={value.echoes[index] ? 'text' : 'password'}
        value={answers[index]}
        onChange={event => setAnswers(current => current.map((answer, answerIndex) =>
          answerIndex === index ? event.target.value : answer
        ))}
        onKeyDown={event => event.key === 'Enter' && void onSubmit(answers)} />
    </label>)}
    <small className="hint">动态验证码只用于本次连接，不会保存到保险库或命令历史。</small>
    <footer className="modal-actions"><button onClick={() => void onCancel()}>取消连接</button>
      <button className="primary" onClick={() => void onSubmit(answers)}>继续</button></footer>
  </Modal>
}

function SSHAuthPromptDialog({ connection, value, onCancel, onSubmit }: {
  connection: Connection
  value: NonNullable<Awaited<ReturnType<typeof api.StartSSH>>['authPrompt']>
  onCancel: () => void
  onSubmit: (payload: {
    password?: string
    privateKeyPem?: string
    passphrase?: string
    save: boolean
  }) => Promise<void>
}) {
  const [password, setPassword] = useState('')
  const [privateKeyPem, setPrivateKeyPem] = useState('')
  const [passphrase, setPassphrase] = useState('')
  const [save, setSave] = useState(true)
  return <Modal title="补充 SSH 凭据" onClose={onCancel} width="680px">
    <div className={`security-notice ${value.reason === 'invalid' ? 'danger' : ''}`}>
      <LockKeyhole size={26} /><div>
        <strong>{connection.name || connection.host}</strong>
        <p>{value.message}</p>
      </div>
    </div>
    <div className="form-grid">
      {value.kind === 'password' && <label className="full">密码
        <input autoFocus type="password" value={password} onChange={e => setPassword(e.target.value)} />
      </label>}
      {value.kind === 'private_key' && <>
        <label className="full">私钥 PEM
          <textarea autoFocus rows={7} value={privateKeyPem} onChange={e => setPrivateKeyPem(e.target.value)} />
        </label>
        <label className="full">私钥密码（如有）
          <input type="password" value={passphrase} onChange={e => setPassphrase(e.target.value)} />
        </label>
      </>}
      <label className="check full">
        <input type="checkbox" checked={save} onChange={e => setSave(e.target.checked)} />
        保存到保险库供后续连接复用
      </label>
    </div>
    <footer className="modal-actions">
      <button onClick={onCancel}>取消连接</button>
      <button className="primary"
        disabled={value.kind === 'password' ? !password : !privateKeyPem.trim()}
        onClick={() => void onSubmit({
          password: value.kind === 'password' ? password : undefined,
          privateKeyPem: value.kind === 'private_key' ? privateKeyPem : undefined,
          passphrase: value.kind === 'private_key' ? passphrase : undefined,
          save
        })}>
        继续连接
      </button>
    </footer>
  </Modal>
}

