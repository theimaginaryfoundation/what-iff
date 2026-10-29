import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/** What an owner attestation is, under the attestation field (adding a community, or editing one). */
@Component({
  selector: 'app-buzz-attestation-help',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400" data-testid="buzz-attestation-help">
      A NIP-OA owner attestation is an <code>["auth", owner, conditions, sig]</code> tag, signed with your own Buzz key for this persona's
      key (<code class="break-all">{{ npub() }}</code
      >). With it, Buzz shows the persona as your agent and can let it in through your membership. Create it in your Buzz client and paste
      it here: What Iff never asks for your key.
    </p>
  `,
})
export class BuzzAttestationHelpComponent {
  readonly npub = input.required<string>();
}
