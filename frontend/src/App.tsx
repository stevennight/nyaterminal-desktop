import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import QRCode from 'qrcode'
import {
  ChevronDown, ChevronRight, Folder, FolderPlus, LockKeyhole, Monitor,
  Moon, MoreHorizontal, Plus, Search, Settings as SettingsIcon, Sun,
  TerminalSquare, X
} from 'lucide-react'
import { api } from './bridge'
import { SftpPanel } from './SftpPanel'
import { SftpWorkspace } from './SftpWorkspace'
import { TerminalView } from './TerminalView'
import type {
  Bootstrap, Connection, Credential, Group, InteractiveChallenge,
  PendingHostKey, Settings, Tag
} from './types'

type SessionTab = {
  id: string
  connection: Connection
  attempt: number
  sshSessionId?: string
  sftp: boolean
  privateSession: boolean
}

const emptyConnection: Connection = {
  id: '', name: '', host: '', port: 22, username: 'root',
  authentication: 'password', tags: [], encoding: 'utf-8',
  sortOrder: 0,
  keepAliveSeconds: 30, connectTimeoutSeconds: 15,
  legacyAlgorithms: false, commandHistory: true
}

export function App() {
  const [bootstrap, setBootstrap] = useState<Bootstrap>()
  const [error, setError] = useState('')
  const [connectionEditor, setConnectionEditor] = useState<Connection>()
  const [groupEditor, setGroupEditor] = useState(false)
  const [tagEditor, setTagEditor] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [sftpWorkspace, setSftpWorkspace] = useState<Connection>()
  const [sessions, setSessions] = useState<SessionTab[]>([])
  const [activeSession, setActiveSession] = useState('')
  const [hostKey, setHostKey] = useState<{ tabId: string; value: PendingHostKey }>()
  const [interactiveChallenge, setInteractiveChallenge] = useState<InteractiveChallenge>()
  const [query, setQuery] = useState('')
  const [activeTag, setActiveTag] = useState('')
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

  useEffect(() => {
    if (!bootstrap?.syncConfigured || bootstrap.vault.locked || !bootstrap.settings) return
    const run = () => void api.SyncNow(
      bootstrap.settings!.syncSecretsByDefault,
      bootstrap.settings!.syncCommandHistory
    ).catch(() => undefined)
    const initial = window.setTimeout(run, 10_000)
    const timer = window.setInterval(run, 5 * 60_000)
    return () => {
      window.clearTimeout(initial)
      window.clearInterval(timer)
    }
  }, [bootstrap?.syncConfigured, bootstrap?.vault.locked,
    bootstrap?.settings?.syncSecretsByDefault, bootstrap?.settings?.syncCommandHistory])

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
    return <CenteredCard title="NyaTerminal" subtitle="正在准备安全保险库…"><Spinner /></CenteredCard>
  }

  if (!bootstrap.vault.initialized) {
    return <VaultSetup onComplete={reload} />
  }

  if (bootstrap.vault.locked && (!bootstrap.settings || sessions.length === 0)) {
    return <Unlock quickUnlock={bootstrap.vault.quickUnlock} onComplete={reload} />
  }

  const settings = bootstrap.settings!
  const activeTab = sessions.find(session => session.id === activeSession)

  return (
    <div className={`app-shell theme-${settings.theme}`}>
      <aside className="sidebar">
        <div className="brand-row">
          <div className="brand-mark">N</div>
          <div><strong>NyaTerminal</strong><small>Secure workspace</small></div>
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
      {settingsOpen && <SettingsDialog value={settings} vault={bootstrap.vault}
        onClose={() => setSettingsOpen(false)}
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
          <Unlock quickUnlock={bootstrap.vault.quickUnlock} onComplete={reload} />
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

function VaultSetup({ onComplete }: { onComplete: () => Promise<void> }) {
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
  return <CenteredCard title="创建安全保险库" subtitle="主密码用于保护本机保存的连接和凭据。">
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

function Unlock({ quickUnlock, onComplete }: { quickUnlock: boolean; onComplete: () => Promise<void> }) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const submit = async () => {
    try {
      await api.Unlock(password)
      await onComplete()
    } catch { setError('主密码不正确') }
  }
  return <CenteredCard title="欢迎回来" subtitle="保险库已锁定，请验证后继续。">
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

function CenteredCard({ title, subtitle, children }: {
  title: string; subtitle: string; children: React.ReactNode
}) {
  return <div className="auth-screen"><div className="auth-card">
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
      <label className="check full"><input type="checkbox" checked={value.syncSecrets ?? false}
        onChange={e => update('syncSecrets', e.target.checked)} />同步此连接使用的密码或私钥</label>
      <label className="check full"><input type="checkbox" checked={value.legacyAlgorithms}
        onChange={e => update('legacyAlgorithms', e.target.checked)} />允许旧版弱算法（不推荐）</label>
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

function SettingsDialog({ value, vault, onClose, onSaved }: {
  value: Settings; vault: Bootstrap['vault']; onClose: () => void; onSaved: () => Promise<void>
}) {
  const [next, setNext] = useState(value)
  const [syncOpen, setSyncOpen] = useState(false)
  const [lockPassword, setLockPassword] = useState('')
  const [quickUnlock, setQuickUnlock] = useState(vault.quickUnlock)
  const [systemUnlockStatus, setSystemUnlockStatus] = useState('')
  const [oldMasterPassword, setOldMasterPassword] = useState('')
  const [newMasterPassword, setNewMasterPassword] = useState('')
  const [sensitiveRules, setSensitiveRules] = useState(value.sensitiveCommandRules.join('\n'))
  const updateSystemUnlock = async () => {
    setSystemUnlockStatus('')
    try {
      if (quickUnlock) {
        await api.DisableSystemUnlock()
        setQuickUnlock(false)
        setSystemUnlockStatus('系统快速解锁已关闭')
      } else {
        await api.EnableSystemUnlock()
        setQuickUnlock(true)
        setSystemUnlockStatus('系统快速解锁已启用')
      }
    } catch (error) {
      setSystemUnlockStatus(String(error))
    }
  }
  return <Modal title="设置" onClose={onClose}>
    <div className="form-grid">
      <label className="full">外观<div className="theme-picker">
        <button className={next.theme === 'dark' ? 'selected' : ''}
          onClick={() => setNext({ ...next, theme: 'dark' })}><Moon size={16} />深色</button>
        <button className={next.theme === 'light' ? 'selected' : ''}
          onClick={() => setNext({ ...next, theme: 'light' })}><Sun size={16} />浅色</button>
      </div></label>
      <label>终端字号<input type="number" value={next.fontSize}
        onChange={e => setNext({ ...next, fontSize: Number(e.target.value) })} /></label>
      <label className="full">终端字体<input value={next.fontFamily}
        onChange={e => setNext({ ...next, fontFamily: e.target.value })} /></label>
      <label>自动锁屏（分钟）<input type="number" value={next.lockAfterMinutes}
        onChange={e => setNext({ ...next, lockAfterMinutes: Number(e.target.value) })} /></label>
      <label className="check full"><input type="checkbox" checked={next.disconnectOnLock}
        onChange={e => setNext({ ...next, disconnectOnLock: e.target.checked })} />锁屏时断开 SSH 会话</label>
      <label className="check full"><input type="checkbox" checked={next.syncCommandHistory}
        onChange={e => setNext({ ...next, syncCommandHistory: e.target.checked })} />允许同步命令历史</label>
      <label className="check full"><input type="checkbox" checked={next.syncSecretsByDefault}
        onChange={e => setNext({ ...next, syncSecretsByDefault: e.target.checked })} />
        默认同步密码和私钥</label>
      <button className="secondary full" title={quickUnlock ? '关闭系统快速解锁' : '启用系统快速解锁'}
        onClick={() => void updateSystemUnlock()}>
        {quickUnlock ? '关闭系统快速解锁' : '启用系统快速解锁'}
      </button>
      <label className="full">独立锁屏密码（可选）
        <input type="password" value={lockPassword} placeholder="至少 8 个字符；留空不修改"
          onChange={event => setLockPassword(event.target.value)} /></label>
      {vault.customLockPassword && <button className="secondary full"
        onClick={() => void api.ClearLockPassword()
          .then(() => setSystemUnlockStatus('独立锁屏密码已清除'))
          .catch(error => setSystemUnlockStatus(String(error)))}>
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
      <div className="full settings-divider" />
      <button className="secondary full" onClick={() => setSyncOpen(true)}>配置端到端加密同步</button>
    </div>
    <footer className="modal-actions"><button onClick={onClose}>取消</button>
      <button className="primary" onClick={() => void (async () => {
        try {
          if (lockPassword) await api.SetLockPassword(lockPassword)
          if (oldMasterPassword || newMasterPassword) {
            await api.ChangeMasterPassword(oldMasterPassword, newMasterPassword)
          }
          await api.SaveSettings({
            ...next,
            sensitiveCommandRules: sensitiveRules.split('\n')
              .map(rule => rule.trim()).filter(Boolean)
          })
          await onSaved()
        } catch (error) {
          setSystemUnlockStatus(String(error))
        }
      })()}>保存</button></footer>
    <small className="hint">
      {quickUnlock
        ? `${vault.quickUnlockMethod} 快速解锁已启用；解锁时需要操作系统用户验证。`
        : `${vault.quickUnlockMethod} 快速解锁未启用。`}
    </small>
    {systemUnlockStatus && <div className="form-error">{systemUnlockStatus}</div>}
    {syncOpen && <SyncDialog settings={next} onClose={() => setSyncOpen(false)} />}
  </Modal>
}

function SyncDialog({ settings, onClose }: { settings: Settings; onClose: () => void }) {
  const [serverUrl, setServerUrl] = useState('https://')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [deviceName, setDeviceName] = useState('My device')
  const [recoveryCode, setRecoveryCode] = useState('')
  const [recoveryInput, setRecoveryInput] = useState('')
  const [status, setStatus] = useState('')
  const [pairing, setPairing] = useState<{
    shortCode: string; qrPayload: string; expiresAt: string
  }>()
  const [approvalPayload, setApprovalPayload] = useState('')
  const [approvalCode, setApprovalCode] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [devices, setDevices] = useState<Array<{
    id: string; name: string; approved: boolean; revoked: boolean
    createdAt: string; lastSeenAt: string
  }>>([])
  const [totpSetup, setTotpSetup] = useState<{ secret: string; setupToken: string; uri: string }>()
  const [accountRecoveryCodes, setAccountRecoveryCodes] = useState<string[]>([])
  const qrCanvas = useRef<HTMLCanvasElement>(null)
  const qrFile = useRef<HTMLInputElement>(null)
  const initialize = async () => {
    setStatus('正在初始化…')
    try {
      const result = await api.InitializeSync(serverUrl, username, password, deviceName)
      setRecoveryCode(result.recoveryCode)
      setStatus('同步服务初始化完成。请立即离线保存恢复码。')
    } catch (error) { setStatus(String(error)) }
  }
  const sync = async () => {
    setStatus('正在同步…')
    try {
      const result = await api.SyncNow(settings.syncSecretsByDefault, settings.syncCommandHistory)
      setStatus(`完成：上传 ${result.pushed}，下载 ${result.pulled}，冲突 ${result.conflicts}`)
    } catch (error) { setStatus(String(error)) }
  }
  const recover = async () => {
    setStatus('正在使用恢复码恢复同步保险库…')
    try {
      const result = await api.RecoverSync(
        serverUrl, username, password, totpCode, deviceName, recoveryInput
      )
      setStatus(`设备 ${result.deviceId} 已恢复，可以开始同步。`)
      setRecoveryInput('')
    } catch (error) { setStatus(String(error)) }
  }
  const rotateRecoveryCode = async () => {
    setStatus('正在轮换恢复码…')
    try {
      setRecoveryCode(await api.RotateSyncRecoveryCode())
      setStatus('恢复码已轮换。旧恢复码已失效，请立即保存新恢复码。')
    } catch (error) { setStatus(String(error)) }
  }
  const beginPairing = async () => {
    setStatus('正在创建设备配对请求…')
    try {
      const result = await api.BeginDevicePairing(serverUrl, deviceName)
      setPairing(result)
      setStatus('请在已授权设备上扫描二维码，并核对六位短码。')
    } catch (error) { setStatus(String(error)) }
  }
  const approvePairing = async () => {
    try {
      const parsed = JSON.parse(approvalPayload) as { shortCode?: string }
      if (!parsed.shortCode || parsed.shortCode !== approvalCode.trim()) {
        return setStatus('短码与配对载荷不一致，请重新核对。')
      }
      await api.ApproveDevicePairing(approvalPayload)
      setStatus('新设备已批准并收到加密密钥包。')
    } catch (error) { setStatus(String(error)) }
  }
  const scanPairingQR = async (file?: File) => {
    if (!file) return
    try {
      const Detector = (window as typeof window & {
        BarcodeDetector?: new (options: { formats: string[] }) => {
          detect(source: ImageBitmap): Promise<Array<{ rawValue: string }>>
        }
      }).BarcodeDetector
      if (!Detector) return setStatus('当前 WebView 不支持二维码识别，请粘贴二维码内容。')
      const bitmap = await createImageBitmap(file)
      const results = await new Detector({ formats: ['qr_code'] }).detect(bitmap)
      bitmap.close()
      if (!results[0]?.rawValue) return setStatus('图片中没有识别到配对二维码。')
      setApprovalPayload(results[0].rawValue)
      const parsed = JSON.parse(results[0].rawValue) as { shortCode?: string }
      if (parsed.shortCode) setApprovalCode(parsed.shortCode)
      setStatus('已识别配对二维码，请核对短码后批准。')
    } catch (error) { setStatus(String(error)) }
  }
  const claimPairing = async () => {
    try {
      const result = await api.ClaimDevicePairing(username, password, totpCode)
      setStatus(result.approved ? '设备配对完成，可以开始同步。' : '仍在等待已授权设备批准。')
    } catch (error) { setStatus(String(error)) }
  }
  const loadDevices = async () => {
    try { setDevices(await api.ListSyncDevices()) } catch (error) { setStatus(String(error)) }
  }
  const beginTOTP = async () => {
    try {
      setTotpSetup(await api.BeginSyncTOTPSetup())
      setStatus('请把密钥添加到验证器，再输入六位验证码确认。')
    } catch (error) { setStatus(String(error)) }
  }
  const confirmTOTP = async () => {
    if (!totpSetup) return
    try {
      setAccountRecoveryCodes(await api.ConfirmSyncTOTPSetup(totpSetup.setupToken, totpCode))
      setStatus('TOTP 已启用。请离线保存账号恢复码。')
    } catch (error) { setStatus(String(error)) }
  }
  useEffect(() => {
    if (pairing && qrCanvas.current) {
      void QRCode.toCanvas(qrCanvas.current, pairing.qrPayload, {
        width: 188, margin: 1, color: { dark: '#0a0e16', light: '#ffffff' }
      })
    }
  }, [pairing])
  return <div className="nested-modal">
    <header><strong>端到端加密同步</strong><button onClick={onClose}><X size={16} /></button></header>
    <label>服务地址<input value={serverUrl} onChange={e => setServerUrl(e.target.value)} /></label>
    <label>用户名<input value={username} onChange={e => setUsername(e.target.value)} /></label>
    <label>账号密码<input type="password" value={password} onChange={e => setPassword(e.target.value)} /></label>
    <label>设备名称<input value={deviceName} onChange={e => setDeviceName(e.target.value)} /></label>
    <div className="sync-button-grid">
      <button className="secondary" onClick={() => void beginPairing()}>将本设备加入已有保险库</button>
      <button className="secondary" onClick={() => void sync()}>立即同步</button>
    </div>
    <details className="pairing-approval">
      <summary>使用恢复码恢复设备</summary>
      <label>同步恢复码<textarea rows={3} value={recoveryInput}
        onChange={e => setRecoveryInput(e.target.value)}
        placeholder="输入初始化或轮换时生成的高熵恢复码" /></label>
      <button className="secondary wide" disabled={!recoveryInput.trim()}
        onClick={() => void recover()}>恢复同步保险库</button>
    </details>
    {pairing && <div className="pairing-card">
      <canvas ref={qrCanvas} />
      <div><small>设备配对短码</small><strong>{pairing.shortCode.slice(0, 3)} {pairing.shortCode.slice(3)}</strong>
        <p>二维码十分钟内有效。领取令牌不会包含在二维码中。</p>
        <button onClick={() => void claimPairing()}>检查批准状态</button></div>
    </div>}
    <details className="pairing-approval">
      <summary>批准另一台设备</summary>
      <input ref={qrFile} hidden type="file" accept="image/*"
        onChange={event => void scanPairingQR(event.target.files?.[0])} />
      <button className="secondary wide" onClick={() => qrFile.current?.click()}>
        从图片扫描配对二维码
      </button>
      <label>二维码载荷<textarea rows={4} value={approvalPayload}
        onChange={e => setApprovalPayload(e.target.value)}
        placeholder="扫描二维码后粘贴配对载荷" /></label>
      <label>六位短码<input inputMode="numeric" maxLength={6}
        value={approvalCode} onChange={e => setApprovalCode(e.target.value.replace(/\D/g, ''))} /></label>
      <button className="secondary wide" onClick={() => void approvePairing()}>核对并批准设备</button>
    </details>
    <label>账号 TOTP（启用时）<input inputMode="numeric" value={totpCode}
      onChange={e => setTotpCode(e.target.value)} /></label>
    <details className="pairing-approval">
      <summary onClick={() => !devices.length && void loadDevices()}>设备管理</summary>
      <div className="device-list">
        {devices.map(device => <div key={device.id}>
          <span><strong>{device.name}</strong><small>
            {device.revoked ? '已撤销' : device.approved ? '已授权' : '等待批准'}
          </small></span>
          {!device.revoked && <button onClick={() => void api.RevokeSyncDevice(device.id)
            .then(loadDevices).catch(error => setStatus(String(error)))}>撤销</button>}
        </div>)}
        {!devices.length && <small>同步尚未配置，或暂无设备。</small>}
      </div>
    </details>
    <details className="pairing-approval">
      <summary>账号二次验证</summary>
      {!totpSetup && <button className="secondary wide" onClick={() => void beginTOTP()}>启用 TOTP</button>}
      {!totpSetup && <button className="secondary wide" onClick={() => {
        if (!window.confirm('确定关闭账户 TOTP？现有账户恢复码也会失效。')) return
        void api.DisableSyncTOTP(password, totpCode)
          .then(() => setStatus('账户 TOTP 已关闭。'))
          .catch(error => setStatus(String(error)))
      }}>关闭 TOTP</button>}
      {totpSetup && <>
        <label>验证器密钥<input readOnly value={totpSetup.secret} /></label>
        <label>六位验证码<input inputMode="numeric" value={totpCode}
          onChange={e => setTotpCode(e.target.value.replace(/\D/g, ''))} /></label>
        <button className="secondary wide" onClick={() => void confirmTOTP()}>验证并启用</button>
      </>}
      {!!accountRecoveryCodes.length && <label>账号恢复码<textarea readOnly rows={6}
        value={accountRecoveryCodes.join('\n')} /></label>}
    </details>
    {recoveryCode && <label>恢复码<textarea readOnly rows={3} value={recoveryCode} /></label>}
    <button className="secondary wide" onClick={() => void rotateRecoveryCode()}>
      轮换同步恢复码
    </button>
    {status && <div className="sync-status">{status}</div>}
    <footer className="modal-actions">
      <button className="primary" onClick={() => void initialize()}>初始化服务</button></footer>
  </div>
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
