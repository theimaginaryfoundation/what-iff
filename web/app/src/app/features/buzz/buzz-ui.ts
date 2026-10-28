import { RELAY_TONE_CLASS, RELAY_UI, RelayTone } from '../relay/relay-ui';
import { BuzzAgentStatus, BuzzBindingStatus, BuzzCommunityStatus } from './buzz.service';

/** Tailwind class strings shared by the Buzz tab's parts (the relay tabs' common look). */
export const BUZZ_UI = RELAY_UI;

const STATUS: Record<BuzzAgentStatus | BuzzCommunityStatus | BuzzBindingStatus, { label: string; tone: RelayTone }> = {
  active: { label: 'Active', tone: 'good' },
  disabled: { label: 'Paused', tone: 'paused' },
  needs_access: { label: 'Needs access', tone: 'warn' },
  error: { label: 'Error', tone: 'bad' },
  broken: { label: 'Broken', tone: 'bad' },
};

/** "Active", "Needs access", "Paused"… for an identity, community or binding status. */
export function buzzStatusLabel(status: BuzzAgentStatus | BuzzCommunityStatus | BuzzBindingStatus): string {
  return STATUS[status]?.label ?? status;
}

/** The pill colours for a status. */
export function buzzStatusClass(status: BuzzAgentStatus | BuzzCommunityStatus | BuzzBindingStatus): string {
  return RELAY_TONE_CLASS[STATUS[status]?.tone ?? 'bad'];
}
