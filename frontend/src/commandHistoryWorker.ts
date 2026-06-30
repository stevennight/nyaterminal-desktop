import { CommandHistoryIndex } from './commandHistorySearch'
import type { CommandHistory } from './types'

type WorkerRequest =
  | { id: number; type: 'hydrate'; entries: CommandHistory[] }
  | { id: number; type: 'record'; connectionId: string; command: string; lastUsedAt: string }
  | { id: number; type: 'deleteCommand'; connectionId: string; command: string }
  | { id: number; type: 'deleteRecords'; ids: string[] }
  | { id: number; type: 'suggest'; connectionId: string; prefix: string; limit: number }

type WorkerResponse =
  | { id: number; ok: true; value?: CommandHistory[] }
  | { id: number; ok: false; error: string }

const workerScope = self as {
  onmessage: ((event: MessageEvent<WorkerRequest>) => void) | null
  postMessage: (value: WorkerResponse) => void
}
const index = new CommandHistoryIndex()

workerScope.onmessage = (event: MessageEvent<WorkerRequest>) => {
  const message = event.data
  try {
    if (message.type === 'hydrate') {
      index.replace(message.entries)
      replyOk(message.id)
      return
    }
    if (message.type === 'record') {
      index.record(message.connectionId, message.command, message.lastUsedAt)
      replyOk(message.id)
      return
    }
    if (message.type === 'deleteCommand') {
      index.deleteCommand(message.connectionId, message.command)
      replyOk(message.id)
      return
    }
    if (message.type === 'deleteRecords') {
      index.deleteRecords(message.ids)
      replyOk(message.id)
      return
    }
    if (message.type === 'suggest') {
      replyOk(message.id, index.suggest(message.connectionId, message.prefix, message.limit))
    }
  } catch (error) {
    replyError(message.id, error)
  }
}

function replyOk(id: number, value?: CommandHistory[]) {
  const response: WorkerResponse = { id, ok: true, value }
  workerScope.postMessage(response)
}

function replyError(id: number, error: unknown) {
  const response: WorkerResponse = { id, ok: false, error: error instanceof Error ? error.message : String(error) }
  workerScope.postMessage(response)
}
