import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';

/** One provider's entry on the guide. */
interface ProviderGuideEntry {
  name: string;
  /** Empty for optional providers; the badge text for required ones. */
  requirement: string;
  /** What a key for this provider buys you, in the user's terms. */
  summary: string;
  uses: string[];
}

/**
 * Written from what the code actually calls, not from what the provider lineup
 * suggests. The OpenAI list in particular is the answer to "why does this one
 * say required when I chat with Claude" — every item on it runs regardless of
 * which model the chat itself uses.
 */
const PROVIDERS: ProviderGuideEntry[] = [
  {
    name: 'OpenAI',
    requirement: 'Required',
    summary: 'Used by the app itself, no matter which model you chat with.',
    uses: [
      'The welcome message in your first chat',
      'Creating a personality',
      'Memory extraction and scratchpad updates',
      'Conversation summaries at each checkpoint',
      'Naming chats and reading their mood',
      'Image generation, expressions and portraits',
      'Reading file attachments',
      'Semantic search over your memories',
      'Scheduling agent jobs',
    ],
  },
  {
    name: 'Anthropic',
    requirement: '',
    summary: 'Adds the Claude models to your picker.',
    uses: ['Chatting with any Claude model'],
  },
  {
    name: 'Google Gemini',
    requirement: '',
    summary: 'Adds the Gemini models to your picker.',
    uses: ['Chatting with any Gemini model'],
  },
  {
    name: 'z.ai',
    requirement: '',
    summary: 'Adds the GLM models to your picker.',
    uses: ['Chatting with any GLM model'],
  },
];

@Component({
  selector: 'app-providers-guide',
  standalone: true,
  imports: [RouterLink],
  templateUrl: './providers-guide.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
})
export class ProvidersGuideComponent {
  providers = PROVIDERS;
}
