import { describe, expect, it } from 'vitest'
import { CommandHistoryIndex, shouldStoreCommandHistory } from './commandHistorySearch'

function entry(id: string, connectionId: string, command: string, useCount: number, lastUsedAt: string) {
  return { id, connectionId, command, useCount, lastUsedAt }
}

describe('shouldStoreCommandHistory', () => {
  it('applies the same skip rules as the backend history writer', () => {
    expect(shouldStoreCommandHistory(true, ' ls', false, [])).toBeUndefined()
    expect(shouldStoreCommandHistory(true, 'ls', true, [])).toBeUndefined()
    expect(shouldStoreCommandHistory(false, 'ls', false, [])).toBeUndefined()
    expect(shouldStoreCommandHistory(true, 'secret', false, ['sec.*'])).toBeUndefined()
    expect(shouldStoreCommandHistory(true, 'ls -la', false, [])).toBe('ls -la')
  })
})

describe('CommandHistoryIndex', () => {
  it('prefers the best command candidate and deduplicates by command text', () => {
    const index = new CommandHistoryIndex()
    index.replace([
      entry('g1', '', 'ls', 2, '2024-01-01T00:00:00.000Z'),
      entry('c1', 'conn-1', 'ls', 2, '2024-01-01T00:00:00.000Z'),
      entry('g2', '', 'ls -la', 5, '2024-01-03T00:00:00.000Z'),
      entry('g3', '', 'git status', 1, '2024-01-02T00:00:00.000Z'),
    ])

    const result = index.suggest('conn-1', 'l')

    expect(result.map(item => item.command)).toEqual(['ls -la', 'ls'])
    expect(result[1].connectionId).toBe('conn-1')
  })

  it('updates both scoped and global history entries when recording a command', () => {
    const index = new CommandHistoryIndex()
    index.replace([
      entry('g1', '', 'ls', 1, '2024-01-01T00:00:00.000Z'),
      entry('c1', 'conn-1', 'ls', 1, '2024-01-01T00:00:00.000Z'),
    ])

    index.record('conn-1', 'ls', '2024-01-02T00:00:00.000Z')

    const result = index.suggest('conn-1', 'ls')

    expect(result).toHaveLength(1)
    expect(result[0]).toMatchObject({
      command: 'ls',
      useCount: 2,
      connectionId: 'conn-1',
      lastUsedAt: '2024-01-02T00:00:00.000Z',
    })
  })
})
