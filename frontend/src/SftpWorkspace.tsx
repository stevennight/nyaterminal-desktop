import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  ArrowDown, ArrowLeft, ArrowUp, Folder, FolderOpen, FolderPlus, Pause,
  Pencil, Play, RefreshCw, Square, Trash2, X
} from 'lucide-react'
import { api } from './bridge'
import type { Connection, RemoteEntry, SFTPTransfer } from './types'

export function SftpWorkspace({ connection, onClose }: {
  connection: Connection
  onClose: () => void
}) {
  const [token, setToken] = useState('')
  const [localRoot, setLocalRoot] = useState('')
  const [localPath, setLocalPath] = useState('.')
  const [remotePath, setRemotePath] = useState('.')
  const [localItems, setLocalItems] = useState<RemoteEntry[]>([])
  const [remoteItems, setRemoteItems] = useState<RemoteEntry[]>([])
  const [selectedLocal, setSelectedLocal] = useState<RemoteEntry[]>([])
  const [selectedRemote, setSelectedRemote] = useState<RemoteEntry[]>([])
  const [transfers, setTransfers] = useState<SFTPTransfer[]>([])
  const [error, setError] = useState('')

  const chooseLocal = async () => {
    try {
      setError('')
      const location = await api.ChooseLocalDirectory()
      if (!location.token) return
      setToken(location.token)
      setLocalRoot(location.path)
      setLocalPath('.')
      setLocalItems(location.items)
      setSelectedLocal([])
    } catch (reason) {
      setError(String(reason))
    }
  }

  const loadLocal = async (next = localPath) => {
    if (!token) return
    try {
      setLocalItems(await api.ListLocal(token, next))
      setLocalPath(next)
      setSelectedLocal([])
    } catch (reason) {
      setError(String(reason))
    }
  }

  const loadRemote = async (next = remotePath) => {
    try {
      setRemoteItems(await api.ListRemote(connection.id, next))
      setRemotePath(next)
      setSelectedRemote([])
    } catch (reason) {
      setError(String(reason))
    }
  }

  useEffect(() => {
    void chooseLocal()
    void loadRemote('.')
  }, [connection.id])

  useEffect(() => {
    const update = () => void api.ListSFTPTransfers().then(values =>
      setTransfers(values.filter(value => value.connectionId === connection.id)
        .sort((a, b) => b.createdAt.localeCompare(a.createdAt)))
    ).catch(reason => setError(String(reason)))
    update()
    const timer = window.setInterval(update, 500)
    return () => window.clearInterval(timer)
  }, [connection.id])

  const transfer = async (
    direction: SFTPTransfer['direction'],
    explicitSources?: RemoteEntry[]
  ) => {
    if (!token) return setError('请选择本地工作目录')
    const sources = (explicitSources ?? (direction === 'upload' ? selectedLocal : selectedRemote))
      .filter(item => !item.isDir)
    if (!sources.length) return setError('请选择至少一个文件')
    try {
      for (const source of sources) {
        const destinationExists = direction === 'upload'
          ? remoteItems.some(item => item.name === source.name)
          : localItems.some(item => item.name === source.name)
        const overwrite = destinationExists
          ? window.confirm(`${source.name} 已存在，覆盖吗？`)
          : false
        if (destinationExists && !overwrite) continue
        if (direction === 'upload') {
          await api.StartSFTPUpload(
            connection.id, token, source.path, joinRemote(remotePath, source.name), overwrite
          )
        } else {
          await api.StartSFTPDownload(
            connection.id, source.path, token, joinLocal(localPath, source.name), overwrite
          )
        }
      }
    } catch (reason) {
      setError(String(reason))
    }
  }

  const dropTransfer = async (target: 'local' | 'remote', source: 'local' | 'remote', paths: string[]) => {
    if (target === source) return
    const sourceItems = source === 'local' ? localItems : remoteItems
    const entries = sourceItems.filter(item => paths.includes(item.path))
    await transfer(source === 'local' ? 'upload' : 'download', entries)
  }

  const createLocalDirectory = async () => {
    if (!token) return setError('请选择本地工作目录')
    const name = window.prompt('请输入本地目录名')
    if (!name?.trim()) return
    try {
      await api.CreateLocalDirectory(token, joinLocal(localPath, name.trim()))
      await loadLocal()
    } catch (reason) {
      setError(String(reason))
    }
  }

  const renameLocal = async () => {
    if (!token || selectedLocal.length !== 1) return
    const selected = selectedLocal[0]
    const name = window.prompt('请输入新名称', selected.name)
    if (!name?.trim() || name.trim() === selected.name) return
    try {
      await api.RenameLocal(token, selected.path, joinLocal(localPath, name.trim()))
      await loadLocal()
    } catch (reason) {
      setError(String(reason))
    }
  }

  const deleteLocal = async () => {
    if (!token || !selectedLocal.length || !window.confirm(`确定删除这 ${selectedLocal.length} 个本地项目吗？`)) return
    try {
      for (const selected of selectedLocal) {
        await api.DeleteLocal(token, selected.path, selected.isDir)
      }
      await loadLocal()
    } catch (reason) {
      setError(String(reason))
    }
  }

  const createRemoteDirectory = async () => {
    const name = window.prompt('请输入远端目录名')
    if (!name?.trim()) return
    try {
      await api.CreateRemoteDirectory(connection.id, joinRemote(remotePath, name.trim()))
      await loadRemote()
    } catch (reason) {
      setError(String(reason))
    }
  }

  const renameRemote = async () => {
    if (selectedRemote.length !== 1) return
    const selected = selectedRemote[0]
    const name = window.prompt('请输入新名称', selected.name)
    if (!name?.trim() || name.trim() === selected.name) return
    try {
      await api.RenameRemote(connection.id, selected.path, joinRemote(remotePath, name.trim()))
      await loadRemote()
    } catch (reason) {
      setError(String(reason))
    }
  }

  const deleteRemote = async () => {
    if (!selectedRemote.length || !window.confirm(`确定删除这 ${selectedRemote.length} 个远端项目吗？`)) return
    try {
      for (const selected of selectedRemote) {
        await api.DeleteRemote(connection.id, selected.path, selected.isDir)
      }
      await loadRemote()
    } catch (reason) {
      setError(String(reason))
    }
  }

  const title = useMemo(() => `${connection.name} · SFTP`, [connection.name])

  return <div className="sftp-workspace-backdrop">
    <section className="sftp-workspace">
      <header className="sftp-workspace-header">
        <div><FolderOpen size={19} /><strong>{title}</strong></div>
        <button onClick={onClose}><X size={18} /></button>
      </header>
      {error && <div className="workspace-error">{error}</div>}
      <div className="sftp-columns">
        <FileColumn
          title="本地"
          root={localRoot || '尚未选择目录'}
          path={localPath}
          items={localItems}
          selected={selectedLocal.map(item => item.path)}
          side="local"
          actions={<>
            <button disabled={!token} onClick={() => void createLocalDirectory()}>
              <FolderPlus size={14} />新建本地目录
            </button>
            <button disabled={selectedLocal.length !== 1} onClick={() => void renameLocal()}>
              <Pencil size={14} />重命名本地
            </button>
            <button disabled={!selectedLocal.length} onClick={() => void deleteLocal()}>
              <Trash2 size={14} />删除本地
            </button>
          </>}
          onChooseRoot={() => void chooseLocal()}
          onRefresh={() => void loadLocal()}
          onParent={() => void loadLocal(parentLocal(localPath))}
          onSelect={(entry, additive) => setSelectedLocal(current => additive
            ? current.some(item => item.path === entry.path)
              ? current.filter(item => item.path !== entry.path) : [...current, entry]
            : [entry])}
          onOpen={entry => entry.isDir && void loadLocal(entry.path)}
          onDragEntries={entry => selectedLocal.some(item => item.path === entry.path)
            ? selectedLocal : [entry]}
          onDropFiles={(source, paths) => void dropTransfer('local', source, paths)}
        />
        <div className="transfer-actions">
          <button
            disabled={!selectedLocal.some(item => !item.isDir)}
            title="上传到远端"
            onClick={() => void transfer('upload')}
          >
            <ArrowUp size={18} />
          </button>
          <button
            disabled={!selectedRemote.some(item => !item.isDir)}
            title="下载到本地"
            onClick={() => void transfer('download')}
          >
            <ArrowDown size={18} />
          </button>
        </div>
        <FileColumn
          title="远端"
          root={`${connection.username}@${connection.host}`}
          path={remotePath}
          items={remoteItems}
          selected={selectedRemote.map(item => item.path)}
          side="remote"
          actions={<>
            <button onClick={() => void createRemoteDirectory()}>
              <FolderPlus size={14} />新建远端目录
            </button>
            <button disabled={selectedRemote.length !== 1} onClick={() => void renameRemote()}>
              <Pencil size={14} />重命名远端
            </button>
            <button disabled={!selectedRemote.length} onClick={() => void deleteRemote()}>
              <Trash2 size={14} />删除远端
            </button>
          </>}
          onRefresh={() => void loadRemote()}
          onParent={() => void loadRemote(parentRemote(remotePath))}
          onSelect={(entry, additive) => setSelectedRemote(current => additive
            ? current.some(item => item.path === entry.path)
              ? current.filter(item => item.path !== entry.path) : [...current, entry]
            : [entry])}
          onOpen={entry => entry.isDir && void loadRemote(entry.path)}
          onDragEntries={entry => selectedRemote.some(item => item.path === entry.path)
            ? selectedRemote : [entry]}
          onDropFiles={(source, paths) => void dropTransfer('remote', source, paths)}
        />
      </div>
      <div className="transfer-queue">
        <strong>传输队列</strong>
        {!transfers.length && <span>当前没有传输任务</span>}
        {transfers.map(item => <div key={item.id} className={`transfer-item ${item.status}`}>
          <i>{item.direction === 'upload' ? '↑' : '↓'}</i>
          <span>{item.name}<progress max={Math.max(1, item.totalBytes)} value={item.bytesDone} /></span>
          <small>{transferStatus(item)}</small>
          <div className="transfer-controls">
            {(item.status === 'running' || item.status === 'queued') &&
              <button title="暂停" onClick={() => void api.PauseSFTPTransfer(item.id)}>
                <Pause size={13} />
              </button>}
            {(item.status === 'paused' || item.status === 'failed') &&
              <button title="继续" onClick={() => void api.ResumeSFTPTransfer(item.id)}>
                <Play size={13} />
              </button>}
            {!['completed', 'cancelled'].includes(item.status) &&
              <button title="取消" onClick={() => void api.CancelSFTPTransfer(item.id)}>
                <Square size={12} />
              </button>}
          </div>
        </div>)}
      </div>
    </section>
  </div>
}

