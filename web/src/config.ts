// Board sizes and given-cell ranges come from shared/config.json (the Go server embeds the same file).
import shared from '../../shared/config.json'
import type { Shape } from './logic/sudoku'

export type Level = 'easy' | 'medium' | 'hard' | 'expert'
export type Size = 4 | 6 | 9

export const LEVELS: Record<Level, string> = { easy: 'Easy', medium: 'Medium', hard: 'Hard', expert: 'Expert' }
export const SIZES: Size[] = [4, 6, 9]

type SizeCfg = { boxRows: number; boxCols: number; name: string; minutes: string }
export const sizeInfo = (size: Size): SizeCfg => (shared.sizes as Record<string, SizeCfg>)[size]
export const shapeOf = (size: Size): Shape => ({ size, boxRows: sizeInfo(size).boxRows, boxCols: sizeInfo(size).boxCols })
export const givens = (size: Size, level: Level) =>
  (shared.givens as unknown as Record<string, Record<Level, [number, number]>>)[size][level]

// Generator gives up hunting for the exact range after this long and returns its best unique puzzle.
export const GENERATE_BUDGET_MS = 4000
// Main thread kills the worker after this long and falls back to generating inline.
export const WORKER_TIMEOUT_MS = 8000
