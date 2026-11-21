import { generate } from './logic/sudoku'
import { givens, shapeOf, type Level, type Size } from './config'

self.onmessage = (e: MessageEvent<[Size, Level]>) => {
  const [size, level] = e.data
  self.postMessage(generate(shapeOf(size), givens(size, level)))
}
