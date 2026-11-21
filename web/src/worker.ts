import { generate } from './logic/sudoku'
import type { Level } from './config'

self.onmessage = (e: MessageEvent<Level>) => self.postMessage(generate(e.data))
