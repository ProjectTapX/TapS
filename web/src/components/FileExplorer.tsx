import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Breadcrumb, Button, Space, Table, Modal, Input, Upload, App, Popconfirm, Tooltip, Progress, Tag,
} from 'antd'
import {
  FolderOpenOutlined, FileOutlined, ReloadOutlined, UploadOutlined,
  FolderAddOutlined, EditOutlined, DeleteOutlined, DownloadOutlined, FormOutlined,
  CopyOutlined, ScissorOutlined, FileZipOutlined, InboxOutlined,
} from '@ant-design/icons'
import type { UploadProps } from 'antd'
import { useTranslation } from 'react-i18next'
import { fsApi, type FsEntry } from '@/api/fs'
import { useAuthStore } from '@/stores/auth'
import { formatApiError } from '@/api/errors'

interface Props {
  daemonId: number
  // rootPath pins the explorer to a virtual subtree — the user can navigate
  // below it but the breadcrumb won't go above. Used by the per-instance
  // file tab so users land directly in the instance's /data dir.
  rootPath?: string
}

function formatSize(n: number) {
  if (n < 1024) return n + ' B'
  if (n < 1024 ** 2) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 ** 3) return (n / 1024 ** 2).toFixed(1) + ' MB'
  return (n / 1024 ** 3).toFixed(2) + ' GB'
}

interface UploadJob {
  id: string
  name: string
  total: number
  loaded: number
  done: boolean
  error?: string
}

