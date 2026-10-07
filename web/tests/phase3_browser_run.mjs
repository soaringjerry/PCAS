// Runs only this checkout, a new owned database, and exact child processes.
// No production URL/DSN, deployments, or process-name termination.
import { mkdtemp, readFile, access, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { spawn } from 'node:child_process'
import { setTimeout as delay } from 'node:timers/promises'

const cwd = resolve(import.meta.dirname, '..'), repo = resolve(cwd, '..')
const temporary = await mkdtemp(join(tmpdir(), 'phase3-browser-'))
const manifestPath = join(temporary, 'manifest.json')
const real = process.argv.includes('--real'), boundary = process.argv.includes('--boundary')
const localURL = 'http://127.0.0.1:18493'
const spawnChild = (command, args, cwd, env, log) => {
  const child = spawn(command, args, { cwd, env, stdio: ['ignore', 'pipe', 'pipe'] })
  let output = ''
  child.stdout.on('data', b => { output += b; process.stdout.write(b) })
  child.stderr.on('data', b => { output += b; process.stderr.write(b) })
  child.done = new Promise((resolve, reject) => {
    child.on('error', reject)
    child.on('exit', async code => { await writeFile(join(temporary, log), output); resolve(code) })
  })
  return child
}
let controller, vite, manifest
try {
  if (real) {
    controller = spawnChild('go', ['test', '-tags', 'phase3_browser', './internal/postgres', '-run', '^TestPhase3BrowserOwnedServer$', '-count=1', '-v'], repo,
      { ...process.env, GOCACHE: process.env.GOCACHE ?? '/tmp/pcas-p3-acceptance-go', PCAS_PHASE3_RUN_FINDINGS: '1', PCAS_PHASE3_BROWSER_MANIFEST: manifestPath }, 'backend.log')
    const deadline = Date.now() + 120000
    while (Date.now() < deadline) {
      if (controller.exitCode !== null) throw new Error('Owned controller did not become ready; inspect its finding or failure above')
      try { await access(manifestPath); manifest = JSON.parse(await readFile(manifestPath, 'utf8')); break } catch {}
      await delay(200)
    }
    if (!manifest?.ownedDisposable || new URL(manifest.backendURL).hostname !== '127.0.0.1') throw new Error('No valid owned disposable controller manifest')
  }
  vite = spawnChild(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '18493', '--strictPort'], cwd, process.env, 'vite.log')
  const deadline = Date.now() + 30000
  while (Date.now() < deadline) {
    if (vite.exitCode !== null) throw new Error('Local Vite child exited')
    try { if ((await fetch(localURL)).ok) break } catch {}
    await delay(200)
  }
  const env = { ...process.env, PCAS_TEST_BASE_URL: localURL }
  delete env.PCAS_PHASE3_BROWSER_MANIFEST
  delete env.PCAS_PHASE3_BROWSER_BOUNDARY
  if (real) Object.assign(env, { PCAS_PHASE3_RUN_FINDINGS: '1', PCAS_PHASE3_BROWSER_MANIFEST: manifestPath })
  if (boundary) Object.assign(env, { PCAS_PHASE3_RUN_FINDINGS: '1', PCAS_PHASE3_BROWSER_BOUNDARY: '1' })
  const browser = spawnChild(process.execPath, ['node_modules/@playwright/test/cli.js', 'test', 'phase3_acceptance.spec.ts', ...(boundary ? ['--grep', 'T5 boundary'] : [])], cwd, env, 'browser.log')
  process.exitCode = await browser.done
} finally {
  if (manifest) await fetch(`${manifest.backendURL}/phase3-finish?key=${manifest.finishKey}`, { method: 'POST' }).catch(() => {})
  if (controller) await controller.done // Go timeout/cleanup owns only the created database.
  if (vite && vite.exitCode === null) { vite.kill('SIGTERM'); await vite.done }
  console.log(`phase3 browser artifacts: ${temporary}`)
}
