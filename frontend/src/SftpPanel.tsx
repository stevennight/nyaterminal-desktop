import { useEffect, useState } from 'react'
import {
  ArrowDownToLine, ArrowUpFromLine, Folder, FolderPlus, Maximize2,
  Pause, Pencil, Play, RefreshCw, Square, Trash2, X
} from 'lucide-react'
import { api } from './bridge'
import type { Connection, RemoteEntry, SFTPTransfer } from './types'

export function SftpPanel({ connection, onClose, onOpenWorkspace }: {
  connection: Connection
  onClose: () => void
  onOpenWorkspace: () => void
}) {
  const [remotePath, setRemotePath] = useState('.')
  const [entries, setEntries] = useState<RemoteEntry[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<RemoteEntry>()
  const [transfers, setTransfers] = useState<SFTPTransfer[]>([])

  const load = async (next = remotePath) => {
    setBusy(true)
    setError('')
    try {
      setEntries(await api.ListRemote(connection.id, next))
      setRemotePath(next)
      setSelected(undefined)
    } catch (value) {
      setError(String(value))
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => { void load('.') }, [connection.id])

  useEffect(() => {
    const update = () => void api.ListSFTPTransfers().then(values =>
      setTransfers(values.filter(value => value.connectionId === connection.id)
        .sort((a, b) => b.createdAt.localeCompare(a.createdAt)))
    ).catch(value => setError(String(value)))
    update()
    const timer = window.setInterval(update, 700)
    return () => window.clearInterval(timer)
  }, [connection.id])

  const createDirectory = async () => {
    const name = window.prompt('新建远端目录名称')
    if (!name?.trim()) return
    await api.CreateRemoteDirectory(connection.id, joinRemote(remotePath, name.trim()))
    await load()
  }
  const rename = async () => {
    if (!selected) return
    const name = window.prompt('新的名称', selected.name)
    if (!name?.trim() || name.trim() === selected.name) return
    await api.RenameRemote(connection.id, selected.path, joinRemote(remotePath, name.trim()))
    await load()
  }
  const remove = async () => {
    if (!selected || !window.confirm(`确定删除 ${selected.name}？`)) return
    await api.DeleteRemote(connection.id, selected.path, selected.isDir)
    await load()
  }

  return (
    <aside className="sftp-panel">
      <header>
        <strong>SFTP</strong>
        <div className="icon-actions">
          <button title="上传文件" onClick={() => void api.UploadFile(connection.id, remotePath)
            .catch(value => setError(String(value)))}>
            <ArrowUpFromLine size={16} />
          </button>
          <button title="新建目录" onClick={() => void createDirectory().catch(value => setError(String(value)))}>
            <FolderPlus size={16} />
          </button>
          <button title="重命名" disabled={!selected}
            onClick={() => void rename().catch(value => setError(String(value)))}>
            <Pencil size={15} />
          </button>
          <button title="删除" disabled={!selected}
            onClick={() => void remove().catch(value => setError(String(value)))}>
            <Trash2 size={15} />
          </button>
          <button title="打开双栏 SFTP" onClick={onOpenWorkspace}><Maximize2 size={16} /></button>
          <button title="刷新" onClick={() => void load()}><RefreshCw size={16} /></button>
          <button title="关闭" onClick={onClose}><X size={16} /></button>
        </div>
      </header>
      <input className="path-input" value={remotePath}
        onChange={event => setRemotePath(event.target.value)}
        onKeyDown={event => event.key === 'Enter' && void load(remotePath)} />
      {error && <div className="inline-error">{error}</div>}
      <div className="file-list">
        {busy && <div className="empty">正在读取目录…</div>}
        {!busy && entries.map(entry => (
          <div className={`file-row ${selected?.path === entry.path ? 'selected' : ''}`} key={entry.path}
            onClick={() => setSelected(entry)}
            onDoubleClick={() => entry.isDir && void load(entry.path)}>
            {entry.isDir ? <Folder size={16} /> : <span className="file-dot" />}
            <span className="file-name">{entry.name}</span>
            <span className="file-size">{entry.isDir ? '' : formatSize(entry.size)}</span>
            {!entry.isDir && (
              <button title="下载" onClick={() => void api.DownloadFile(connection.id, entry.path, entry.name)
                .catch(value => setError(String(value)))}>
                <ArrowDownToLine size={14} />
              </button>
            )}
          </div>
        ))}
      </div>
      <div className="panel-transfer-queue">
        <strong>传输队列</strong>
        {!transfers.length && <span>暂无任务</span>}
        {!!transfers.length && <div className="panel-transfer-list">
          {transfers.map(item => <div key={item.id} className={`panel-transfer ${item.status}`}>
          <i>{item.direction === 'upload' ? '↑' : '↓'}</i>
          <span title={item.name}>{item.name}</span>
          <small>{transferStatus(item)}</small>
          <div>
            {(item.status === 'running' || item.status === 'queued') &&
              <button title="暂停" onClick={() => void api.PauseSFTPTransfer(item.id)}>
                <Pause size={12} />
              </button>}
            {(item.status === 'paused' || item.status === 'failed') &&
              <button title="继续" onClick={() => void api.ResumeSFTPTransfer(item.id)}>
                <Play size={12} />
              </button>}
            {!['completed', 'cancelled'].includes(item.status) &&
              <button title="取消" onClick={() => void api.CancelSFTPTransfer(item.id)}>
                <Square size={11} />
              </button>}
          </div>
          </div>)}
        </div>}
      </div>
    </aside>
  )
}

function transferStatus(item: SFTPTransfer) {
  if (item.status === 'failed') return item.error || '失败'
  const progress = item.totalBytes > 0
    ? `${Math.floor(item.bytesDone * 100 / item.totalBytes)}%`
    : formatSize(item.bytesDone)
  const labels: Record<SFTPTransfer['status'], string> = {
    queued: '等待中', running: '传输中', paused: '已暂停',
    completed: '已完成', failed: '失败', cancelled: '已取消'
  }
  return `${labels[item.status]} · ${progress}`
}

function joinRemote(parent: string, name: string) {
  return `${parent.replace(/\/+$/, '')}/${name}`.replace(/^\.\//, '')
}

function formatSize(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KB`
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MB`
  return `${(value / 1024 ** 3).toFixed(1)} GB`
}