export default function FileExplorer({ daemonId, rootPath }: Props) {
  const { t } = useTranslation()
  const { message, modal } = App.useApp()
  const fs = useMemo(() => fsApi(daemonId), [daemonId])
  const root = (rootPath && rootPath.trim()) || '/'
  const [path, setPath] = useState(root)
  const [data, setData] = useState<FsEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<{ path: string; content: string } | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [transfer, setTransfer] = useState<{ kind: 'copy' | 'move'; src: string; defaultName: string } | null>(null)
  const [uploads, setUploads] = useState<UploadJob[]>([])

  const load = async (p = path) => {
    setLoading(true)
    try {
      // Refuse to navigate above the configured root.
      if (root !== '/' && !p.startsWith(root)) p = root
      const r = await fs.list(p)
      setData(r.entries)
      setPath(r.path || p)
      setSelected([])
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setLoading(false) }
  }
  useEffect(() => { load(root) }, [daemonId, root])

  // Antd Tabs keep inactive panes mounted (just display:none), so switching
  // away to the terminal and back doesn't remount the explorer. Watch our
  // own visibility with IntersectionObserver — when the wrapper goes from
  // hidden to visible, refetch so the user sees the current state of the
  // directory (anything added by terminal commands etc.).
  const wrapperRef = useRef<HTMLDivElement>(null)
  // Track the latest path in a ref so the IntersectionObserver callback
  // (registered once) can call load with the current path, not whatever
  // was current when it was set up.
  const pathRef = useRef(path)
  useEffect(() => { pathRef.current = path }, [path])
  useEffect(() => {
    const el = wrapperRef.current
    if (!el) return
    let wasVisible = false
    const io = new IntersectionObserver(([entry]) => {
      const visible = entry.isIntersecting && entry.intersectionRatio > 0
      if (visible && !wasVisible) load(pathRef.current)
      wasVisible = visible
    })
    io.observe(el)
    return () => io.disconnect()
  }, [daemonId])

  // Breadcrumb: when scoped to a root, hide the segments above it and use
  // a "Files" label for the root crumb instead of "/".
  const rootSegs = root === '/' ? [] : root.replace(/\\/g, '/').split('/').filter(Boolean)
  const allSegs = path.replace(/\\/g, '/').split('/').filter(Boolean)
  const segments = root === '/' ? allSegs : allSegs.slice(rootSegs.length)
  const goTo = (idx: number) => load(root === '/' ? ('/' + allSegs.slice(0, idx + 1).join('/')) : (root + (idx >= 0 ? '/' + segments.slice(0, idx + 1).join('/') : '')))
  // Entering a folder navigates into it (load picks up the new listing).
  // Entering a file opens the editor; we refresh the current directory at
  // the same time so the listing reflects any out-of-band changes since
  // the user last looked.
  const enter = (e: FsEntry) => {
    if (e.isDir) {
      load(joinPath(path, e.name))
    } else {
      load(path)
      openEditor(joinPath(path, e.name))
    }
  }

  const openEditor = async (full: string) => {
    try { setEditing({ path: full, content: (await fs.read(full)).content }) }
    catch (e: any) { message.error(formatApiError(e, 'files.readFailed')) }
  }

  const onMkdir = () => {
    let name = ''
    modal.confirm({
      title: t('files.mkdir'),
      content: <Input placeholder={t('files.mkdirNew')} onChange={(e) => { name = e.target.value }} />,
      onOk: async () => {
        if (!name) return
        await fs.mkdir(joinPath(path, name)); message.success(t('common.success')); load()
      },
    })
  }

  const onRename = (e: FsEntry) => {
    let name = e.name
    modal.confirm({
      title: t('files.rename'),
      content: <Input defaultValue={e.name} onChange={(ev) => { name = ev.target.value }} />,
      onOk: async () => {
        if (!name || name === e.name) return
        await fs.rename(joinPath(path, e.name), joinPath(path, name))
        load()
      },
    })
  }

  const onBatchDelete = () => {
    if (selected.length === 0) return
    modal.confirm({
      title: t('files.batchDelete', { n: selected.length }),
      onOk: async () => {
        for (const name of selected) {
          try { await fs.remove(joinPath(path, name)) } catch { /* keep going */ }
        }
        message.success(t('common.success')); load()
      },
    })
  }

  const onZipSelected = () => {
    if (selected.length === 0) return
    let dest = (selected.length === 1 ? selected[0] : 'archive') + '.zip'
    modal.confirm({
      title: t('files.zipTitle', { n: selected.length }),
      content: (
        <Input
          defaultValue={dest}
          onChange={(e) => { dest = e.target.value }}
          placeholder="archive.zip"
        />
      ),
      onOk: async () => {
        if (!dest) return
        const paths = selected.map(name => joinPath(path, name))
        try {
          await fs.zip(paths, joinPath(path, dest))
          message.success(t('common.success'))
          load()
        } catch (e: any) {
          message.error(formatApiError(e, 'common.error'))
        }
      },
    })
  }

  const onUnzip = (e: FsEntry) => {
    let destDir = path
    modal.confirm({
      title: t('files.unzipTitle', { name: e.name }),
      content: (
        <>
          <p style={{ marginBottom: 6 }}>{t('files.unzipDest')}:</p>
          <Input
            defaultValue={destDir}
            onChange={(ev) => { destDir = ev.target.value }}
          />
        </>
      ),
      onOk: async () => {
        try {
          await fs.unzip(joinPath(path, e.name), destDir || path)
          message.success(t('common.success'))
          load()
        } catch (err: any) {
          message.error(formatApiError(err, 'common.error'))
        }
      },
    })
  }

  const onTransferConfirm = async (destPath: string) => {
    if (!transfer) return
    try {
      // The dialog operates on root-relative paths so the user doesn't see
      // the noisy `/data/inst-xxx` mount prefix. Re-expand to absolute
      // before talking to the daemon.
      const absDest = toAbsPath(destPath, root)
      const absSrc = toAbsPath(transfer.src, root)
      if (transfer.kind === 'copy') await fs.copy(absSrc, absDest)
      else await fs.move(absSrc, absDest)
      message.success(t('common.success'))
      setTransfer(null)
      load()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    }
  }

  const token = useAuthStore.getState().token ?? ''
  const CHUNK_SIZE = 1024 * 1024 // 1 MiB — small enough that XHR.upload.onprogress reports accurately

  // Init the upload session: declare total bytes / chunks / target so
  // the daemon can quota-check upfront. Returns the uploadId every
  // chunk request must echo back.
  const initUpload = (file: File, dst: string, totalChunks: number) =>
    new Promise<string>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      const q = `token=${encodeURIComponent(token)}`
      xhr.open('POST', `/api/daemons/${daemonId}/files/upload/init?${q}`)
      xhr.setRequestHeader('Content-Type', 'application/json')
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          try {
            const r = JSON.parse(xhr.responseText)
            resolve(String(r.uploadId))
          } catch (e: any) { reject(new Error('bad init response')) }
        } else {
          reject(new Error(xhr.responseText || `HTTP ${xhr.status}`))
        }
      }
      xhr.onerror = () => reject(new Error('network'))
      xhr.send(JSON.stringify({
        path: dst, filename: file.name, totalBytes: file.size, totalChunks,
      }))
    })

  // Send a single chunk via XHR. Returns when the chunk has been ACKed by daemon
  // (so the next chunk only starts after the previous landed).
  const sendChunk = (file: File, chunk: Blob, params: { uploadId: string; seq: number; total: number; final: boolean; dst: string }, onChunkProgress: (loaded: number) => void) =>
    new Promise<void>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      const q = `token=${encodeURIComponent(token)}&path=${encodeURIComponent(params.dst)}&uploadId=${encodeURIComponent(params.uploadId)}&seq=${params.seq}&total=${params.total}${params.final ? '&final=true' : ''}`
      xhr.open('POST', `/api/daemons/${daemonId}/files/upload?${q}`)
      xhr.upload.onprogress = (e) => { if (e.lengthComputable) onChunkProgress(e.loaded) }
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) resolve()
        else reject(new Error(xhr.responseText || `HTTP ${xhr.status}`))
      }
      xhr.onerror = () => reject(new Error('network'))
      const fd = new FormData(); fd.append('file', chunk, file.name)
      xhr.send(fd)
    })

  const uploadProps: UploadProps = {
    multiple: true, showUploadList: false, name: 'file',
    customRequest: async ({ file, onSuccess, onError }) => {
      const f = file as File
      const dst = joinPath(path, f.name)
      const totalChunks = Math.max(1, Math.ceil(f.size / CHUNK_SIZE))
      const jobId = (crypto as any).randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`
      const job: UploadJob = { id: jobId, name: f.name, total: f.size, loaded: 0, done: false }
      setUploads(prev => [...prev, job])
      const updateJob = (patch: Partial<UploadJob>) => setUploads(prev => prev.map(u => u.id === jobId ? { ...u, ...patch } : u))

      try {
        // 1. Reserve the quota up front. If the volume can't fit the
        //    file the daemon refuses here and we never start streaming.
        const uploadId = await initUpload(f, dst, totalChunks)
        let baseLoaded = 0
        for (let i = 0; i < totalChunks; i++) {
          const start = i * CHUNK_SIZE
          const end = Math.min(start + CHUNK_SIZE, f.size)
          const chunk = f.slice(start, end)
          const isFinal = i === totalChunks - 1
          await sendChunk(f, chunk, { uploadId, seq: i, total: totalChunks, final: isFinal, dst },
            (loaded) => updateJob({ loaded: baseLoaded + loaded }))
          baseLoaded += chunk.size
          updateJob({ loaded: baseLoaded })
        }
        updateJob({ done: true, loaded: f.size })
        onSuccess?.({}, new XMLHttpRequest())
        message.success(`${f.name} ${t('files.uploadOk')}`)
        load()
      } catch (e: any) {
        updateJob({ done: true, error: String(e?.message ?? e) })
        onError?.(e)
        message.error(`${f.name} ${t('files.uploadFail')}`)
      }
    },
  }

  const downloadUrl = (full: string) =>
    `/api/daemons/${daemonId}/files/download?token=${encodeURIComponent(token)}&path=${encodeURIComponent(full)}`

  return (
    <div ref={wrapperRef}>
      <Space style={{ marginBottom: 12 }} wrap>
        <Breadcrumb
          items={[
            { title: <a onClick={() => load(root)}>{root === '/' ? '/' : t('files.root')}</a> },
            ...segments.map((s, i) => ({ title: <a onClick={() => goTo(i)}>{s}</a> })),
          ]}
        />
      </Space>
      <Space style={{ marginBottom: 12 }} wrap>
        <Button icon={<ReloadOutlined />} onClick={() => load()}>{t('common.refresh')}</Button>
        <Button icon={<FolderAddOutlined />} onClick={onMkdir}>{t('files.mkdir')}</Button>
        <Upload {...uploadProps}><Button icon={<UploadOutlined />}>{t('files.upload')}</Button></Upload>
        {selected.length > 0 && (
          <>
            <Tag color="blue">{t('files.selected', { n: selected.length })}</Tag>
            <Button icon={<FileZipOutlined />} onClick={onZipSelected}>{t('files.zip')}</Button>
            <Button danger icon={<DeleteOutlined />} onClick={onBatchDelete}>{t('common.delete')}</Button>
          </>
        )}
      </Space>

      {uploads.length > 0 && (
        <div style={{
          marginBottom: 12, padding: 12,
          background: 'var(--taps-surface)', border: '1px solid var(--taps-border)',
          borderRadius: 8,
        }}>
          {uploads.map((u, i) => (
            <div key={i} style={{ marginBottom: 6 }}>
              <Space>
                <code style={{ fontSize: 12 }}>{u.name}</code>
                <span style={{ fontSize: 12, color: 'var(--taps-text-muted)' }}>{formatSize(u.loaded)} / {formatSize(u.total)}</span>
              </Space>
              <Progress percent={u.total ? Math.floor((u.loaded / u.total) * 100) : 0}
                size="small" status={u.error ? 'exception' : u.done ? 'success' : 'active'} />
            </div>
          ))}
          <Button size="small" type="link" onClick={() => setUploads([])}>{t('common.cancel')}</Button>
        </div>
      )}

      <Table<FsEntry>
        rowKey="name"
        loading={loading}
        dataSource={data}
        size="middle"
        pagination={{
          pageSize: 20,
          showSizeChanger: false,
          hideOnSinglePage: true,
          size: 'small',
        }}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: (keys) => setSelected(keys as string[]),
        }}
        onRow={(r) => ({ onDoubleClick: () => enter(r) })}
        columns={[
          {
            title: t('files.name'), dataIndex: 'name',
            render: (_, r) => (
              <Space>
                {r.isDir ? <FolderOpenOutlined style={{ color: '#7c5cff' }} /> : <FileOutlined />}
                <a onClick={() => enter(r)}>{r.name}</a>
              </Space>
            ),
          },
          { title: t('files.size'), dataIndex: 'size', width: 120, render: (v: number, r) => r.isDir ? '-' : formatSize(v) },
          { title: t('files.mtime'), dataIndex: 'modified', width: 200, render: (s: number) => new Date(s * 1000).toLocaleString() },
          { title: t('files.mode'), dataIndex: 'mode', width: 120 },
          {
            title: t('common.actions'), width: 280,
            render: (_, r) => (
              <Space size={2}>
                {!r.isDir && (
                  <Tooltip title={t('common.edit')}>
                    <Button size="small" icon={<EditOutlined />} onClick={() => openEditor(joinPath(path, r.name))} />
                  </Tooltip>
                )}
                {!r.isDir && (
                  <Tooltip title={t('files.download')}>
                    <Button size="small" icon={<DownloadOutlined />} href={downloadUrl(joinPath(path, r.name))} />
                  </Tooltip>
                )}
                {!r.isDir && r.name.toLowerCase().endsWith('.zip') && (
                  <Tooltip title={t('files.unzip')}>
                    <Button size="small" icon={<InboxOutlined />} onClick={() => onUnzip(r)} />
                  </Tooltip>
                )}
                <Tooltip title={t('files.rename')}>
                  <Button size="small" icon={<FormOutlined />} onClick={() => onRename(r)} />
                </Tooltip>
                <Tooltip title={t('files.copy')}>
                  <Button size="small" icon={<CopyOutlined />}
                    onClick={() => setTransfer({ kind: 'copy', src: toRelPath(joinPath(path, r.name), root), defaultName: toRelPath(joinPath(path, r.name), root) + '.copy' })} />
                </Tooltip>
                <Tooltip title={t('files.move')}>
                  <Button size="small" icon={<ScissorOutlined />}
                    onClick={() => setTransfer({ kind: 'move', src: toRelPath(joinPath(path, r.name), root), defaultName: toRelPath(joinPath(path, r.name), root) })} />
                </Tooltip>
                <Popconfirm title={t('files.confirmDelete', { name: r.name })} onConfirm={async () => { await fs.remove(joinPath(path, r.name)); load() }}>
                  <Button size="small" danger icon={<DeleteOutlined />} />
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title={editing?.path}
        open={!!editing}
        width={900}
        onCancel={() => { setEditing(null); load() }}
        onOk={async () => {
          if (!editing) return
          try { await fs.write(editing.path, editing.content); message.success(t('files.saved')); setEditing(null); load() }
          catch (e: any) { message.error(formatApiError(e, 'files.saveFailed')) }
        }}
        destroyOnClose
      >
        <Input.TextArea
          value={editing?.content}
          onChange={(e) => setEditing(editing ? { ...editing, content: e.target.value } : null)}
          autoSize={{ minRows: 20, maxRows: 30 }}
          style={{ fontFamily: 'Consolas, monospace' }}
        />
      </Modal>

      {transfer && (
        <TransferDialog
          kind={transfer.kind}
          src={transfer.src}
          defaultDest={transfer.defaultName}
          onCancel={() => setTransfer(null)}
          onOk={onTransferConfirm}
        />
      )}
    </div>
  )
}

interface TDProps {
  kind: 'copy' | 'move'
  src: string
  defaultDest: string
  onCancel: () => void
  onOk: (dest: string) => void
}
function TransferDialog({ kind, src, defaultDest, onCancel, onOk }: TDProps) {
  const { t } = useTranslation()
  const [dest, setDest] = useState(defaultDest)
  return (
    <Modal
      open
      title={t(kind === 'copy' ? 'files.copy' : 'files.move')}
      onCancel={onCancel}
      onOk={() => onOk(dest)}
      destroyOnClose
    >
      <p style={{ marginBottom: 8 }}>{t('files.transferFrom')}: <code>{src}</code></p>
      <p style={{ marginBottom: 4 }}>{t('files.transferTo')}:</p>
      <Input value={dest} onChange={(e) => setDest(e.target.value)} />
    </Modal>
  )
}

function joinPath(base: string, name: string) {
  const b = base.replace(/\\/g, '/').replace(/\/+$/, '')
  return (b === '' ? '/' : b) + '/' + name
}

// Convert an absolute file-manager path to one rooted at the visible root.
// "/data/inst-abc/world" with root "/data/inst-abc" → "/world".
function toRelPath(abs: string, root: string): string {
  if (!root || root === '/') return abs
  if (abs === root) return '/'
  if (abs.startsWith(root + '/')) return abs.slice(root.length)
  return abs
}

// Inverse of toRelPath. Accepts both "/world" and "world".
function toAbsPath(rel: string, root: string): string {
  if (!root || root === '/') return rel.startsWith('/') ? rel : '/' + rel
  const r = rel.startsWith('/') ? rel : '/' + rel
  if (r === '/') return root
  return root + r
}
