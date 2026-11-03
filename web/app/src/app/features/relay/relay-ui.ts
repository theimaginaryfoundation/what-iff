/**
 * Tailwind class strings shared by the relay tabs (Integrations → Buzz, Slack), the look the
 * Discord tab set.
 */
export const RELAY_UI = {
  primary:
    'rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50',
  secondary:
    'rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700',
  danger: 'rounded-lg bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700 disabled:opacity-50',
  link: 'text-indigo-600 underline dark:text-indigo-400',
  input:
    'w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-white disabled:opacity-60',
  chip: 'inline-flex items-center rounded-full px-2 py-0.5 font-medium',
  error: 'mt-1 text-xs text-red-700 dark:text-red-300',
  hint: 'text-xs text-gray-500 dark:text-gray-400',
  warningBox:
    'rounded-lg border border-amber-300 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-700/60 dark:bg-amber-900/20 dark:text-amber-200',
} as const;

/** How a status pill reads: working, paused by the owner, needs attention, or failing. */
export type RelayTone = 'good' | 'paused' | 'warn' | 'bad';

/** The pill colours for each tone. */
export const RELAY_TONE_CLASS: Record<RelayTone, string> = {
  good: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300',
  paused: 'bg-gray-100 text-gray-700 dark:bg-gray-900/40 dark:text-gray-300',
  warn: 'bg-amber-100 text-amber-900 dark:bg-amber-900/30 dark:text-amber-200',
  bad: 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300',
};
