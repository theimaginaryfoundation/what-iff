import { ChangeDetectionStrategy, Component } from '@angular/core';

/** Any address that is not a share's. */
@Component({
  selector: 'share-not-found',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './share-message.scss',
  template: `
    <main class="share-message">
      <h1 class="share-message__title">Nothing here</h1>
      <p class="share-message__text">This address is not a shared file. Check the link you were sent.</p>
    </main>
  `,
})
export class ShareNotFoundComponent {}
