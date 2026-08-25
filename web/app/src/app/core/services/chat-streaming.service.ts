import { DOCUMENT } from '@angular/common';
import { Injectable, inject, signal } from '@angular/core';
import { ChatMessage } from '../models/message.model';

export interface StreamingConfig {
  intervalMs: number;
  scrollCheckInterval: number;
  codeBlockChunkSize: number;
}

const DEFAULT_STREAMING_CONFIG: StreamingConfig = {
  intervalMs: 30,
  scrollCheckInterval: 5,
  codeBlockChunkSize: 50
};

@Injectable({
  providedIn: 'root'
})
export class ChatStreamingService {
  private readonly document = inject(DOCUMENT);
  private readonly displayedMessages = signal<Map<string, string>>(new Map());
  private readonly targetMessages = new Map<string, string>();
  private readonly queuedUnits = new Map<string, string[]>();
  private readonly streamingIntervals = new Map<string, ReturnType<typeof setInterval>>();
  private readonly displayRevision = signal(0);
  private config: StreamingConfig = DEFAULT_STREAMING_CONFIG;
  private onStreamingProgress?: () => void;
  private onStreamingComplete?: (messageId: string) => void;
  private readonly visibilityListener = () => {
    // Always snap to latest target on visibility transitions so backgrounded tabs
    // never replay stale queued typing when focus changes.
    this.flushAllToLatest();
  };

  constructor() {
    this.document.addEventListener('visibilitychange', this.visibilityListener);
  }

  configure(config: Partial<StreamingConfig>): void {
    this.config = { ...this.config, ...config };
  }

  setProgressCallback(callback: () => void): void {
    this.onStreamingProgress = callback;
  }

  setCompletionCallback(callback: (messageId: string) => void): void {
    this.onStreamingComplete = callback;
  }

  startStreaming(message: ChatMessage): void {
    const fullText = message.message ?? '';
    this.resetMessage(message.id);
    this.appendServerChunks(message.id, [fullText], { animate: true });
  }

  appendServerChunks(messageId: string, chunks: readonly string[], opts?: { animate?: boolean }): void {
    if (!chunks.length) return;
    const nextTarget = `${this.targetMessages.get(messageId) ?? ''}${chunks.join('')}`;
    this.targetMessages.set(messageId, nextTarget);
    const shouldAnimate = opts?.animate ?? !this.document.hidden;
    if (!shouldAnimate) {
      this.setDisplayed(messageId, nextTarget);
      this.clearInterval(messageId);
      this.queuedUnits.delete(messageId);
      return;
    }
    const units = chunks.flatMap(chunk => this.splitIntoStreamableUnits(chunk));
    if (!units.length) return;
    const nextQueue = [...(this.queuedUnits.get(messageId) ?? []), ...units];
    this.queuedUnits.set(messageId, nextQueue);
    this.ensureInterval(messageId);
  }

  completeStreaming(messageId: string, fullText?: string, triggerCompletion: boolean = true): void {
    if (fullText !== undefined) {
      this.targetMessages.set(messageId, fullText);
      this.setDisplayed(messageId, fullText);
    } else {
      const target = this.targetMessages.get(messageId) ?? this.displayedMessages().get(messageId) ?? '';
      this.setDisplayed(messageId, target);
    }
    this.clearInterval(messageId);
    this.queuedUnits.delete(messageId);
    if (triggerCompletion && this.onStreamingComplete) this.onStreamingComplete(messageId);
  }

  stopStreaming(messageId: string, fullText?: string, triggerCompletion: boolean = false): void {
    this.completeStreaming(messageId, fullText, triggerCompletion);
  }

  clearMessageState(messageId: string): void {
    if (!messageId) return;
    this.clearInterval(messageId);
    this.queuedUnits.delete(messageId);
    this.targetMessages.delete(messageId);
    this.displayedMessages.update(map => {
      if (!map.has(messageId)) return map;
      const next = new Map(map);
      next.delete(messageId);
      return next;
    });
    this.displayRevision.update(v => v + 1);
  }

  stopAll(): void {
    this.streamingIntervals.forEach(interval => clearInterval(interval));
    this.streamingIntervals.clear();
    this.queuedUnits.clear();
    this.targetMessages.clear();
    this.displayedMessages.set(new Map());
    this.displayRevision.update(v => v + 1);
  }

