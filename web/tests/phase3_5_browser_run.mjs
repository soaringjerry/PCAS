// Runs only this checkout, a new owned database, and exact child processes.
// No production URL/DSN, deployments, or process-name termination.
import { mkdtemp, readFile, access, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { spawn, spawnSync } from 'node:child_process'
import { setTimeout as delay } from 'node:timers/promises'

const cwd = resolve(import.meta.dirname, '..'), repo = resolve(cwd, '..')
const temporary = await mkdtemp(join(tmpdir(), 'phase3_5-browser-'))
const manifestPath = join(temporary, 'manifest.json')
console.log(`phase3.5 owned artifacts: ${temporary}`)
const localURL = 'http://127.0.0.1:18495'
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
  {
    controller = spawnChild('go', ['test', '-tags', 'phase35_browser', './internal/postgres', '-run', '^TestPhase35BrowserOwnedServer$', '-count=1', '-timeout', '10m', '-v'], repo,
      { ...process.env, PCAS_PHASE35_RUN_FINDINGS: '1', PCAS_PHASE35_BROWSER_MANIFEST: manifestPath }, 'backend.log')
    const deadline = Date.now() + 240000
    while (Date.now() < deadline) {
      if (controller.exitCode !== null) throw new Error('Owned controller did not become ready; inspect its finding or failure above')
      try { await access(manifestPath); manifest = JSON.parse(await readFile(manifestPath, 'utf8')); break } catch {}
      await delay(200)
    }
    if (!manifest?.ownedDisposable || new URL(manifest.backendURL).hostname !== '127.0.0.1') throw new Error('No valid owned disposable controller manifest')
  }
  // Pre-bundle dependencies so a cold Vite build does not consume the browser
  // interaction timeout.
  spawnSync(process.execPath, ['node_modules/vite/bin/vite.js', 'optimize'], { cwd, env: process.env, stdio: 'ignore' })
  vite = spawnChild(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '18495', '--strictPort'], cwd, process.env, 'vite.log')
  const deadline = Date.now() + 30000
  while (Date.now() < deadline) {
    if (vite.exitCode !== null) throw new Error('Local Vite child exited')
    try { if ((await fetch(localURL)).ok) break } catch {}
    await delay(200)
  }
  const env = { ...process.env, PCAS_TEST_BASE_URL: localURL }
  delete env.PCAS_PHASE35_BROWSER_MANIFEST
  Object.assign(env, { PCAS_PHASE35_RUN_FINDINGS: '1', PCAS_PHASE35_BROWSER_MANIFEST: manifestPath })
  const browser = spawnChild(process.execPath, ['node_modules/@playwright/test/cli.js', 'test', 'phase3_5_acceptance.spec.ts', '--output', join(temporary, 'playwright-results'), ...process.argv.slice(2)], cwd, env, 'browser.log')
  process.exitCode = await browser.done
} finally {
  if (manifest) await fetch(`${manifest.backendURL}/phase35-finish?key=${manifest.finishKey}`, { method: 'POST' }).catch(() => {})
  if (controller) await controller.done // Go timeout/cleanup owns only the created database.
  if (vite && vite.exitCode === null) { vite.kill('SIGTERM'); await vite.done }
  console.log(`phase3.5 browser artifacts: ${temporary}`)
}
