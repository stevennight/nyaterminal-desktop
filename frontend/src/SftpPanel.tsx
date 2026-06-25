import { useEffect, useMemo, useState, type CSSProperties, type MouseEvent } from 'react'
import { createPortal } from 'react-dom'
import {
  ArrowDownToLine, ArrowUpFromLine, Folder, FolderPlus, GripHorizontal, Maximize2,
  Pause, Pencil, Play, RefreshCw, Square, Trash2, X
} from 'lucide-react'
import { api } from './bridge'
import { ContextMenu, type ContextMenuItem } from './ContextMenu'
import type { Connection, RemoteEntry, SFTPTransfer } from './types'
import { useVerticalSplit } from './useVerticalSplit'

const PANEL_TRANSFER_STORAGE_KEY = 'nyaterminal.sftpPanelTransferHeight'
const PANEL_FILE_MIN_HEIGHT = 220
const PANEL_TRANSFER_MIN_HEIGHT = 120
const PANEL_SPLITTER_HEIGHT = 10

type ContextMenuState = {
  x: number
  y: number
  items: ContextMenuItem[]
}

type RenameState = {
  entry: RemoteEntry
  value: string
}

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
  const [contextMenu, setContextMenu] = useState<ContextMenuState>()
  const [renaming, setRenaming] = useState<RenameState>()
  const {
    containerRef: panelBodyRef,
    height: transferHeight,
    beginResize: beginTransferResize,
  } = useVerticalSplit({
    storageKey: PANEL_TRANSFER_STORAGE_KEY,
    initialHeight: 150,
    minHeight: PANEL_TRANSFER_MIN_HEIGHT,
  })

  const load = async (next = remotePath) => {
    setBusy(true)
    setError('')
    try {
      setEntries(await api.ListRemote(connection.id, next))
      setRemotePath(next)
      setSelected(undefined)
      setContextMenu(undefined)
      setRenaming(undefined)
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

  const panelStyle = useMemo(() => ({
    ['--panel-transfer-height']: `${transferHeight}px`,
    ['--panel-file-min-height']: `${PANEL_FILE_MIN_HEIGHT}px`,
    ['--panel-transfer-min-height']: `${PANEL_TRANSFER_MIN_HEIGHT}px`,
    ['--panel-splitter-height']: `${PANEL_SPLITTER_HEIGHT}px`,
  }) as CSSProperties, [transferHeight])

  const createDirectory = async (basePath = remotePath) => {
    const name = window.prompt('新建远端目录名称')
    if (!name?.trim()) return
    await api.CreateRemoteDirectory(connection.id, joinRemote(basePath, name.trim()))
    await load()
  }
  const openRenameDialog = (entry = selected) => {
    if (!entry) return
    setContextMenu(undefined)
    setRenaming({ entry, value: entry.name })
  }
  const rename = async () => {
    if (!renaming) return
    const name = renaming.value.trim()
    if (!name || name === renaming.entry.name) {
      setRenaming(undefined)
      return
    }
    await api.RenameRemote(
      connection.id,
      renaming.entry.path,
      joinRemote(parentRemote(renaming.entry.path), name),
    )
    await load()
  }
  const remove = async (entry = selected) => {
    if (!entry || !window.confirm(`确定删除 ${entry.name}？`)) return
    await api.DeleteRemote(connection.id, entry.path, entry.isDir)
    await load()
  }
  const download = async (entry = selected) => {
    if (!entry) return
    if (entry.isDir) {
      setError('暂不支持直接下载目录，请进入目录后选择文件下载。')
      return
    }
    await api.DownloadFile(connection.id, entry.path, entry.name)
  }
  const copyPath = async (entry = selected) => {
    if (!entry) return
    if (!navigator.clipboard?.writeText) {
      setError('当前环境不支持复制到剪贴板。')
      return
    }
    try {
      await navigator.clipboard.writeText(entry.path)
    } catch {
      setError('复制路径失败，请稍后重试。')
    }
  }
  const showEntryContextMenu = (event: MouseEvent<HTMLDivElement>, entry: RemoteEntry) => {
    event.preventDefault()
    setSelected(entry)
    setContextMenu({
      x: event.clientX,
      y: event.clientY,
      items: [
        {
          label: '重命名',
          onSelect: () => openRenameDialog(entry),
        },
        {
          label: '下载',
          disabled: entry.isDir,
          onSelect: () => download(entry),
        },
        {
          label: entry.isDir ? '复制目录路径' : '复制文件路径',
          onSelect: () => copyPath(entry),
        },
        {
          label: entry.isDir ? '新建子目录' : '新建目录',
          onSelect: () => createDirectory(entry.isDir ? entry.path : remotePath),
        },
        {
          label: '删除',
          danger: true,
          onSelect: () => remove(entry),
        },
      ],
    })
  }

  return (
    <>
    <aside className="sftp-panel" style={panelStyle}>
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
            onClick={() => openRenameDialog()}>
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
      <div className="panel-main" ref={panelBodyRef}>
        <div className="file-list" onClick={event => {
          if (event.target === event.currentTarget) {
            setSelected(undefined)
            setContextMenu(undefined)
          }
        }}>
          {busy && <div className="empty">正在读取目录…</div>}
          {!busy && entries.map(entry => (
            <div className={`file-row ${selected?.path === entry.path ? 'selected' : ''}`} key={entry.path}
              onClick={() => {
                setSelected(entry)
                setContextMenu(undefined)
              }}
              onContextMenu={event => showEntryContextMenu(event, entry)}
              onDoubleClick={() => entry.isDir && void load(entry.path)}>
              {entry.isDir ? <Folder size={16} /> : <span className="file-dot" />}
              <span className="file-name">{entry.name}</span>
              <span className="file-size">{entry.isDir ? '' : formatSize(entry.size)}</span>
              {!entry.isDir && (
                <button title="下载" onClick={event => {
                  event.stopPropagation()
                  setSelected(entry)
                  void download(entry).catch(value => setError(String(value)))
                }}>
                  <ArrowDownToLine size={14} />
                </button>
              )}
            </div>
          ))}
        </div>
        <div
          className="panel-splitter"
          role="separator"
          aria-orientation="horizontal"
          aria-label="调整文件列表和传输队列高度"
          onPointerDown={beginTransferResize}
        >
          <GripHorizontal size={15} />
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
      </div>
    </aside>
    {contextMenu && <ContextMenu
      x={contextMenu.x}
      y={contextMenu.y}
      items={contextMenu.items}
      onClose={() => setContextMenu(undefined)}
    />}
    {renaming && <RenameDialog
      value={renaming.value}
      entry={renaming.entry}
      onChange={value => setRenaming(current => current ? { ...current, value } : current)}
      onClose={() => setRenaming(undefined)}
      onSave={() => void rename().catch(value => setError(String(value)))}
    />}
    </>
  )
}

function RenameDialog({ value, entry, onChange, onClose, onSave }: {
  value: string
  entry: RemoteEntry
  onChange: (value: string) => void
  onClose: () => void
  onSave: () => void
}) {
  const target = modalPortalTarget()
  if (!target) return null
  return createPortal(
    <div className="modal-backdrop" onMouseDown={event => event.target === event.currentTarget && onClose()}>
      <section className="modal" style={{ width: '420px' }}>
        <header><h2>重命名{entry.isDir ? '目录' : '文件'}</h2><button onClick={onClose}><X size={18} /></button></header>
        <div className="modal-body">
          <div className="form-grid rename-tab-form">
            <label className="full">名称
              <input
                autoFocus
                value={value}
                onChange={event => onChange(event.target.value)}
                onKeyDown={event => {
                  if (event.key === 'Enter') onSave()
                }}
              />
            </label>
            <small className="hint full">当前路径：{entry.path}</small>
          </div>
        </div>
        <footer className="modal-actions">
          <button onClick={onClose}>取消</button>
          <button className="primary" onClick={onSave}>保存</button>
        </footer>
      </section>
    </div>,
    target,
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

function parentRemote(value: string) {
  const normalized = value.replace(/\\/g, '/').replace(/\/+$/, '')
  const index = normalized.lastIndexOf('/')
  if (index < 0) return '.'
  return normalized.slice(0, index) || '/'
}

function modalPortalTarget() {
  if (typeof document === 'undefined') return null
  return document.querySelector('.app-shell') ?? document.body
}

function formatSize(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KB`
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MB`
  return `${(value / 1024 ** 3).toFixed(1)} GB`
}
