import { type ChildProcess, spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'

import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

const root = fileURLToPath(new URL('..', import.meta.url))
const bridgePort = Number(process.env.DEV_BRIDGE_PORT ?? 5175)
const bridgeUrl = `http://127.0.0.1:${bridgePort}`

/**
 * Runs dev/bridge.py next to the Vite server. A main.py change restarts the bridge; a Go change
 * rebuilds and restarts the backend.
 */
function devBridge(): Plugin {
  let bridge: ChildProcess | null = null

  const start = () => {
    bridge = spawn('python3', ['dev/bridge.py'], {
      cwd: root,
      stdio: 'inherit',
      env: { ...process.env, DEV_BRIDGE_PORT: String(bridgePort) },
    })
  }
  const stop = () =>
    new Promise<void>((resolve) => {
      if (!bridge || bridge.exitCode !== null) return resolve()
      bridge.once('exit', () => resolve())
      bridge.kill('SIGTERM')
    })

  return {
    name: 'dev-bridge',
    configureServer(server) {
      start()
      server.watcher.add([`${root}main.py`, `${root}backend`])
      server.watcher.on('change', async (file) => {
        if (file === `${root}main.py`) {
          await stop()
          start()
        } else if (file.startsWith(`${root}backend/`) && file.endsWith('.go')) {
          await fetch(`${bridgeUrl}/dev/rebuild`, { method: 'POST' }).catch(() => undefined)
        }
      })
      server.httpServer?.once('close', () => void stop())
      process.once('exit', () => bridge?.kill('SIGTERM'))
    },
  }
}

export default defineConfig({
  root: fileURLToPath(new URL('.', import.meta.url)),
  plugins: [react(), devBridge()],
  resolve: {
    alias: {
      '@decky/api': fileURLToPath(new URL('./mocks/decky-api.ts', import.meta.url)),
      '@decky/ui': fileURLToPath(new URL('./mocks/decky-ui.tsx', import.meta.url)),
    },
  },
  server: {
    port: 5174,
    proxy: {
      '/api': bridgeUrl,
      '/events': bridgeUrl,
      '/dev/': bridgeUrl,
    },
  },
})
