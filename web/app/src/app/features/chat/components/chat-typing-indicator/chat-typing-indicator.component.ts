import { ChangeDetectionStrategy, Component, input } from '@angular/core';

@Component({
  selector: 'app-chat-typing-indicator',
  standalone: true,
  template: `
    @if (active()) {
      <div class="typing" aria-label="Assistant is typing">
        <span></span>
        <span></span>
        <span></span>
      </div>
    }
  `,
  styles: [`
    .typing {
      display: inline-flex;
      gap: 0.25rem;
      padding: 0.5rem 1rem;
    }

    span {
      animation: typing-dot 1.2s ease-in-out infinite;
      background: var(--color-text-muted);
      border-radius: 999px;
      height: 0.375rem;
      width: 0.375rem;
    }

    span:nth-child(2) {
      animation-delay: 0.15s;
    }

    span:nth-child(3) {
      animation-delay: 0.3s;
    }

    @media (prefers-reduced-motion: reduce) {
      span {
        animation: none;
      }
    }

    @keyframes typing-dot {
      0%, 60%, 100% { opacity: 0.35; transform: translateY(0); }
      30% { opacity: 1; transform: translateY(-0.125rem); }
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ChatTypingIndicatorComponent {
  readonly active = input(false);
}
