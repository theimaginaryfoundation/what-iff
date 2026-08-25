import { rankResults } from '../../../layout/command-palette/command-palette.helpers';

export interface SlashCommand {
  readonly id: string;
  readonly label: string;
  readonly description?: string;
  readonly keywords?: readonly string[];
}

export interface ParsedSlashCommand {
  readonly command: string | null;
  readonly args: readonly string[];
}

export function parseSlash(input: string): ParsedSlashCommand {
  const trimmed = input.trimStart();
  if (!trimmed.startsWith('/')) return { command: null, args: [] };

  const withoutSlash = trimmed.slice(1).trim();
  if (!withoutSlash) return { command: '', args: [] };

  const [command = '', ...args] = withoutSlash.split(/\s+/);
  return { command: command.toLowerCase(), args };
}

export function matchCommands(
  query: string,
  registry: readonly SlashCommand[],
): SlashCommand[] {
  const normalized = query.startsWith('/') ? query.slice(1) : query;
  const ranked = rankResults(
    registry.map(command => ({
      command,
      label: command.id,
      description: [command.label, command.description, ...(command.keywords ?? [])]
        .filter(Boolean)
        .join(' '),
    })),
    normalized,
  );
  return ranked.map(({ command }) => command);
}

/** Resolves a bare slash token (no args) to a registry entry by id or keyword alias. */
export function resolveSlashCommand(
  token: string,
  registry: readonly SlashCommand[],
): SlashCommand | null {
  const q = token.trim().toLowerCase();
  if (!q) return null;
  return (
    registry.find(
      command =>
        command.id === q ||
        command.label.toLowerCase() === q ||
        (command.keywords ?? []).some(keyword => keyword.toLowerCase() === q),
    ) ?? null
  );
}
