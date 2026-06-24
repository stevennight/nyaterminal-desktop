import type { Connection } from './types'

export function cloneConnectionDraft(connection: Connection): Connection {
  return {
    ...connection,
    id: '',
    credentialId: undefined,
    createdAt: undefined,
    updatedAt: undefined,
    tags: [...connection.tags],
  }
}
