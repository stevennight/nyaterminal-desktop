import { api } from './bridge'
import { CommandHistoryIndex, shouldStoreCommandHistory } from './commandHistorySearch'
import type { CommandHistory } from './types'

type WorkerRequest =
  | { type: 'hydrate'; entries: CommandHistory[] }
  | { type: 'record'; connectionId: string; command: string; lastUsedAt: string }
  | { type: 'deleteCommand'; connectionId: string; command: string }
  | { type: 'deleteRecords'; ids: string[] }
  | { type: 'suggest'; connectionId: string; prefix: string; limit: number }

type WorkerResponse =
  | { id: number; ok: true; value?: CommandHistory[] }
  | { id: number; ok: false; error: string }

type PendingRequest = {
  resolve: (value: unknown) => void
  reject: (error: unknown) => void
}

const suggestionLimit = 20

class CommandHistorySuggestions {
  private readonly index = new CommandHistoryIndex()
  private worker?: Worker
  private workerFailed = false
  private nextRequestId = 1
  private readonly pending = new Map<number, PendingRequest>()
  private queue = Promise.resolve()
  private hasSnapshot = false

  loadCommandHistoryEntries() {
    return this.enqueue(async () => this.loadSnapshotFromBackend())
  }

  refreshCommandHistorySuggestions() {
    return this.loadCommandHistoryEntries().then(() => undefined)
  }

  recordCommandHistory(
    connectionId: string,
    commandHistoryEnabled: boolean,
    command: string,
    privateSession: boolean,
    sensitiveRules: readonly string[],
  ) {
    return this.enqueue(async () => {
      await api.AddCommandHistory(connectionId, command, privateSession)
      const normalized = shouldStoreCommandHistory(commandHistoryEnabled, command, privateSession, sensitiveRules)
      if (!normalized) return
      const lastUsedAt = new Date().toISOString()
      this.index.record(connectionId, normalized, lastUsedAt)
      await this.mirror({
        type: 'record',
        connectionId,
        command: normalized,
        lastUsedAt,
      })
    })
  }

  deleteCommandHistory(connectionId: string, command: string) {
    return this.enqueue(async () => {
      await api.DeleteCommandHistory(connectionId, command)
      this.index.deleteCommand(connectionId, command)
      await this.mirror({
        type: 'deleteCommand',
        connectionId,
        command,
      })
    })
  }

  deleteCommandHistoryRecords(ids: readonly string[]) {
    return this.enqueue(async () => {
      await api.DeleteCommandHistoryRecords(Array.from(ids))
      this.index.deleteRecords(ids)
      await this.mirror({
        type: 'deleteRecords',
        ids: Array.from(ids),
      })
    })
  }

  async suggestCommandHistory(connectionId: string, commandHistoryEnabled: boolean, prefix: string) {
    if (!commandHistoryEnabled) return []
    await this.waitForQueue()
    if (!this.hasSnapshot) {
      try {
        await this.loadCommandHistoryEntries()
      } catch {
        return this.index.suggest(connectionId, prefix, suggestionLimit)
      }
    }
    await this.waitForQueue()
    if (!this.worker || this.workerFailed) {
      return this.index.suggest(connectionId, prefix, suggestionLimit)
    }
    try {
      return await this.send<CommandHistory[]>({
        type: 'suggest',
        connectionId,
        prefix,
        limit: suggestionLimit,
      })
    } catch {
      this.disableWorker()
      return this.index.suggest(connectionId, prefix, suggestionLimit)
    }
  }

  private enqueue<T>(task: () => Promise<T>) {
    const next = this.queue.then(task, task)
    this.queue = next.then(() => undefined, () => undefined)
    return next
  }

  private async waitForQueue() {
    await this.queue
  }

  private async loadSnapshotFromBackend() {
    const entries = await api.ListCommandHistory()
    this.index.replace(entries)
    this.hasSnapshot = true
    await this.mirror({
      type: 'hydrate',
      entries,
    })
    return entries
  }

  private ensureWorker() {
    if (this.worker || this.workerFailed || typeof Worker === 'undefined') {
      return
    }
    try {
      this.worker = new Worker(new URL('./commandHistoryWorker.ts', import.meta.url), { type: 'module' })
      this.worker.onmessage = event => this.handleWorkerMessage(event.data as WorkerResponse)
      this.worker.onerror = () => {
        this.disableWorker()
      }
    } catch {
      this.disableWorker()
    }
  }

  private async mirror(message: WorkerRequest) {
    this.ensureWorker()
    if (!this.worker || this.workerFailed) return
    try {
      await this.send(message)
    } catch {
      this.disableWorker()
    }
  }

  private send<T = void>(message: WorkerRequest): Promise<T> {
    this.ensureWorker()
    if (!this.worker) {
      return Promise.reject(new Error('worker unavailable'))
    }
    const id = this.nextRequestId++
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, {
        resolve: value => resolve(value as T),
        reject,
      })
      this.worker?.postMessage({ id, ...message })
    })
  }

  private handleWorkerMessage(message: WorkerResponse) {
    const pending = this.pending.get(message.id)
    if (!pending) return
    this.pending.delete(message.id)
    if (message.ok) {
      pending.resolve(message.value)
      return
    }
    pending.reject(new Error(message.error))
  }

  private disableWorker() {
    if (this.worker) {
      this.worker.terminate()
    }
    this.worker = undefined
    this.workerFailed = true
    for (const pending of this.pending.values()) {
      pending.reject(new Error('worker unavailable'))
    }
    this.pending.clear()
  }
}

const commandHistorySuggestions = new CommandHistorySuggestions()

export function loadCommandHistoryEntries() {
  return commandHistorySuggestions.loadCommandHistoryEntries()
}

export function refreshCommandHistorySuggestions() {
  return commandHistorySuggestions.refreshCommandHistorySuggestions()
}

export function recordCommandHistory(
  connectionId: string,
  commandHistoryEnabled: boolean,
  command: string,
  privateSession: boolean,
  sensitiveRules: readonly string[],
) {
  return commandHistorySuggestions.recordCommandHistory(
    connectionId,
    commandHistoryEnabled,
    command,
    privateSession,
    sensitiveRules,
  )
}

export function deleteCommandHistory(connectionId: string, command: string) {
  return commandHistorySuggestions.deleteCommandHistory(connectionId, command)
}

export function deleteCommandHistoryRecords(ids: readonly string[]) {
  return commandHistorySuggestions.deleteCommandHistoryRecords(ids)
}

export function suggestCommandHistory(
  connectionId: string,
  commandHistoryEnabled: boolean,
  prefix: string,
) {
  return commandHistorySuggestions.suggestCommandHistory(connectionId, commandHistoryEnabled, prefix)
}
