/**
 * The MCP Apps host style variables (spec 2026-01-26, `HostContext.styles.variables`): the chat's
 * theme expressed in the names views read, so a view that uses them looks like part of the app. The
 * frame has an opaque origin and the app's stylesheets don't reach it, so values are resolved here
 * from the live theme tokens. Spacing is deliberately not part of the spec.
 */
export function mcpHostStyleVariables(tokens: (name: string) => string): Record<string, string> {
  const t = (name: string, fallback = ''): string => tokens(name).trim() || fallback;
  const tint = (color: string, percent: number): string => `color-mix(in srgb, ${color} ${percent}%, transparent)`;

  const card = t('--bg-card', '#ffffff');
  const surface = t('--bg-surface', '#e5e7eb');
  const base = t('--bg-base', card);
  const primary = t('--text-primary', '#111827');
  const secondary = t('--text-secondary', '#374151');
  const muted = t('--text-muted', '#6b7280');
  const border = t('--border', '#d1d5db');
  const borderHover = t('--border-hover', border);
  const focus = t('--focus-ring', t('--accent', '#8c87ff'));
  const info = t('--info', '#2563eb');
  const danger = t('--danger', '#dc2626');
  const success = t('--success', '#16a34a');
  const warning = t('--warning', '#d97706');

  return {
    '--color-background-primary': card,
    '--color-background-secondary': base,
    '--color-background-tertiary': surface,
    '--color-background-inverse': primary,
    '--color-background-ghost': 'transparent',
    '--color-background-info': tint(info, 12),
    '--color-background-danger': tint(danger, 12),
    '--color-background-success': tint(success, 12),
    '--color-background-warning': tint(warning, 12),
    '--color-background-disabled': surface,

    '--color-text-primary': primary,
    '--color-text-secondary': secondary,
    '--color-text-tertiary': muted,
    '--color-text-inverse': card,
    '--color-text-info': info,
    '--color-text-danger': danger,
    '--color-text-success': success,
    '--color-text-warning': warning,
    '--color-text-disabled': muted,
    '--color-text-ghost': secondary,

    '--color-border-primary': border,
    '--color-border-secondary': borderHover,
    '--color-border-tertiary': border,
    '--color-border-inverse': card,
    '--color-border-ghost': 'transparent',
    '--color-border-info': info,
    '--color-border-danger': danger,
    '--color-border-success': success,
    '--color-border-warning': warning,
    '--color-border-disabled': border,

    '--color-ring-primary': focus,
    '--color-ring-secondary': borderHover,
    '--color-ring-inverse': card,
    '--color-ring-info': info,
    '--color-ring-danger': danger,
    '--color-ring-success': success,
    '--color-ring-warning': warning,

    // The chat's own web font can't load inside the frame, so a view falls through to the system stack.
    '--font-sans': t('--font-sans', 'system-ui, sans-serif') ,
    '--font-mono': t('--font-mono', 'ui-monospace, SFMono-Regular, Menlo, monospace'),

    '--font-weight-normal': '400',
    '--font-weight-medium': '500',
    '--font-weight-semibold': '600',
    '--font-weight-bold': '700',

    '--font-text-xs-size': '0.75rem',
    '--font-text-sm-size': '0.8125rem',
    '--font-text-md-size': '0.875rem',
    '--font-text-lg-size': '1rem',
    '--font-heading-xs-size': '0.875rem',
    '--font-heading-sm-size': '1rem',
    '--font-heading-md-size': '1.125rem',
    '--font-heading-lg-size': '1.25rem',
    '--font-heading-xl-size': '1.5rem',
    '--font-heading-2xl-size': '1.875rem',
    '--font-heading-3xl-size': '2.25rem',

    '--font-text-xs-line-height': '1rem',
    '--font-text-sm-line-height': '1.125rem',
    '--font-text-md-line-height': '1.25rem',
    '--font-text-lg-line-height': '1.5rem',
    '--font-heading-xs-line-height': '1.25rem',
    '--font-heading-sm-line-height': '1.5rem',
    '--font-heading-md-line-height': '1.625rem',
    '--font-heading-lg-line-height': '1.75rem',
    '--font-heading-xl-line-height': '2rem',
    '--font-heading-2xl-line-height': '2.25rem',
    '--font-heading-3xl-line-height': '2.5rem',

    '--border-radius-xs': '0.25rem',
    '--border-radius-sm': '0.375rem',
    '--border-radius-md': '0.5rem',
    '--border-radius-lg': '0.75rem',
    '--border-radius-xl': '1rem',
    '--border-radius-full': '9999px',
    '--border-width-regular': '1px',

    '--shadow-hairline': `0 0 0 1px ${border}`,
    '--shadow-sm': t('--card-shadow', '0 1px 2px rgb(0 0 0 / 0.08)'),
    '--shadow-md': t('--card-shadow-hover', '0 4px 12px rgb(0 0 0 / 0.1)'),
    '--shadow-lg': '0 12px 32px rgb(0 0 0 / 0.16)',
  };
}

/** A tool's name for a header: `show_counter` reads "Show counter". */
export function toolDisplayName(name: string): string {
  const words = name.replace(/[_-]+/g, ' ').replace(/([a-z0-9])([A-Z])/g, '$1 $2').trim();
  if (!words) return 'App';
  return words.charAt(0).toUpperCase() + words.slice(1);
}
