import { RELAY_TONE_CLASS, RelayTone } from '../relay/relay-ui';
import { SlackAppStatus, SlackBindingStatus } from './slack.service';

const STATUS: Record<SlackAppStatus | SlackBindingStatus, { label: string; tone: RelayTone }> = {
  active: { label: 'Active', tone: 'good' },
  invalid_token: { label: 'Invalid token', tone: 'bad' },
  disabled: { label: 'Paused', tone: 'paused' },
  broken: { label: 'Broken', tone: 'bad' },
};

/** "Active", "Invalid token", "Paused"… for an app or binding status. */
export function slackStatusLabel(status: SlackAppStatus | SlackBindingStatus): string {
  return STATUS[status]?.label ?? status;
}

/** The pill colours for a status. */
export function slackStatusClass(status: SlackAppStatus | SlackBindingStatus): string {
  return RELAY_TONE_CLASS[STATUS[status]?.tone ?? 'bad'];
}