function transferStatus(item: SFTPTransfer) {
  if (item.status === 'failed') return item.error || '失败'
  const progress = item.totalBytes > 0
    ? `${Math.floor(item.bytesDone * 100 / item.totalBytes)}%`
    : formatSize(item.bytesDone)
  const labels: Record<SFTPTransfer['status'], string> = {
    queued: '排队中',
    running: '传输中',
    paused: '已暂停',
    completed: '已完成',
    failed: '失败',
    cancelled: '已取消'
  }
  return `${labels[item.status]} · ${progress}`
}

function FileColumn({ title, root, path, items, selected, side, onChooseRoot, onRefresh,
  onParent, onSelect, onOpen, onDragEntries, onDropFiles, actions }: {
  title: string; root: string; path: string; items: RemoteEntry[]; selected: string[]
  side: 'local' | 'remote'
  onChooseRoot?: () => void; onRefresh: () => void; onParent: () => void
  onSelect: (entry: RemoteEntry, additive: boolean) => void; onOpen: (entry: RemoteEntry) => void
  onDragEntries: (entry: RemoteEntry) => RemoteEntry[]
  onDropFiles: (source: 'local' | 'remote', paths: string[]) => void
  actions: ReactNode
}) {
  return <section className="file-column">
    <header>
      <div>
        <strong>{title}</strong>
        <small>{root}</small>
      </div>
      <div>
        {onChooseRoot && <button title="选择目录" onClick={onChooseRoot}><FolderOpen size={15} /></button>}
        <button title="返回上级" onClick={onParent}><ArrowLeft size={15} /></button>
        <button title="刷新" onClick={onRefresh}><RefreshCw size={15} /></button>
      </div>
    </header>
    <div className="column-path">{path}</div>
    <div className="column-actions">{actions}</div>
    <div className="column-head"><span>名称</span><span>大小</span><span>修改时间</span></div>
    <div
      className="column-files"
      onDragOver={event => event.preventDefault()}
      onDrop={event => {
        event.preventDefault()
        const source = event.dataTransfer.getData('application/x-nya-sftp-side') as 'local' | 'remote'
        const rawPaths = event.dataTransfer.getData('application/x-nya-sftp-paths')
        if (!source || !rawPaths) return
        try {
          const paths = JSON.parse(rawPaths)
          if (Array.isArray(paths) && paths.every(item => typeof item === 'string')) {
            onDropFiles(source, paths)
          }
        } catch {
          return
        }
      }}
    >
      {items.map(entry => <button
        key={entry.path}
        className={selected.includes(entry.path) ? 'selected' : ''}
        draggable
        onDragStart={event => {
          const entries = onDragEntries(entry)
          event.dataTransfer.setData('application/x-nya-sftp-side', side)
          event.dataTransfer.setData(
            'application/x-nya-sftp-paths',
            JSON.stringify(entries.map(item => item.path))
          )
          event.dataTransfer.effectAllowed = 'copyMove'
        }}
        onClick={event => onSelect(entry, event.ctrlKey || event.metaKey)}
        onDoubleClick={() => onOpen(entry)}
      >
        {entry.isDir ? <Folder size={16} /> : <i className="file-dot" />}
        <span>{entry.name}</span>
        <small>{entry.isDir ? '目录' : formatSize(entry.size)}</small>
        <time>{formatDate(entry.modTime)}</time>
      </button>)}
    </div>
  </section>
}

function parentRemote(value: string) {
  const normalized = value.replace(/\\/g, '/').replace(/\/+$/, '')
  const index = normalized.lastIndexOf('/')
  if (index < 0) return '.'
  return normalized.slice(0, index) || '/'
}

function parentLocal(value: string) {
  const normalized = value.replace(/\\/g, '/').replace(/\/+$/, '')
  const index = normalized.lastIndexOf('/')
  return index < 0 ? '.' : normalized.slice(0, index) || '.'
}

function joinRemote(parent: string, name: string) {
  return `${parent.replace(/\/+$/, '')}/${name}`.replace(/^\.\//, '')
}

function joinLocal(parent: string, name: string) {
  return parent === '.' ? name : `${parent.replace(/\/+$/, '')}/${name}`
}

function formatSize(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KB`
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MB`
  return `${(value / 1024 ** 3).toFixed(1)} GB`
}

function formatDate(value: string) {
  return new Date(value).toLocaleString(undefined, {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'
  })
}
