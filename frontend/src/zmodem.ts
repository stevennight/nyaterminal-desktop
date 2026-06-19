import { Receiver, ReceiverEvent, Sender, SenderEvent } from 'zmodem2'
import { api } from './bridge'

type Callbacks = {
  toTerminal: (data: Uint8Array) => void
  send: (data: Uint8Array) => void
  onStatus: (message: string) => void
  onActive?: (active: boolean) => void
}

const decoder = new TextDecoder('latin1')
const encoder = new TextEncoder()
const receiveHeaderPrefix = encoder.encode('**\x18B00')
const sendHeaderPrefix = encoder.encode('**\x18B01')

// zmodem2 deliberately exposes protocol state machines rather than terminal
// detection. This adapter keeps the protocol isolated from the terminal and
// only switches modes after a valid hex ZMODEM header prefix is observed.
export class ZmodemAdapter {
  private receiver?: Receiver
  private sender?: Sender
  private detector = new ZmodemHeaderDetector()
  private receiveName = ''
  private receiveHandle = ''
  private receiveSize = 0
  private receivedBytes = 0
  private receiveQueue = Promise.resolve()
  private receiverFlush = Promise.resolve()
  private receiverInput = new Uint8Array()
  private sendFiles: File[] = []
  private sendIndex = 0
  private selectingFile = false
  private senderFlush = Promise.resolve()

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
    const detection = this.detector.consume(data)
    if (detection.terminal.length) this.callbacks.toTerminal(detection.terminal)
    if (!detection.mode || !detection.protocol) {
      return
    }
    const protocolData = detection.protocol
    if (detection.mode === 'receive') {
      this.callbacks.onStatus('检测到远端 sz，正在接收文件')
      this.callbacks.onActive?.(true)
      this.receiver = new Receiver()
      this.consumeReceiver(protocolData)
    } else {
      this.selectFiles(protocolData)
    }
  }

  private consumeReceiver(data: Uint8Array) {
    if (!this.receiver) return
    this.receiverInput = concatBytes(this.receiverInput, data)
    this.receiverFlush = this.receiverFlush
      .then(() => this.flushReceiverInput())
      .catch(error => this.failReceive(error))
  }

  private async flushReceiverInput() {
    while (this.receiver && this.receiverInput.length) {
      const receiver = this.receiver
      const consumed = receiver.feedIncoming(this.receiverInput)
      if (consumed > 0) {
        this.receiverInput = this.receiverInput.slice(consumed)
      }
      const progressed = await this.flushReceiverState()
      if (consumed === 0 && !progressed) break
    }
    await this.flushReceiverState()
  }

  private async flushReceiverState() {
    const receiver = this.receiver
    if (!receiver) return false
    let progress = false
    while (true) {
      let localProgress = false
      let outgoing = receiver.drainOutgoing()
      while (outgoing.length) {
        progress = true
        localProgress = true
        this.callbacks.send(outgoing)
        outgoing = receiver.drainOutgoing()
      }
      for (let event = receiver.pollEvent(); event; event = receiver.pollEvent()) {
        progress = true
        localProgress = true
        if (event === ReceiverEvent.FileStart) {
          this.receiveName = safeFilename(receiver.getFileName())
          this.receiveSize = receiver.getFileSize()
          this.receivedBytes = 0
          this.receiveHandle = await api.BeginZmodemReceive(this.receiveName, this.receiveSize)
          if (!this.receiveHandle) throw new Error('已取消 ZMODEM 接收')
        } else if (event === ReceiverEvent.FileComplete) {
          await this.receiveQueue
          if (this.receiveHandle) await api.FinishZmodemReceive(this.receiveHandle)
          this.receiveHandle = ''
          this.callbacks.onStatus(`已接收 ${this.receiveName}`)
        } else if (event === ReceiverEvent.SessionComplete) {
          this.receiver = undefined
          this.callbacks.onActive?.(false)
          this.callbacks.onStatus('ZMODEM 接收完成')
        }
      }
      for (let fileData = receiver.drainFile(); fileData.length; fileData = receiver.drainFile()) {
        progress = true
        localProgress = true
        this.queueReceiveData(fileData)
      }
      outgoing = receiver.drainOutgoing()
      while (outgoing.length) {
        progress = true
        localProgress = true
        this.callbacks.send(outgoing)
        outgoing = receiver.drainOutgoing()
      }
      if (!this.receiver || !localProgress) break
    }
    return progress
  }

  private queueReceiveData(data: Uint8Array) {
    if (!data.length || !this.receiveHandle) return
    const chunk = data.slice()
    const handle = this.receiveHandle
    this.receivedBytes += chunk.length
    this.callbacks.onStatus(
      `正在接收 ${this.receiveName} · ${formatProgress(this.receivedBytes, this.receiveSize)}`
    )
    this.receiveQueue = this.receiveQueue.then(() =>
      api.WriteZmodemReceive(handle, Array.from(chunk))
    )
  }

  private async failReceive(error: unknown) {
    if (this.receiveHandle) await api.CancelZmodemReceive(this.receiveHandle).catch(() => undefined)
    this.receiveHandle = ''
    this.receiver = undefined
    this.callbacks.onActive?.(false)
    this.callbacks.onStatus(String(error))
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
      this.callbacks.onActive?.(true)
      this.sender.feedIncoming(initialData)
      this.startCurrentFile()
      this.queueSenderFlush()
    }
    input.click()
  }

  private consumeSender(data: Uint8Array) {
    if (!this.sender) return
    this.sender.feedIncoming(data)
    this.queueSenderFlush()
  }

  private queueSenderFlush() {
    this.senderFlush = this.senderFlush
      .then(() => this.flushSender())
      .catch(error => {
        this.sender = undefined
        this.callbacks.onActive?.(false)
        this.callbacks.onStatus(String(error))
      })
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
      this.callbacks.onStatus(
        `正在发送 ${file.name} · ${formatProgress(request.offset + chunk.length, file.size)}`
      )
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
        this.callbacks.onActive?.(false)
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

  async cancel() {
    this.callbacks.send(new Uint8Array(8).fill(0x18))
    if (this.receiveHandle) await api.CancelZmodemReceive(this.receiveHandle).catch(() => undefined)
    this.receiver = undefined
    this.sender = undefined
    this.receiveHandle = ''
    this.sendFiles = []
    this.sendIndex = 0
    this.detector.reset()
    this.callbacks.onActive?.(false)
    this.callbacks.onStatus('ZMODEM 传输已取消')
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

export class ZmodemHeaderDetector {
  private buffer = new Uint8Array()

  consume(data: Uint8Array): {
    terminal: Uint8Array
    mode?: 'receive' | 'send'
    protocol?: Uint8Array
  } {
    this.buffer = concatBytes(this.buffer, data)
    const header = detectHeader(this.buffer)
    if (header) {
      const terminal = this.buffer.slice(0, header.offset)
      const protocol = this.buffer.slice(header.offset)
      this.buffer = new Uint8Array()
      return { terminal, mode: header.type, protocol }
    }
    const retain = longestHeaderPrefixSuffix(this.buffer)
    if (retain === 0) {
      const terminal = this.buffer
      this.buffer = new Uint8Array()
      return { terminal }
    }
    if (this.buffer.length <= retain) return { terminal: new Uint8Array() }
    const terminal = this.buffer.slice(0, this.buffer.length - retain)
    this.buffer = this.buffer.slice(-retain)
    return { terminal }
  }

  reset() {
    this.buffer = new Uint8Array()
  }

  flush() {
    const terminal = this.buffer
    this.buffer = new Uint8Array()
    return terminal
  }
}

function safeFilename(value: string) {
  const name = value.replaceAll('\\', '/').split('/').at(-1)?.replaceAll('\0', '') ?? ''
  return name === '.' || name === '..' || name === '' ? 'transfer.bin' : name
}

function concatBytes(left: Uint8Array, right: Uint8Array) {
  const result = new Uint8Array(left.length + right.length)
  result.set(left)
  result.set(right, left.length)
  return result
}

function longestHeaderPrefixSuffix(buffer: Uint8Array) {
  const patterns = [receiveHeaderPrefix, sendHeaderPrefix]
  const maxLength = Math.min(buffer.length, receiveHeaderPrefix.length - 1)
  for (let retain = maxLength; retain > 0; retain--) {
    if (patterns.some(pattern => matchesPrefix(buffer, retain, pattern))) return retain
  }
  return 0
}

function matchesPrefix(buffer: Uint8Array, length: number, pattern: Uint8Array) {
  if (length > pattern.length) return false
  const offset = buffer.length - length
  for (let index = 0; index < length; index++) {
    if (buffer[offset + index] !== pattern[index]) return false
  }
  return true
}

function formatProgress(done: number, total: number) {
  if (total <= 0) return `${formatSize(done)}`
  return `${Math.min(100, Math.floor(done * 100 / total))}% · ${formatSize(done)} / ${formatSize(total)}`
}

function formatSize(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KB`
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MB`
  return `${(value / 1024 ** 3).toFixed(1)} GB`
}
