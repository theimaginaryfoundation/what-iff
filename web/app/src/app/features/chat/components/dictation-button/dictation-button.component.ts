import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, DestroyRef, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { SpeechService } from '../../../../core/services/speech.service';
import { apiErrorMessage, hasErrorCode } from '../../../../core/utils/api-error.helpers';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';
import { ChatSendGate } from '../../services/chat-send-gate';
import { ClipRecorder, RecorderDeps, RecordingCancelled, browserRecorderDeps, canDictate } from './clip-recorder';

/** Where a dictation is. */
export type DictationState = 'idle' | 'starting' | 'recording' | 'transcribing';

/**
 * The composer's microphone (ADR 0x052 §10): press to record, press again to transcribe; Escape
 * discards. The transcript goes to the composer, never straight to the chat: the person reads and
 * edits it first. Shown only with a dictation model and a browser that can record.
 */
@Component({
  selector: 'app-dictation-button',
  standalone: true,
  imports: [TooltipDirective],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { '(document:keydown.escape)': 'onEscape($event)' },
  template: `
    @if (available()) {
      <span class="dictation" [class.dictation--recording]="state() === 'recording'">
        @if (state() === 'recording') {
          <span class="dictation__meter" aria-hidden="true" [style.--level]="level()"></span>
          <span class="dictation__time" aria-live="off" data-testid="dictation-time">{{ time() }}</span>
        }
        <button
          type="button"
          class="dictation__button"
          data-testid="dictation-button"
          [disabled]="startDisabled()"
          [attr.aria-pressed]="state() === 'recording'"
          [attr.aria-label]="label()"
          [uiTooltip]="label()"
          (click)="toggle()"
        >
          @if (state() === 'transcribing') {
            <span class="dictation__spinner" aria-hidden="true"></span>
          } @else if (state() === 'recording') {
            <span class="dictation__stop" aria-hidden="true"></span>
          } @else {
            <svg
              width="14"
              height="14"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              aria-hidden="true"
            >
              <rect x="5.5" y="1.5" width="5" height="8.5" rx="2.5" />
              <path d="M3 7.5a5 5 0 0 0 10 0M8 12.5v2" />
            </svg>
          }
        </button>
        @if (error()) {
          <span class="dictation__error" role="alert" data-testid="dictation-error">{{ error() }}</span>
        }
      </span>
    }
  `,
  styles: `
    :host {
      display: contents;
    }
    .dictation {
      align-items: center;
      display: inline-flex;
      gap: 0.4rem;
      position: relative;
    }
    .dictation__button {
      align-items: center;
      background: transparent;
      border: 1px solid var(--border);
      border-radius: 0.4375rem;
      color: var(--text-secondary);
      cursor: pointer;
      display: inline-flex;
      flex-shrink: 0;
      height: 1.875rem;
      justify-content: center;
      width: 1.875rem;
    }
    .dictation__button:hover:not(:disabled) {
      color: var(--text-primary);
    }
    .dictation__button:disabled {
      cursor: default;
      opacity: 0.5;
    }
    .dictation--recording .dictation__button {
      background: var(--color-danger);
      border-color: var(--color-danger);
      color: white;
    }
    .dictation__stop {
      background: currentColor;
      border-radius: 0.15rem;
      height: 0.6rem;
      width: 0.6rem;
    }
    .dictation__spinner {
      animation: dictation-spin 0.8s linear infinite;
      border: 2px solid currentColor;
      border-radius: 50%;
      border-right-color: transparent;
      height: 0.8rem;
      width: 0.8rem;
    }
    .dictation__meter {
      background: var(--color-danger);
      border-radius: 999px;
      height: 0.5rem;
      opacity: calc(0.35 + var(--level, 0) * 0.65);
      transform: scale(calc(0.6 + var(--level, 0) * 0.8));
      transition: transform 80ms linear;
      width: 0.5rem;
    }
    .dictation__time {
      color: var(--text-secondary);
      font-size: 0.75rem;
      font-variant-numeric: tabular-nums;
    }
    .dictation__error {
      background: var(--bg-secondary);
      border: 1px solid var(--border);
      border-radius: 0.4rem;
      bottom: calc(100% + 0.4rem);
      color: var(--text-primary);
      font-size: 0.75rem;
      max-width: 16rem;
      padding: 0.35rem 0.5rem;
      position: absolute;
      right: 0;
      width: max-content;
      z-index: 5;
    }
    @keyframes dictation-spin {
      to {
        transform: rotate(360deg);
      }
    }
    @media (prefers-reduced-motion: reduce) {
      .dictation__spinner {
        animation-duration: 2s;
      }
      .dictation__meter {
        transition: none;
      }
    }
  `,
})
export class DictationButtonComponent implements OnInit {
  private readonly speech = inject(SpeechService);
  private readonly sendGate = inject(ChatSendGate, { optional: true });
  private readonly destroyRef = inject(DestroyRef);

  readonly chatId = input<string | null>(null);
  readonly disabled = input(false);
  /** The transcript, for the composer to insert. */
  readonly transcribed = output<string>();
  /** Recording started; anything playing aloud stops. */
  readonly recordingStarted = output<void>();

  /** Stands in for the browser in tests. */
  deps: RecorderDeps = browserRecorderDeps;
  browserCanDictate: () => boolean = canDictate;

