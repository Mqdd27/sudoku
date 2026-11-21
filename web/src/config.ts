// Number of given (pre-filled) cells per level. Edit freely; min/max inclusive.
export const LEVELS = {
  easy: { label: 'Easy', min: 40, max: 45 },
  medium: { label: 'Medium', min: 32, max: 36 },
  hard: { label: 'Hard', min: 28, max: 31 },
  expert: { label: 'Expert', min: 24, max: 27 },
} as const

export type Level = keyof typeof LEVELS

// Generator gives up hunting for the exact range after this long and returns its best unique puzzle.
export const GENERATE_BUDGET_MS = 4000
// Main thread kills the worker after this long and falls back to generating inline.
export const WORKER_TIMEOUT_MS = 8000
