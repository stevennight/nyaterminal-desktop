function normalizeRemotePath(value: string) {
  const normalized = value.replace(/\\/g, '/')
  const absolute = normalized.startsWith('/')
  const segments: string[] = []
  for (const part of normalized.split('/')) {
    if (!part || part === '.') continue
    if (part === '..') {
      if (segments.length > 0) segments.pop()
      continue
    }
    segments.push(part)
  }
  const body = segments.join('/')
  if (absolute) return body ? `/${body}` : '/'
  return body || '.'
}

export function resolveRemotePath(base: string, value: string) {
  const trimmed = value.trim()
  if (!trimmed) return normalizeRemotePath(base)
  if (trimmed.startsWith('/')) return normalizeRemotePath(trimmed)
  const prefix = base.replace(/\/+$/, '')
  return normalizeRemotePath(`${prefix || '.'}/${trimmed}`)
}

export function joinRemotePath(parent: string, name: string) {
  return resolveRemotePath(parent, name)
}

export function parentRemotePath(value: string) {
  const normalized = normalizeRemotePath(value)
  if (normalized === '/' || normalized === '.') return normalized
  const body = normalized.replace(/\/+$/, '')
  const index = body.lastIndexOf('/')
  if (index < 0) return '.'
  if (index === 0) return '/'
  return body.slice(0, index)
}

export function normalizeLocalPath(value: string) {
  const normalized = value.replace(/\\/g, '/')
  const { prefix, absolute, segments } = splitLocalPath(normalized)
  const stack: string[] = []
  for (const part of segments) {
    if (!part || part === '.') continue
    if (part === '..') {
      if (stack.length > 0) {
        stack.pop()
        continue
      }
      if (!absolute) stack.push(part)
      continue
    }
    stack.push(part)
  }
  return buildLocalPath(prefix, absolute, stack)
}

export function resolveLocalPath(base: string, value: string) {
  const trimmed = value.trim()
  if (!trimmed) return normalizeLocalPath(base)
  if (isAbsoluteLocalPath(trimmed)) return normalizeLocalPath(trimmed)
  return normalizeLocalPath(`${normalizeLocalPath(base)}/${trimmed}`)
}

export function joinLocalDisplayPath(base: string, name: string) {
  return formatLocalDisplayPath(base, resolveLocalPath(base, name))
}

export function joinLocalRelativePath(parent: string, name: string) {
  return parent === '.' || parent === '' ? name : `${parent.replace(/\/+$/, '')}/${name}`
}

export function localRelativePath(root: string, value: string) {
  const normalizedRoot = normalizeLocalPath(root)
  const normalizedValue = normalizeLocalPath(value)
  if (normalizedRoot === normalizedValue) return '.'
  const prefix = normalizedRoot.endsWith('/') ? normalizedRoot : `${normalizedRoot}/`
  if (!normalizedValue.startsWith(prefix)) return null
  return normalizedValue.slice(prefix.length)
}

export function isLocalPathWithinRoot(root: string, value: string) {
  return localRelativePath(root, value) !== null
}

export function formatLocalDisplayPath(root: string, value: string) {
  const separator = root.includes('\\') ? '\\' : '/'
  return normalizeLocalPath(value).replace(/\//g, separator)
}

export function parentLocalDisplayPath(root: string, value: string) {
  const relative = localRelativePath(root, value)
  if (relative === null || relative === '.') {
    return formatLocalDisplayPath(root, root)
  }
  const parentRelative = relative.split('/').slice(0, -1).join('/')
  if (!parentRelative) return formatLocalDisplayPath(root, root)
  return formatLocalDisplayPath(root, resolveLocalPath(root, parentRelative))
}

export function isAbsoluteLocalPath(value: string) {
  const normalized = value.replace(/\\/g, '/')
  return /^[a-zA-Z]:\//.test(normalized) || normalized.startsWith('/') || normalized.startsWith('//')
}

function splitLocalPath(value: string) {
  if (value.startsWith('//')) {
    const parts = value.slice(2).split('/')
    const server = parts[0]
    const share = parts[1]
    if (server && share) {
      return {
        prefix: `//${server}/${share}`,
        absolute: true,
        segments: parts.slice(2),
      }
    }
    return { prefix: '//', absolute: true, segments: parts.slice(1) }
  }
  const drive = value.match(/^([a-zA-Z]:)(\/.*)?$/)
  if (drive) {
    return {
      prefix: drive[1],
      absolute: true,
      segments: (drive[2] ?? '/').split('/').filter(Boolean),
    }
  }
  if (value.startsWith('/')) {
    return {
      prefix: '/',
      absolute: true,
      segments: value.slice(1).split('/').filter(Boolean),
    }
  }
  return {
    prefix: '',
    absolute: false,
    segments: value.split('/').filter(Boolean),
  }
}

function buildLocalPath(prefix: string, absolute: boolean, segments: string[]) {
  const body = segments.join('/')
  if (prefix === '//') {
    return body ? `//${body}` : '//'
  }
  if (prefix.startsWith('//')) {
    return body ? `${prefix}/${body}` : prefix
  }
  if (prefix === '/') {
    return body ? `/${body}` : '/'
  }
  if (prefix) {
    return body ? `${prefix}/${body}` : `${prefix}/`
  }
  if (absolute) return body ? `/${body}` : '/'
  return body || '.'
}
