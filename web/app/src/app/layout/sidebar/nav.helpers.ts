import { Type } from '@angular/core';
import {
  BoltIconComponent,
  BrainIconComponent,
  ChatIconComponent,
  ClockIconComponent,
  ImageIconComponent,
  SmileIconComponent,
  UsersIconComponent,
  WrenchIconComponent,
} from '../../shared/ui/icons/icons';

/**
 * Sidebar nav modes. Mirrors Concept D's left-sidebar `navMode` state. Mode is
 * derived from the active route by `isConfigPath()` so the URL stays the
 * source of truth.
 */
export type NavMode = 'app' | 'config';

/**
 * Single nav-list entry. `route` is an absolute path matching `app.routes.ts`.
 * Components render `IconComponent` directly via Angular's `<ng-container>` /
 * dynamic component creation; helpers stay framework-agnostic data-only.
 */
export interface NavItem {
  readonly id: string;
  readonly label: string;
  /** What the section is for; shown as the nav tooltip in every sidebar state. */
  readonly hint: string;
  readonly route: string;
  readonly icon: Type<unknown>;
}

/** What the mode switch leads to, shared by the expanded and collapsed sidebar. */
export const CONFIG_MODE_HINT = 'Memories, modes, skills, tools and jobs';
export const APP_MODE_HINT = 'Back to chat, personalities and gallery';

/**
 * Tooltip for a nav control. When the label is on screen the hint says what it's for; when the
 * control is icon-only the tooltip is its only name, so it leads with the label.
 */
export function navTooltip(label: string, hint: string, labelVisible: boolean): string {
  if (labelVisible) return hint;
  return `${label}: ${hint.charAt(0).toLowerCase()}${hint.slice(1)}`;
}

const APP_NAV_ITEMS: ReadonlyArray<NavItem> = [
  { id: 'chat', label: 'Chat', hint: 'Your threads. Click again to open or close the Thread Manager', route: '/chat', icon: ChatIconComponent },
  { id: 'personalities', label: 'Personalities', hint: 'The characters you talk to: prompt, portraits and notes', route: '/personality', icon: UsersIconComponent },
  { id: 'gallery', label: 'Gallery', hint: "Images you've generated or imported, and expressions", route: '/gallery', icon: ImageIconComponent },
];

const CONFIG_BASE_ITEMS: ReadonlyArray<NavItem> = [
  { id: 'memories', label: 'Memories', hint: 'What your personalities remember across threads', route: '/memories', icon: BrainIconComponent },
  { id: 'modes', label: 'Modes', hint: 'Sets of instructions, skills and a model a thread can switch into', route: '/mode', icon: SmileIconComponent },
  { id: 'skills', label: 'Skills', hint: 'Saved instructions you add to a message with /', route: '/skills', icon: BoltIconComponent },
  { id: 'tools', label: 'Tools', hint: 'Connectors (MCP servers) and webhook tokens', route: '/integrations', icon: WrenchIconComponent },
  { id: 'jobs', label: 'Jobs', hint: 'Prompts your personalities run on a schedule', route: '/agent-jobs', icon: ClockIconComponent },
];

/**
 * Slot for Phase 13 (Tools) and Phase 14 (Features Toggles) to register their
 * own config-mode entries without forking this module. Stays empty in Phase 03.
 */
export const extraConfigItems: ReadonlyArray<NavItem> = [];

/**
 * App-mode primary nav: chat / personalities / gallery.
 */
export function appNavItems(): ReadonlyArray<NavItem> {
  return APP_NAV_ITEMS;
}

/**
 * Config-mode nav. Jobs is always included. `extraConfigItems` is appended last.
 */
export function configNavItems(): ReadonlyArray<NavItem> {
  const items: NavItem[] = [...CONFIG_BASE_ITEMS];
  items.push(...extraConfigItems);
  return items;
}

/**
 * Returns true when the given URL belongs to a config-mode page. Matches the
 * top path segment so `/memories/<id>` and `/agent-jobs/<id>` are still config.
 *
 * Future config routes (`/tools`, `/features`) are pre-registered so Phases
 * 13 and 14 can ship leaf views without touching this helper again.
 */
export function isConfigPath(url: string): boolean {
  if (!url) return false;
  const path = stripQueryAndHash(url);
  const segments = path.split('/').filter(Boolean);
  if (segments.length === 0) return false;
  return CONFIG_TOP_SEGMENTS.has(segments[0]);
}

const CONFIG_TOP_SEGMENTS: ReadonlySet<string> = new Set([
  'memories',
  'memory',
  'mode',
  'skills',
  'integrations',
  'rituals',
  'ritual',
  'agent-jobs',
  'tools',
  'features',
]);

function stripQueryAndHash(url: string): string {
  const q = url.indexOf('?');
  const h = url.indexOf('#');
  let cut = url.length;
  if (q !== -1) cut = Math.min(cut, q);
  if (h !== -1) cut = Math.min(cut, h);
  return url.slice(0, cut);
}
