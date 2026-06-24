import { describe, expect, it } from 'vitest'
import { cloneConnectionDraft } from './connectionDraft'
import type { Connection } from './types'

describe('cloneConnectionDraft', () => {
  it('copies connection settings while clearing identity and credential links', () => {
    const source: Connection = {
      id: 'connection-1',
      groupId: 'group-1',
      name: 'prod',
      remark: 'jump host',
      host: 'example.com',
      port: 22,
      username: 'deploy',
      credentialId: 'credential-1',
      authentication: 'private_key',
      tags: ['tag-1', 'tag-2'],
      sortOrder: 7,
      encoding: 'utf-8',
      keepAliveSeconds: 30,
      connectTimeoutSeconds: 15,
      autoReconnect: true,
      legacyAlgorithms: true,
      syncSecrets: false,
      commandHistory: true,
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-02T00:00:00Z',
    }

    const clone = cloneConnectionDraft(source)

    expect(clone).toMatchObject({
      groupId: source.groupId,
      name: source.name,
      remark: source.remark,
      host: source.host,
      port: source.port,
      username: source.username,
      authentication: source.authentication,
      sortOrder: source.sortOrder,
      encoding: source.encoding,
      keepAliveSeconds: source.keepAliveSeconds,
      connectTimeoutSeconds: source.connectTimeoutSeconds,
      autoReconnect: source.autoReconnect,
      legacyAlgorithms: source.legacyAlgorithms,
      syncSecrets: source.syncSecrets,
      commandHistory: source.commandHistory,
    })
    expect(clone.id).toBe('')
    expect(clone.credentialId).toBeUndefined()
    expect(clone.createdAt).toBeUndefined()
    expect(clone.updatedAt).toBeUndefined()
    expect(clone.tags).toEqual(source.tags)
    expect(clone.tags).not.toBe(source.tags)
  })
})
