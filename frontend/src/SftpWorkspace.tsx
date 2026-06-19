import { useEffect, useMemo, useState } from 'react'
import {
  ArrowDown, ArrowLeft, ArrowUp, Folder, FolderOpen, RefreshCw, X
} from 'lucide-react'
import { api } from './bridge'
import type { Connection, RemoteEntry } from './types'

type Transfer = {
  id: string
  name: string
  direction: 'upload' | 'download'
  status: 'running' | 'done' | 'error'
  error?: string
}

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
  const [selectedLocal, setSelectedLocal] = useState<RemoteEntry>()
  const [selectedRemote, setSelectedRemote] = useState<RemoteEntry>()
  const [transfers, setTransfers] = useState<Transfer[]>([])
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
      setSelectedLocal(undefined)
    } catch (reason) { setError(String(reason)) }
  }

  const loadLocal = async (next = localPath) => {
    if (!token) return
    try {
      setLocalItems(await api.ListLocal(token, next))
      setLocalPath(next)
      setSelectedLocal(undefined)
    } catch (reason) { setError(String(reason)) }
  }

  const loadRemote = async (next = remotePath) => {
    try {
      setRemoteItems(await api.ListRemote(connection.id, next))
      setRemotePath(next)
      setSelectedRemote(undefined)
    } catch (reason) { setError(String(reason)) }
  }

  useEffect(() => {
    void chooseLocal()
    void loadRemote('.')
  }, [connection.id])

  const transfer = async (direction: Transfer['direction']) => {
    if (!token) return setError('请先选择本地工作目录')
    const source = direction === 'upload' ? selectedLocal : selectedRemote
    if (!source || source.isDir) return setError('请选择要传输的文件')
    const item: Transfer = {
      id: crypto.randomUUID(), name: source.name, direction, status: 'running'
    }
    setTransfers(current => [item, ...current])
    try {
      if (direction === 'upload') {
        await api.UploadGranted(
          connection.id, token, source.path, joinRemote(remotePath, source.name)
        )
        await loadRemote()
      } else {
        await api.DownloadGranted(
          connection.id, source.path, token, joinLocal(localPath, source.name)
        )
        await loadLocal()
      }
      setTransfers(current => current.map(value =>
        value.id === item.id ? { ...value, status: 'done' } : value
      ))
    } catch (reason) {
      setTransfers(current => current.map(value =>
        value.id === item.id ? { ...value, status: 'error', error: String(reason) } : value
      ))
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
        <FileColumn title="本地" root={localRoot || '尚未选择目录'} path={localPath}
          items={localItems} selected={selectedLocal?.path}
          onChooseRoot={() => void chooseLocal()}
          onRefresh={() => void loadLocal()}
          onParent={() => void loadLocal(parentLocal(localPath))}
          onSelect={setSelectedLocal}
          onOpen={entry => entry.isDir && void loadLocal(entry.path)} />
        <div className="transfer-actions">
          <button disabled={!selectedLocal || selectedLocal.isDir}
            title="上传到远端" onClick={() => void transfer('upload')}><ArrowUp size={18} /></button>
          <button disabled={!selectedRemote || selectedRemote.isDir}
            title="下载到本地" onClick={() => void transfer('download')}><ArrowDown size={18} /></button>
        </div>
        <FileColumn title="远端" root={`${connection.username}@${connection.host}`} path={remotePath}
          items={remoteItems} selected={selectedRemote?.path}
          onRefresh={() => void loadRemote()}
          onParent={() => void loadRemote(parentRemote(remotePath))}
          onSelect={setSelectedRemote}
          onOpen={entry => entry.isDir && void loadRemote(entry.path)} />
      </div>
      <div className="transfer-queue">
        <strong>传输队列</strong>
        {!transfers.length && <span>暂无传输任务</span>}
        {transfers.map(item => <div key={item.id} className={`transfer-item ${item.status}`}>
          <i>{item.direction === 'upload' ? '↑' : '↓'}</i>
          <span>{item.name}</span>
          <small>{item.status === 'running' ? '传输中' : item.status === 'done' ? '完成' : item.error}</small>
        </div>)}
      </div>
    </section>
  </div>
}

function FileColumn({ title, root, path, items, selected, onChooseRoot, onRefresh,
  onParent, onSelect, onOpen }: {
  title: string; root: string; path: string; items: RemoteEntry[]; selected?: string
  onChooseRoot?: () => void; onRefresh: () => void; onParent: () => void
  onSelect: (entry: RemoteEntry) => void; onOpen: (entry: RemoteEntry) => void
}) {
  return <section className="file-column">
    <header><div><strong>{title}</strong><small>{root}</small></div>
      <div>{onChooseRoot && <button title="选择目录" onClick={onChooseRoot}><FolderOpen size={15} /></button>}
        <button title="上级目录" onClick={onParent}><ArrowLeft size={15} /></button>
        <button title="刷新" onClick={onRefresh}><RefreshCw size={15} /></button></div></header>
    <div className="column-path">{path}</div>
    <div className="column-head"><span>名称</span><span>大小</span><span>修改时间</span></div>
    <div className="column-files">
      {items.map(entry => <button key={entry.path}
        className={selected === entry.path ? 'selected' : ''}
        onClick={() => onSelect(entry)} onDoubleClick={() => onOpen(entry)}>
        {entry.isDir ? <Folder size={16} /> : <i className="file-dot" />}
        <span>{entry.name}</span><small>{entry.isDir ? '—' : formatSize(entry.size)}</small>
        <time>{formatDate(entry.modTime)}</time>
      </button>)}
    </div>
  </section>
}

function parentRemote(value: string) {
  const normalized = value.replaceAll('\\', '/').replace(/\/+$/, '')
  const index = normalized.lastIndexOf('/')
  if (index < 0) return '.'
  return normalized.slice(0, index) || '/'
}

function parentLocal(value: string) {
  const normalized = value.replaceAll('\\', '/').replace(/\/+$/, '')
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

