import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

type PackageJson = {
  version?: string
  dependencies?: Record<string, string>
}

type WailsConfig = {
  name?: string
  info?: {
    productName?: string
    productVersion?: string
  }
}

const configDir = dirname(fileURLToPath(import.meta.url))

function readJson<T>(path: string): T {
  return JSON.parse(readFileSync(path, 'utf8')) as T
}

function parseDirectGoDependencies(goMod: string) {
  const dependencies: Array<{ name: string; version: string }> = []
  let inRequireBlock = false

  for (const rawLine of goMod.split(/\r?\n/)) {
    const line = rawLine.trim()
    if (!line || line.startsWith('//')) continue
    if (line === 'require (') {
      inRequireBlock = true
      continue
    }
    if (inRequireBlock && line === ')') {
      break
    }
    if (!inRequireBlock || line.includes('// indirect')) continue
    const match = line.match(/^(\S+)\s+(\S+)/)
    if (match) dependencies.push({ name: match[1], version: match[2] })
  }

  return dependencies
}

const packageJson = readJson<PackageJson>(resolve(configDir, 'package.json'))
const wailsConfig = readJson<WailsConfig>(resolve(configDir, '..', 'wails.json'))
const buildDate = new Date()
const buildDateTime = buildDate.toISOString()
const buildNumber = buildDateTime.replace(/[-:TZ.]/g, '').slice(0, 14)
const frontendLibraries = Object.entries(packageJson.dependencies ?? {}).map(([name, version]) => ({
  name,
  version,
  source: 'frontend',
}))
const goLibraries = parseDirectGoDependencies(readFileSync(resolve(configDir, '..', 'go.mod'), 'utf8')).map(item => ({
  ...item,
  source: 'go',
}))

export default defineConfig({
  plugins: [react()],
  define: {
    __APP_INFO__: JSON.stringify({
      name: wailsConfig.info?.productName ?? wailsConfig.name ?? 'NyaTerminal',
      version: wailsConfig.info?.productVersion ?? packageJson.version ?? '0.1.0',
      buildNumber,
      buildDateTime,
      libraries: [...frontendLibraries, ...goLibraries],
    }),
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    target: 'es2022'
  },
  server: {
    host: '127.0.0.1',
    strictPort: true
  }
})
