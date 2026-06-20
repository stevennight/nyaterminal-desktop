import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ChevronDown, ChevronRight, Folder, FolderPlus, LockKeyhole, Monitor,
  Moon, MoreHorizontal, Paintbrush, Plus, Search, Settings as SettingsIcon,
  Shield, SlidersHorizontal, Sun, TerminalSquare, X
} from 'lucide-react'
import { api } from './bridge'
import { SftpPanel } from './SftpPanel'
import { SftpWorkspace } from './SftpWorkspace'
import { TerminalView } from './TerminalView'
import type {
  AccountSummary, Bootstrap, Connection, Credential, Group, InteractiveChallenge,
  PendingHostKey, Settings, SyncSummary, Tag
} from './types'

type SessionTab = {
  id: string
  connection: Connection
  attempt: number
  sshSessionId?: string
  sftp: boolean
  privateSession: boolean
}

type ThemeName = 'dark' | 'light'

const THEME_STORAGE_KEY = 'nyaterminal.theme'

const emptyConnection: Connection = {
  id: '', name: '', host: '', port: 22, username: 'root',
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

function syncHeadline(syncSummary?: SyncSummary) {
  if (!syncSummary?.loggedIn) return '未登录'
  if (!syncSummary.syncInitialized) return '等待初始化'
  if (!syncSummary.configured) return '等待加入'
  return '已加入同步'
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
  const [hostKey, setHostKey] = useState<{ tabId: string; value: PendingHostKey }>()
  const [interactiveChallenge, setInteractiveChallenge] = useState<InteractiveChallenge>()
  const [query, setQuery] = useState('')
  const [activeTag, setActiveTag] = useState('')
  const [syncBusy, setSyncBusy] = useState(false)
  const activityTimer = useRef<number | undefined>(undefined)
  const searchInput = useRef<HTMLInputElement>(null)

  const reload = useCallback(async () => {
    try {
      setError('')
      setBootstrap(await api.Bootstrap())
    } catch (value) {
      setError(String(value))
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
      id, connection, attempt: 0, sftp: false, privateSession
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
                    void api.DeleteTag(tag.id).then(reload).catch(reason => setError(String(reason)))
                  }
                } else {
                  void api.SaveTag({ ...tag, name: name.trim() }).then(reload)
                    .catch(reason => setError(String(reason)))
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
          {sessions.map(tab => (
            <button key={tab.id} className={`session-tab ${tab.id === activeSession ? 'active' : ''}`}
              onClick={() => setActiveSession(tab.id)}>
              <TerminalSquare size={15} /><span>{tab.connection.name}</span>
              <i onClick={event => { event.stopPropagation(); closeSession(tab.id) }}><X size={13} /></i>
            </button>
          ))}
          <button className="new-tab" onClick={() => setConnectionEditor({ ...emptyConnection })}><Plus size={16} /></button>
          <div className="window-drag" />
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
              onReady={sessionId => setSessions(current => current.map(item =>
                item.id === tab.id ? { ...item, sshSessionId: sessionId } : item
              ))}
              onHostKey={value => setHostKey({ tabId: tab.id, value })}
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
          onClose={() => setConnectionEditor(undefined)}
          onDeleted={async id => {
            await api.DeleteConnection(id)
            setConnectionEditor(undefined)
            await reload()
          }}
          onSaved={async connection => {
            setConnectionEditor(undefined)
            await reload()
            openConnection(connection)
          }} />
      )}
      {groupEditor && <GroupEditor groups={bootstrap.groups ?? []} onClose={() => setGroupEditor(false)}
        onSaved={async () => { setGroupEditor(false); await reload() }} />}
      {tagEditor && <TagEditor onClose={() => setTagEditor(false)}
        onSaved={async () => { setTagEditor(false); await reload() }} />}
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
  const submit = async () => {
    try {
      await api.Unlock(password)
      await onComplete()
    } catch { setError('主密码不正确') }
  }
  return <CenteredCard theme={theme} title="欢迎回来" subtitle="保险库已锁定，请验证后继续。">
    <div className="unlock-icon"><LockKeyhole /></div>
    <label>主密码<input autoFocus type="password" value={password}
      onChange={event => setPassword(event.target.value)}
      onKeyDown={event => event.key === 'Enter' && void submit()} /></label>
    {error && <div className="form-error">{error}</div>}
    <button className="primary wide" onClick={() => void submit()}>解锁</button>
    {quickUnlock && <button className="system-unlock wide" onClick={() =>
      void api.UnlockWithSystem().then(onComplete).catch(() => setError('系统快捷解锁不可用'))
    }>使用系统凭据解锁</button>}
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

function Modal({ title, children, onClose, width = '520px' }: {
  title: string; children: React.ReactNode; onClose: () => void; width?: string
}) {
  return <div className="modal-backdrop" onMouseDown={event => event.target === event.currentTarget && onClose()}>
    <section className="modal" style={{ width }}><header><h2>{title}</h2>
      <button onClick={onClose}><X size={18} /></button></header>{children}</section>
  </div>
}

function ConnectionEditor({ initial, groups, tags, onClose, onSaved, onDeleted }: {
  initial: Connection; groups: Group[]; tags: Tag[]; onClose: () => void
  onSaved: (value: Connection) => Promise<void>
  onDeleted: (id: string) => Promise<void>
}) {
  const [value, setValue] = useState(initial)
  const [secret, setSecret] = useState('')
  const [passphrase, setPassphrase] = useState('')
  const [error, setError] = useState('')
  const update = <K extends keyof Connection>(key: K, next: Connection[K]) =>
    setValue(current => ({ ...current, [key]: next }))
  const save = async () => {
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
      await onSaved(await api.SaveConnection({ ...value, credentialId }))
    } catch (reason) { setError(String(reason)) }
  }
  return <Modal title={value.id ? '编辑 SSH 连接' : '新建 SSH 连接'} onClose={onClose}>
    <div className="form-grid">
      <label className="full">名称<input value={value.name} onChange={e => update('name', e.target.value)} /></label>
      <label className="wide">主机<input value={value.host} onChange={e => update('host', e.target.value)} /></label>
      <label>端口<input type="number" value={value.port} onChange={e => update('port', Number(e.target.value))} /></label>
      <label className="wide">用户名<input value={value.username} onChange={e => update('username', e.target.value)} /></label>
      <label>分组<select value={value.groupId ?? ''} onChange={e => update('groupId', e.target.value)}>
        <option value="">未分组</option>{groups.map(group => <option key={group.id} value={group.id}>{group.name}</option>)}
      </select></label>
      <label className="full">认证方式<select value={value.authentication}
        onChange={e => update('authentication', e.target.value as Connection['authentication'])}>
        <option value="password">密码</option><option value="private_key">私钥</option>
        <option value="agent">SSH Agent</option><option value="interactive">键盘交互</option>
      </select></label>
      <div className="full connection-tags"><span>标签</span><div>
        {tags.map(tag => <button key={tag.id} type="button"
          className={value.tags.includes(tag.id) ? 'selected' : ''}
          onClick={() => update('tags', value.tags.includes(tag.id)
            ? value.tags.filter(id => id !== tag.id) : [...value.tags, tag.id])}>
          <i style={{ background: tag.color }} />{tag.name}
        </button>)}
        {!tags.length && <small>可先在左侧创建标签</small>}
      </div></div>
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
    <footer className="modal-actions">
      {value.id && <button className="danger-button" onClick={() => {
        if (window.confirm(`确定删除连接 ${value.name}？`)) void onDeleted(value.id)
      }}>删除</button>}
      <button onClick={onClose}>取消</button>
      <button className="primary" onClick={() => void save()}>保存并连接</button></footer>
  </Modal>
}

function GroupEditor({ groups, onClose, onSaved }: {
  groups: Group[]; onClose: () => void; onSaved: () => Promise<void>
}) {
  const [name, setName] = useState('')
  const [parentId, setParentId] = useState('')
  return <Modal title="新建分组" onClose={onClose}>
    <div className="form-grid">
      <label className="full">名称<input autoFocus value={name} onChange={e => setName(e.target.value)} /></label>
      <label className="full">上级分组<select value={parentId} onChange={e => setParentId(e.target.value)}>
        <option value="">无（顶级分组）</option>{groups.map(group =>
          <option key={group.id} value={group.id}>{group.name}</option>)}
      </select></label>
    </div>
    <footer className="modal-actions"><button onClick={onClose}>取消</button>
      <button className="primary" onClick={() => void api.SaveGroup({
        id: '', name, parentId, sortOrder: groups.length
      }).then(onSaved)}>创建</button></footer>
  </Modal>
}

function TagEditor({ onClose, onSaved }: {
  onClose: () => void; onSaved: () => Promise<void>
}) {
  const [name, setName] = useState('')
  const [color, setColor] = useState('#62d9ca')
  return <Modal title="新建标签" onClose={onClose}>
    <div className="form-grid">
      <label className="wide">名称<input autoFocus value={name} onChange={e => setName(e.target.value)} /></label>
      <label>颜色<input type="color" value={color} onChange={e => setColor(e.target.value)} /></label>
    </div>
    <footer className="modal-actions"><button onClick={onClose}>取消</button>
      <button className="primary" onClick={() => void api.SaveTag({
        id: '', name, color
      }).then(onSaved)}>创建</button></footer>
  </Modal>
}

function SettingsDialog({ value, vault, syncSummary, syncBusy, onSyncBusyChange, onClose, onSaved, onReload }: {
  value: Settings; vault: Bootstrap['vault']; syncSummary?: SyncSummary
  syncBusy: boolean; onSyncBusyChange: (value: boolean) => void
  onClose: () => void; onSaved: () => Promise<void>; onReload: () => Promise<void>
}) {
  const [next, setNext] = useState(value)
  const [autoSyncEnabled, setAutoSyncEnabled] = useState(syncSummary?.autoSyncEnabled ?? true)
  const [lockPassword, setLockPassword] = useState('')
  const [quickUnlock, setQuickUnlock] = useState(vault.quickUnlock)
  const [notice, setNotice] = useState<{ title: string; message: string }>()
  const [oldMasterPassword, setOldMasterPassword] = useState('')
  const [newMasterPassword, setNewMasterPassword] = useState('')
  const [sensitiveRules, setSensitiveRules] = useState(value.sensitiveCommandRules.join('\n'))
  const [activeSection, setActiveSection] = useState<'appearance' | 'terminal' | 'security' | 'sync'>('appearance')
  const [deviceName, setDeviceName] = useState(syncSummary?.deviceName ?? '当前设备')
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
    setDeviceName(syncSummary?.deviceName ?? '当前设备')
  }, [syncSummary?.deviceName])

  const sections = [
    { id: 'appearance', label: '外观', icon: Paintbrush },
    { id: 'terminal', label: '终端', icon: Monitor },
    { id: 'security', label: '安全', icon: Shield },
    { id: 'sync', label: '同步', icon: SlidersHorizontal }
  ] as const

  const showNotice = (title: string, message: string) => setNotice({ title, message })

  const toggleSystemUnlock = async (enabled: boolean) => {
    try {
      if (enabled) await api.EnableSystemUnlock()
      else await api.DisableSystemUnlock()
      setQuickUnlock(enabled)
      showNotice('系统快速解锁', enabled ? '系统快速解锁已启用。' : '系统快速解锁已关闭。')
    } catch (error) {
      showNotice('系统快速解锁', String(error))
    }
  }

  const persist = async () => {
    try {
      if (lockPassword) await api.SetLockPassword(lockPassword)
      if (oldMasterPassword || newMasterPassword) {
        await api.ChangeMasterPassword(oldMasterPassword, newMasterPassword)
      }
      await api.SaveSettings({
        ...next,
        sensitiveCommandRules: sensitiveRules.split('\n').map(rule => rule.trim()).filter(Boolean)
      })
      await onSaved()
    } catch (error) {
      showNotice('保存设置失败', String(error))
    }
  }

  const syncNow = async () => {
    onSyncBusyChange(true)
    try {
      const result = await api.SyncNow(next.syncSecretsByDefault, next.syncCommandHistory)
      showNotice('同步完成', `上传 ${result.pushed}，下载 ${result.pulled}，冲突 ${result.conflicts}。`)
      await onReload()
    } catch (error) {
      showNotice('同步失败', String(error))
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
      showNotice('自动同步', String(error))
      setAutoSyncEnabled(syncSummary?.autoSyncEnabled ?? true)
    }
  }

  const initializeSync = async () => {
    onSyncBusyChange(true)
    try {
      const result = await api.InitializeSync(deviceName.trim())
      setRecoveryCodeModal({
        title: '同步初始化完成',
        code: result.recoveryCode
      })
      await onReload()
    } catch (error) {
      showNotice('同步初始化失败', String(error))
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
        deviceName.trim(),
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
      showNotice('设备加入', String(error))
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
      setPairing(await api.BeginDevicePairing(syncSummary.serverUrl, deviceName.trim()))
      setShowJoinModal(true)
      showNotice('设备加入', '请在已加入同步的设备上输入批准串并核对短码。')
    } catch (error) {
      showNotice('设备加入', String(error))
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
      showNotice('设备加入', String(error))
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
      showNotice('批准新设备', String(error))
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
      showNotice('刷新同步恢复码失败', String(error))
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
      showNotice('退出同步失败', String(error))
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
      showNotice('重置同步失败', String(error))
    } finally {
      onSyncBusyChange(false)
    }
  }

  const loggedIn = !!syncSummary?.loggedIn
  const syncInitialized = !!syncSummary?.syncInitialized
  const syncConfigured = !!syncSummary?.configured

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
    </div>
  } else if (activeSection === 'terminal') {
    content = <div className="settings-page">
      <h3>终端</h3>
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
            .catch(error => showNotice('独立锁屏密码', String(error)))}>
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
          <span>{syncSummary?.serverUrl
            ? `${syncSummary.serverUrl} · ${syncSummary.username}${syncSummary.deviceName ? ` · ${syncSummary.deviceName}` : ''}`
            : '请先登录服务端账号'}</span>
        </div>
        <div>
          <strong>{syncSummary?.running ? '同步中' : syncConfigured && syncSummary?.autoSyncEnabled === false ? '自动同步已关闭' : '同步待命'}</strong>
          <span>{syncSummary?.lastSyncedAt ? `上次同步：${formatDateTime(syncSummary.lastSyncedAt)}` : '还没有同步记录'}</span>
        </div>
        <div>
          <strong>{syncSummary?.lastError ? '最近失败' : '最近结果'}</strong>
          <span>{syncSummary?.lastError ?? (syncSummary?.lastAttemptAt ? `上次尝试：${formatDateTime(syncSummary.lastAttemptAt)}` : '暂无')}</span>
        </div>
      </div>
      {!loggedIn && <div className="pairing-approval">
        <small className="hint full">登录后才能查看远端同步状态、初始化同步保险库或加入现有同步。</small>
      </div>}
      {loggedIn && !syncInitialized && <div className="pairing-approval">
        <label>设备名称<input value={deviceName} onChange={e => setDeviceName(e.target.value)} /></label>
        <small className="hint full">当前账号在服务端还没有同步保险库。初始化后会生成恢复码，并将本机加入同步。</small>
        <button className="secondary wide" type="button" disabled={syncBusy}
          onClick={() => void initializeSync()}>{syncBusy ? '处理中…' : '初始化同步保险库'}</button>
      </div>}
      {loggedIn && syncInitialized && !syncConfigured && <div className="pairing-approval">
        <label>设备名称<input value={deviceName} onChange={e => setDeviceName(e.target.value)} /></label>
        <small className="hint full">服务端已有同步保险库，但本机还没加入。可以使用恢复码直接加入，或请求一台已加入的设备批准。</small>
        <div className="sync-actions">
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
        <button className="secondary full" type="button" disabled={syncBusy}
          onClick={() => void syncNow()}>{syncBusy ? '同步中…' : '立即同步'}</button>
        <button className="secondary full" type="button" disabled={syncBusy}
          onClick={() => setShowRotateRecoveryModal(true)}>刷新同步恢复码</button>
        <button className="secondary full" type="button" disabled={syncBusy}
          onClick={() => setShowApproveModal(true)}>批准新设备加入</button>
        <button className="danger-button full" type="button" disabled={syncBusy}
          onClick={() => setShowLeaveSyncModal(true)}>退出同步保险库</button>
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

  return <Modal title="设置" onClose={onClose} width="920px">
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
    <footer className="modal-actions"><button onClick={onClose}>取消</button>
      <button className="primary" onClick={() => void persist()}>保存</button></footer>
    {notice && <NoticeDialog title={notice.title} message={notice.message} onClose={() => setNotice(undefined)} />}
    {recoveryCodeModal && <RecoveryCodeDialog title={recoveryCodeModal.title} code={recoveryCodeModal.code}
      onClose={() => setRecoveryCodeModal(undefined)} />}
    {showJoinModal && <JoinSyncDialog
      syncBusy={syncBusy}
      deviceName={deviceName}
      joinPassword={joinPassword}
      joinTotpCode={joinTotpCode}
      syncRecoveryCode={syncRecoveryCode}
      pairing={pairing}
      onDeviceNameChange={setDeviceName}
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
  const [accessExpiresAt, setAccessExpiresAt] = useState(account?.accessExpiresAt ?? '')
  const [refreshExpiresAt, setRefreshExpiresAt] = useState(account?.refreshExpiresAt ?? '')
  const [devices, setDevices] = useState<Array<{
    id: string; name: string; approved: boolean; revoked: boolean
    createdAt: string; lastSeenAt: string
  }>>([])
  const [totpSetup, setTotpSetup] = useState<{ secret: string; setupToken: string; uri: string }>()
  const [accountRecoveryCodes, setAccountRecoveryCodes] = useState<string[]>([])
  useEffect(() => {
    if (account?.serverUrl) setServerUrl(account.serverUrl)
    if (account?.username) setUsername(account.username)
    setLoggedIn(account?.loggedIn ?? false)
    setDeviceId(account?.deviceId ?? '')
    setAccessExpiresAt(account?.accessExpiresAt ?? '')
    setRefreshExpiresAt(account?.refreshExpiresAt ?? '')
  }, [account?.serverUrl, account?.username, account?.loggedIn, account?.deviceId, account?.accessExpiresAt, account?.refreshExpiresAt])
  useEffect(() => {
    if (loggedIn) void loadDevices()
  }, [loggedIn])
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
      await onReload()
    } catch (error) { showNotice('账号管理', String(error)) }
  }
  const logout = async () => {
    try {
      await api.LogoutAccount()
      showNotice('账号管理', '已退出登录。')
      setLoggedIn(false)
      await onReload()
    } catch (error) { showNotice('账号管理', String(error)) }
  }
  const loadDevices = async () => {
    try {
      const values = await api.ListSyncDevices()
      setDevices(Array.isArray(values) ? values : [])
    } catch (error) { showNotice('设备管理', String(error)) }
  }
  const beginTOTP = async () => {
    try {
      setTotpSetup(await api.BeginSyncTOTPSetup())
      showNotice('TOTP 设置', '请把密钥添加到验证器，再输入六位验证码确认。')
    } catch (error) { showNotice('TOTP 设置', String(error)) }
  }
  const confirmTOTP = async () => {
    if (!totpSetup) return
    try {
      setAccountRecoveryCodes(await api.ConfirmSyncTOTPSetup(totpSetup.setupToken, totpCode))
      showNotice('TOTP 设置', 'TOTP 已启用。请离线保存账号恢复码。')
    } catch (error) { showNotice('TOTP 设置', String(error)) }
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
        <button className="secondary" onClick={() => void loadDevices()}>刷新设备</button>
        <button className="danger-button" onClick={() => void logout()}>退出登录</button>
        <button className="icon-button" onClick={onClose} title="关闭"><X size={16} /></button>
      </div>
    </header>
    <div className="account-panel-body">
      <div className="sync-summary">
        <div>
          <strong>{deviceId ? '当前设备' : '暂无设备编号'}</strong>
          <span>{deviceId || '设备编号会在首次登录时生成并保存。'}</span>
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
      <div className="pairing-approval">
        <div className="device-list">
          {devices.map(device => <div key={device.id}>
            <span><strong>{device.name}</strong><small>
              {device.approved ? '已授权' : '等待批准'}
            </small></span>
            <button onClick={() => {
              if (!window.confirm(`确定撤销设备「${device.name}」？`)) return
              void api.RevokeSyncDevice(device.id)
                .then(loadDevices).catch(error => showNotice('设备管理', String(error)))
            }}>撤销</button>
          </div>)}
          {!devices.length && <small>暂无设备，或尚未刷新。</small>}
        </div>
      </div>
      <details className="pairing-approval">
        <summary>账号二次验证</summary>
        {!totpSetup && <button className="secondary wide" onClick={() => void beginTOTP()}>启用 TOTP</button>}
        {totpSetup && <>
          <label>验证器密钥<input readOnly value={totpSetup.secret} /></label>
          <label>六位验证码<input inputMode="numeric" value={totpCode}
            onChange={e => setTotpCode(e.target.value.replace(/\D/g, ''))} /></label>
          <button className="secondary wide" onClick={() => void confirmTOTP()}>验证并启用</button>
        </>}
        {!!accountRecoveryCodes.length && <label>账号恢复码<textarea readOnly rows={6}
          value={accountRecoveryCodes.join('\n')} /></label>}
        {!!accountRecoveryCodes.length && <small className="hint full">
          每条恢复码只能使用一次，可在登录时填到 “TOTP / 恢复码” 输入框里代替 6 位验证码。
        </small>}
      </details>
      <details className="pairing-approval">
        <summary>登录状态</summary>
        <small className="hint full">
          当前设备和用户名仅作展示，退出后可重新登录，已撤销设备也可再次登录并重新授权。
        </small>
      </details>
    </div>
    {notice && <NoticeDialog title={notice.title} message={notice.message} onClose={() => setNotice(undefined)} />}
    </section>
  </Modal>
}