  readonly state = signal<DictationState>('idle');
  readonly error = signal('');
  readonly elapsed = signal(0);
  readonly level = signal(0);

  readonly available = computed(() => !!this.speech.dictation()?.available && this.browserCanDictate());
  readonly blocked = computed(() => this.sendGate?.blocked() ?? false);
  /** Stopping is always allowed; only starting waits on the composer and the credit gate. */
  readonly startDisabled = computed(() => {
    const state = this.state();
    return state === 'starting' || state === 'transcribing' || (state === 'idle' && (this.disabled() || this.blocked()));
  });
  readonly maxSeconds = computed(() => this.speech.dictation()?.max_seconds ?? 300);
  readonly time = computed(() => {
    const s = Math.floor(this.elapsed());
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
  });
  readonly label = computed(() => {
    switch (this.state()) {
      case 'recording':
        return 'Stop and transcribe (Escape discards)';
      case 'transcribing':
        return 'Transcribing…';
      default:
        return 'Dictate a message';
    }
  });

  /** The recording, from the moment the microphone is asked for: a discard then gives it back. */
  private recorder: ClipRecorder | null = null;
  /** The chat the recording is for: a transcript that comes back after a chat switch is dropped. */
  private recordingChatId: string | null = null;
  private ticker: ReturnType<typeof setInterval> | null = null;

  private destroyed = false;

  constructor() {
    this.destroyRef.onDestroy(() => {
      this.destroyed = true;
      this.discard();
    });
  }

  ngOnInit(): void {
    void this.speech.loadDictation();
  }

  toggle(): void {
    if (this.state() === 'recording') {
      void this.finish();
    } else if (this.state() === 'idle') {
      void this.begin();
    }
  }

  onEscape(event: Event): void {
    // Something nearer the focus (a dialog, a menu) already took this Escape.
    if (event.defaultPrevented) return;
    if (this.state() === 'recording' || this.state() === 'starting') {
      event.preventDefault();
      event.stopPropagation();
      this.discard();
    }
  }

  async begin(): Promise<void> {
    const model = this.speech.dictation();
    if (!model?.available) return;
    this.error.set('');
    this.state.set('starting');
    const recorder = new ClipRecorder(this.deps, this.maxSeconds() + 1);
    this.recorder = recorder;
    this.recordingChatId = this.chatId();
    try {
      await recorder.start();
    } catch (err) {
      recorder.cancel();
      if (err instanceof RecordingCancelled || this.recorder !== recorder) {
        return; // discarded (Escape, or the composer went away) while the browser was asking
      }
      this.recorder = null;
      this.state.set('idle');
      this.error.set(
        err instanceof DOMException && (err.name === 'NotAllowedError' || err.name === 'SecurityError')
          ? 'Allow the microphone to dictate.'
          : "Couldn't start the microphone.",
      );
      return;
    }
    if (this.recorder !== recorder) {
      recorder.cancel();
      return;
    }
    this.elapsed.set(0);
    this.state.set('recording');
    this.recordingStarted.emit();
    this.ticker = setInterval(() => this.tick(), 100);
  }

  private tick(): void {
    if (!this.recorder) return;
    const elapsed = this.recorder.elapsed();
    this.elapsed.set(elapsed);
    this.level.set(this.recorder.level());
    if (elapsed >= this.maxSeconds()) {
      void this.finish(true);
    }
  }

  async finish(atLimit = false): Promise<void> {
    const recorder = this.recorder;
    if (!recorder || this.state() !== 'recording') return;
    const chatId = this.recordingChatId;
    this.stopTicker();
    this.recorder = null;
    this.state.set('transcribing');
    try {
      const clip = await recorder.stop();
      if (clip.seconds < 0.3 || clip.audio.size === 0) {
        this.state.set('idle');
        return;
      }
      const text = await firstValueFrom(this.speech.transcribe(clip, chatId, navigator.language));
      if (text && !this.destroyed && this.chatId() === chatId) {
        this.transcribed.emit(text);
      }
      if (atLimit) {
        this.error.set(`Stopped at ${Math.round(this.maxSeconds() / 60)} minutes, the longest a clip can be.`);
      }
    } catch (err) {
      this.error.set(this.failure(err));
    } finally {
      this.state.set('idle');
    }
  }

  private failure(err: unknown): string {
    if (err instanceof HttpErrorResponse && err.status === 429) {
      this.sendGate?.handleSendError(err);
      return "You're out of credits for dictation.";
    }
    if (
      hasErrorCode(err, 'utility_model_not_set') ||
      hasErrorCode(err, 'utility_role_off') ||
      hasErrorCode(err, 'utility_model_unavailable')
    ) {
      void this.speech.loadDictation(true);
      return 'Dictation is turned off or its model is unavailable. Check Voice in Settings.';
    }
    return apiErrorMessage(err, "Couldn't transcribe that. Try again.");
  }

  /** Stops recording without transcribing. */
  discard(): void {
    this.stopTicker();
    this.recorder?.cancel();
    this.recorder = null;
    if (this.state() === 'recording' || this.state() === 'starting') {
      this.state.set('idle');
    }
  }

  private stopTicker(): void {
    if (this.ticker) {
      clearInterval(this.ticker);
      this.ticker = null;
    }
  }
}
