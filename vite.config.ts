import { defineConfig } from 'vite'
import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { join, relative } from 'node:path'

// Fills the precache list + version into dist/sw.js so the app works offline after the first visit.
const swManifest = () => ({
  name: 'sw-manifest',
  apply: 'build' as const,
  closeBundle() {
    const dist = 'dist'
    const files = (readdirSync(dist, { recursive: true, withFileTypes: true }) as import('node:fs').Dirent[])
      .filter((d) => d.isFile())
      .map((d) => '/' + relative(dist, join(d.parentPath, d.name)).replaceAll('\\', '/'))
      .filter((f) => f !== '/sw.js')
    const hash = createHash('sha1')
    for (const f of files) hash.update(readFileSync(join(dist, f)))
    const sw = join(dist, 'sw.js')
    writeFileSync(sw, readFileSync(sw, 'utf8')
      .replace('__VERSION__', 'sudoku-' + hash.digest('hex').slice(0, 10))
      .replace('__ASSETS__', JSON.stringify(files)))
  },
})

export default defineConfig({
  plugins: [swManifest()],
  worker: { format: 'es' },
  build: { target: 'es2022' },
})
