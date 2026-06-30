import type { CommandHistory } from './types'

const DEFAULT_LIMIT = 20
const MAX_LIMIT = 100
const MAX_COMMAND_LENGTH = 16 * 1024

export function shouldStoreCommandHistory(
  commandHistoryEnabled: boolean,
  command: string,
  privateSession: boolean,
  sensitiveRules: readonly string[],
) {
  if (!commandHistoryEnabled || privateSession) return undefined
  if (command.startsWith(' ')) return undefined
  const normalized = command.trim()
  if (!normalized || normalized.length > MAX_COMMAND_LENGTH) return undefined
  if (matchesSensitiveRule(normalized, sensitiveRules)) return undefined
  return normalized
}

export class CommandHistoryIndex {
  private readonly byId = new Map<string, CommandHistory>()
  private readonly byKey = new Map<string, string>()

  replace(entries: readonly CommandHistory[]) {
    this.byId.clear()
    this.byKey.clear()
    for (const entry of entries) {
      const copy = cloneHistory(entry)
      this.byId.set(copy.id, copy)
      this.byKey.set(historyKey(copy.connectionId, copy.command), copy.id)
    }
  }

  record(connectionId: string, command: string, lastUsedAt: string) {
    const scopes = connectionId ? [connectionId, ''] : ['']
    for (const scope of scopes) {
      this.upsert(scope, command, lastUsedAt)
    }
  }

  deleteCommand(connectionId: string, command: string) {
    const normalized = command.trim()
    if (!normalized) return
    for (const entry of Array.from(this.byId.values())) {
      if (entry.command !== normalized) continue
      if (entry.connectionId !== connectionId && entry.connectionId !== '') continue
      this.deleteById(entry.id)
    }
  }

  deleteRecords(ids: readonly string[]) {
    for (const id of ids) {
      this.deleteById(id)
    }
  }

  suggest(connectionId: string, prefix: string, limit = DEFAULT_LIMIT) {
    const normalizedPrefix = prefix.trim().toLowerCase()
    const resultByCommand = new Map<string, CommandHistory>()
    for (const entry of this.byId.values()) {
      if (entry.connectionId !== connectionId && entry.connectionId !== '') continue
      if (!entry.command.toLowerCase().startsWith(normalizedPrefix)) continue
      const existing = resultByCommand.get(entry.command)
      if (!existing || betterHistoryCandidate(entry, existing, connectionId)) {
        resultByCommand.set(entry.command, cloneHistory(entry))
      }
    }
    let result = Array.from(resultByCommand.values()).sort(compareHistoryCandidates)
    if (limit < 1 || limit > MAX_LIMIT) {
      limit = DEFAULT_LIMIT
    }
    if (result.length > limit) {
      result = result.slice(0, limit)
    }
    return result.map(cloneHistory)
  }

  private upsert(connectionId: string, command: string, lastUsedAt: string) {
    const key = historyKey(connectionId, command)
    const existingId = this.byKey.get(key)
    if (existingId) {
      const existing = this.byId.get(existingId)
      if (existing) {
        existing.useCount++
        existing.lastUsedAt = lastUsedAt
        return
      }
    }
    const entry: CommandHistory = {
      id: syntheticHistoryId(connectionId, command),
      connectionId,
      command,
      useCount: 1,
      lastUsedAt,
    }
    this.byId.set(entry.id, entry)
    this.byKey.set(key, entry.id)
  }

  private deleteById(id: string) {
    const existing = this.byId.get(id)
    if (!existing) return
    this.byId.delete(id)
    const key = historyKey(existing.connectionId, existing.command)
    if (this.byKey.get(key) === id) {
      this.byKey.delete(key)
    }
  }
}

function betterHistoryCandidate(candidate: CommandHistory, existing: CommandHistory, connectionId: string) {
  if (candidate.useCount !== existing.useCount) {
    return candidate.useCount > existing.useCount
  }
  if (candidate.lastUsedAt === existing.lastUsedAt) {
    return candidate.connectionId === connectionId && existing.connectionId === ''
  }
  return candidate.lastUsedAt > existing.lastUsedAt
}

function compareHistoryCandidates(left: CommandHistory, right: CommandHistory) {
  if (left.useCount !== right.useCount) {
    return right.useCount - left.useCount
  }
  if (left.lastUsedAt !== right.lastUsedAt) {
    return right.lastUsedAt.localeCompare(left.lastUsedAt)
  }
  return 0
}

function matchesSensitiveRule(command: string, rules: readonly string[]) {
  for (const rule of rules) {
    const trimmed = rule.trim()
    if (!trimmed) continue
    try {
      if (new RegExp(trimmed).test(command)) {
        return true
      }
    } catch {
      // Ignore invalid expressions and keep the history usable.
    }
  }
  return false
}

function cloneHistory(value: CommandHistory): CommandHistory {
  return { ...value }
}

function historyKey(connectionId: string, command: string) {
  return `${connectionId}\u0000${command}`
}

function syntheticHistoryId(connectionId: string, command: string) {
  return `local:${historyKey(connectionId, command)}`
}