function NoticeDialog({ title, message, onClose }: {
  title: string; message: string; onClose: () => void
}) {
  return <div className="modal-backdrop" onMouseDown={event => event.target === event.currentTarget && onClose()}>
    <section className="modal notice-modal">
      <header><h2>{title}</h2><button onClick={onClose}><X size={18} /></button></header>
      <div className="notice-body">{message}</div>
      <footer className="modal-actions"><button className="primary" onClick={onClose}>确定</button></footer>
    </section>
  </div>
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

function JoinSyncDialog({ syncBusy, deviceName, joinPassword, joinTotpCode, syncRecoveryCode, pairing, onDeviceNameChange, onJoinPasswordChange, onJoinTotpCodeChange, onRecoveryCodeChange, onClose, onRecover, onClaim, onCopyApprovalCode }: {
  syncBusy: boolean
  deviceName: string
  joinPassword: string
  joinTotpCode: string
  syncRecoveryCode: string
  pairing?: { pairingId: string; deviceId: string; shortCode: string; approvalCode: string; expiresAt: string }
  onDeviceNameChange: (value: string) => void
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
      <label className="full">设备名称<input value={deviceName} onChange={e => onDeviceNameChange(e.target.value)} /></label>
      <label className="wide">账号密码<input type="password" value={joinPassword} onChange={e => onJoinPasswordChange(e.target.value)} /></label>
      <label>TOTP / 恢复码<input value={joinTotpCode} onChange={e => onJoinTotpCodeChange(e.target.value.trim())} /></label>
      <label className="full">同步恢复码<input value={syncRecoveryCode} onChange={e => onRecoveryCodeChange(e.target.value)} /></label>
    </div>
    <small className="hint full">你可以直接用恢复码加入，或先发起请求，再由已加入同步的设备输入批准串并核对短码。</small>
    <div className="sync-actions">
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
        <div className="sync-actions">
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
