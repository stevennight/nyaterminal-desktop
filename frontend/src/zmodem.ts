import { Receiver, ReceiverEvent, Sender, SenderEvent } from 'zmodem2'

type Callbacks = {
  toTerminal: (data: Uint8Array) => void
  send: (data: Uint8Array) => void
  onStatus: (message: string) => void
}

const decoder = new TextDecoder('latin1')

// zmodem2 deliberately exposes protocol state machines rather than terminal
// detection. This adapter keeps the protocol isolated from the terminal and
// only switches modes after a valid hex ZMODEM header prefix is observed.
export class ZmodemAdapter {
  private receiver?: Receiver
  private sender?: Sender
  private receiveChunks: Uint8Array[] = []
  private receiveName = ''
  private sendFiles: File[] = []
  private sendIndex = 0
  private selectingFile = false

  constructor(private callbacks: Callbacks) {}

  consume(data: Uint8Array) {
    if (this.receiver) {
      this.consumeReceiver(data)
      return
    }
    if (this.sender) {
      this.consumeSender(data)
      return
    }
    const mode = detectHeader(data)
    if (!mode) {
      this.callbacks.toTerminal(data)
      return
    }
    const before = data.slice(0, mode.offset)
    if (before.length) this.callbacks.toTerminal(before)
    const protocolData = data.slice(mode.offset)
    if (mode.type === 'receive') {
      this.callbacks.onStatus('检测到远端 sz，正在接收文件')
      this.receiver = new Receiver()
      this.consumeReceiver(protocolData)
    } else {
      this.selectFiles(protocolData)
    }
  }

  private consumeReceiver(data: Uint8Array) {
    if (!this.receiver) return
    this.receiver.feedIncoming(data)
    this.flushReceiver()
  }

  private flushReceiver() {
    const receiver = this.receiver
    if (!receiver) return
    const outgoing = receiver.drainOutgoing()
    if (outgoing.length) this.callbacks.send(outgoing)
    const fileData = receiver.drainFile()
    if (fileData.length) this.receiveChunks.push(fileData.slice())
    for (let event = receiver.pollEvent(); event; event = receiver.pollEvent()) {
      if (event === ReceiverEvent.FileStart) {
        this.receiveName = safeFilename(receiver.getFileName())
        this.receiveChunks = []
      } else if (event === ReceiverEvent.FileComplete) {
        const blob = new Blob(this.receiveChunks, { type: 'application/octet-stream' })
        const url = URL.createObjectURL(blob)
        const anchor = document.createElement('a')
        anchor.href = url
        anchor.download = this.receiveName || 'download.bin'
        anchor.click()
        window.setTimeout(() => URL.revokeObjectURL(url), 1000)
        this.callbacks.onStatus(`已接收 ${this.receiveName}`)
      } else if (event === ReceiverEvent.SessionComplete) {
        this.receiver = undefined
        this.receiveChunks = []
        this.callbacks.onStatus('ZMODEM 接收完成')
      }
    }
  }

  private selectFiles(initialData: Uint8Array) {
    if (this.selectingFile) return
    this.selectingFile = true
    this.callbacks.onStatus('检测到远端 rz，请选择要发送的文件')
    const input = document.createElement('input')
    input.type = 'file'
    input.multiple = true
    input.hidden = true
    document.body.appendChild(input)
    input.onchange = () => {
      this.selectingFile = false
      this.sendFiles = Array.from(input.files ?? [])
      input.remove()
      if (!this.sendFiles.length) {
        this.callbacks.onStatus('已取消 ZMODEM 发送')
        this.callbacks.toTerminal(initialData)
        return
      }
      this.sender = new Sender(false)
      this.sender.feedIncoming(initialData)
      this.startCurrentFile()
      void this.flushSender()
    }
    input.click()
  }

  private consumeSender(data: Uint8Array) {
    if (!this.sender) return
    this.sender.feedIncoming(data)
    void this.flushSender()
  }

  private async flushSender() {
    const sender = this.sender
    if (!sender) return
    let outgoing = sender.drainOutgoing()
    if (outgoing.length) this.callbacks.send(outgoing)
    for (let request = sender.pollFile(); request; request = sender.pollFile()) {
      const file = this.sendFiles[this.sendIndex]
      const chunk = new Uint8Array(await file.slice(request.offset, request.offset + request.len).arrayBuffer())
      sender.feedFile(chunk)
      outgoing = sender.drainOutgoing()
      if (outgoing.length) this.callbacks.send(outgoing)
    }
    for (let event = sender.pollEvent(); event; event = sender.pollEvent()) {
      if (event === SenderEvent.FileComplete) {
        this.callbacks.onStatus(`已发送 ${this.sendFiles[this.sendIndex].name}`)
        this.sendIndex++
        if (this.sendIndex < this.sendFiles.length) this.startCurrentFile()
        else sender.finishSession()
      } else if (event === SenderEvent.SessionComplete) {
        this.sender = undefined
        this.sendFiles = []
        this.sendIndex = 0
        this.callbacks.onStatus('ZMODEM 发送完成')
      }
    }
    outgoing = sender.drainOutgoing()
    if (outgoing.length) this.callbacks.send(outgoing)
  }

  private startCurrentFile() {
    const file = this.sendFiles[this.sendIndex]
    this.sender?.startFile(safeFilename(file.name), file.size)
  }
}

function detectHeader(data: Uint8Array): { type: 'receive' | 'send'; offset: number } | undefined {
  const text = decoder.decode(data)
  // Hex ZMODEM headers begin with "** ZDLE B". ZRQINIT (00) means the
  // remote is sending; ZRINIT (01) means the remote is ready to receive.
  const receive = text.indexOf('**\x18B00')
  const send = text.indexOf('**\x18B01')
  if (receive >= 0 && (send < 0 || receive < send)) return { type: 'receive', offset: receive }
  if (send >= 0) return { type: 'send', offset: send }
  return undefined
}

function safeFilename(value: string) {
  const name = value.replaceAll('\\', '/').split('/').at(-1)?.replaceAll('\0', '') ?? ''
  return name === '.' || name === '..' || name === '' ? 'transfer.bin' : name
}