  getDisplayMessage(message: ChatMessage): string {
    const streamedContent = this.displayedMessages().get(message.id);
    if (streamedContent !== undefined) {
      return streamedContent;
    }
    return message.message;
  }

  getDisplayLength(message: ChatMessage): number {
    return (this.displayedMessages().get(message.id) ?? message.message ?? '').length;
  }

  getDisplayRevision(): number {
    return this.displayRevision();
  }

  isStreaming(messageId: string, fullMessage: string): boolean {
    const displayed = this.displayedMessages().get(messageId);
    if (displayed === undefined) return false;
    const target = this.targetMessages.get(messageId) ?? fullMessage;
    return displayed.length < target.length;
  }

  private splitIntoStreamableUnits(text: string): string[] {
    const units: string[] = [];
    let currentWord = '';
    let inCodeBlock = false;

    for (let i = 0; i < text.length; i++) {
      const char = text[i];
      const nextThreeChars = text.slice(i, Math.min(i + 3, text.length));

      if (nextThreeChars === '```') {
        if (currentWord) {
          units.push(currentWord);
          currentWord = '';
        }
        units.push('```');
        inCodeBlock = !inCodeBlock;
        i += 2; // Skip past the delimiter
        continue;
      }

      if (inCodeBlock) {
        currentWord += char;
        // Break large code blocks into chunks for smoother streaming
        if (char === '\n' && currentWord.length > this.config.codeBlockChunkSize) {
          units.push(currentWord);
          currentWord = '';
        }
      } else {
        if (char === ' ' || char === '\n') {
          if (currentWord) {
            units.push(currentWord);
            currentWord = '';
          }
          units.push(char);
        } else {
          currentWord += char;
        }
      }
    }

    if (currentWord) {
      units.push(currentWord);
    }

    return units;
  }

  destroy(): void {
    this.document.removeEventListener('visibilitychange', this.visibilityListener);
    this.stopAll();
    this.onStreamingProgress = undefined;
    this.onStreamingComplete = undefined;
  }

  private resetMessage(messageId: string): void {
    this.clearInterval(messageId);
    this.queuedUnits.delete(messageId);
    this.targetMessages.set(messageId, '');
    this.setDisplayed(messageId, '');
  }

  private flushAllToLatest(): void {
    for (const [messageId, target] of this.targetMessages.entries()) {
      this.setDisplayed(messageId, target);
    }
    this.streamingIntervals.forEach(interval => clearInterval(interval));
    this.streamingIntervals.clear();
    this.queuedUnits.clear();
  }

  private ensureInterval(messageId: string): void {
    if (this.streamingIntervals.has(messageId)) return;
    const interval = setInterval(() => {
      const queue = this.queuedUnits.get(messageId) ?? [];
      if (queue.length === 0) {
        this.clearInterval(messageId);
        const target = this.targetMessages.get(messageId) ?? this.displayedMessages().get(messageId) ?? '';
        const displayed = this.displayedMessages().get(messageId) ?? '';
        if (displayed.length >= target.length && this.onStreamingComplete) {
          this.onStreamingComplete(messageId);
        }
        return;
      }
      const nextUnit = queue.shift()!;
      if (queue.length === 0) {
        this.queuedUnits.delete(messageId);
      } else {
        this.queuedUnits.set(messageId, queue);
      }
      const nextDisplayed = `${this.displayedMessages().get(messageId) ?? ''}${nextUnit}`;
      this.setDisplayed(messageId, nextDisplayed);
      if (this.onStreamingProgress) this.onStreamingProgress();
    }, this.config.intervalMs);
    this.streamingIntervals.set(messageId, interval);
  }

  private clearInterval(messageId: string): void {
    const interval = this.streamingIntervals.get(messageId);
    if (!interval) return;
    clearInterval(interval);
    this.streamingIntervals.delete(messageId);
  }

  private setDisplayed(messageId: string, content: string): void {
    this.displayedMessages.update(map => {
      const next = new Map(map);
      next.set(messageId, content);
      return next;
    });
    this.displayRevision.update(v => v + 1);
  }
}

