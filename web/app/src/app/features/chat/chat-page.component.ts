import { CommonModule, DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, OnDestroy, OnInit, computed, effect, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, NavigationEnd, Router } from '@angular/router';
import { filter } from 'rxjs/operators';
import { Subscription } from 'rxjs';
import { take } from 'rxjs/operators';

import { ChatService } from '../../core/services/chat.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { RitualService } from '../../core/services/ritual.service';
import { ChatMessage } from '../../core/models/message.model';
import {
  createPendingFileAttachment,
  FileAttachment,
  PendingFileAttachment,
  pendingAttachmentKey,
} from '../../core/models/file-attachment.model';
import { Personality, PersonalityExpression } from '../../core/models/personality.model';
import { ToolCall } from '../../core/models/toolcall.model';
import { ChatComposerComponent } from './components/chat-composer/chat-composer.component';
import { MessageListComponent } from './components/message-list/message-list.component';
import { PersonaPickerDialogComponent } from '../personality/picker/persona-picker-dialog.component';
import { personalityCoverUrl } from '../personality/helpers/cover-image.helpers';
import { AuthImagePipe } from '../../core/pipes/auth-image.pipe';
import { ToolCallDetailModalComponent } from './components/tool-call-detail-modal/tool-call-detail-modal.component';
import {
  appendPendingAssistantGroup,
  groupMessages,
  lastUserTurnWithGenerationError,
  needsPendingAssistantPlaceholder,
  pendingAssistantPlaceholderMessage,
} from './helpers/message-grouping.helpers';
import { ChatSessionService } from './chat-session.service';
import { isChatSendFailed, isChatSendSucceeded } from './chat-send-result';
import { ChatSendGate } from './services/chat-send-gate';
import { ChatSendOutletComponent } from '../../extensions/chat-send-outlet.component';
import { ThreadListService } from '../../core/services/thread-list.service';
import { ThreadListPanelComponent } from './components/thread-list-panel/thread-list-panel.component';
import { ContextPanelService, ContextPanelTab } from './services/context-panel.service';
import { ScratchpadService } from './services/scratchpad.service';
import { ContextPanelToggleComponent } from './components/context-panel/context-panel-toggle.component';
import { BrainIconComponent, ChevDownIconComponent, EditIconComponent, FileIconComponent, LayersIconComponent, NoteIconComponent, WrenchIconComponent, XIconComponent } from '../../shared/ui/icons/icons';
import { thumbnailCircleToCirclePreviewTransform } from '../../shared/ui/avatar/avatar-thumbnail.helpers';
import { personalityAccent, personalityAccentSurface } from '../personality/helpers/personality-vm.helpers';
import { CHAT_PENDING_ASSISTANT_MESSAGE_ID } from './chat.constants';
import { HOTKEY_RITUALS_LIMIT, HOTKEY_SEQUENCE_TIMEOUT } from '../../core/constants/app.constants';
import { parseHotkey, normalizeKey } from '../../core/utils/hotkey.utils';
import { Ritual } from '../../core/models/ritual.model';

const PORTRAIT_SOURCE_WIDTH = 200;
const PORTRAIT_SOURCE_HEIGHT = 267;
const HEADER_AVATAR_SIZE = 44;
const DEFAULT_ASSISTANT_ACCENT = 'hsl(220 70% 50%)';

@Component({
  selector: 'app-chat-page',
  standalone: true,
  imports: [
    CommonModule,
    ChatSendOutletComponent,
    ChatComposerComponent,
    MessageListComponent,
    PersonaPickerDialogComponent,
    ToolCallDetailModalComponent,
    AuthImagePipe,
    ThreadListPanelComponent,
    ContextPanelToggleComponent,
    BrainIconComponent,
    ChevDownIconComponent,
    EditIconComponent,
    FileIconComponent,
    LayersIconComponent,
    NoteIconComponent,
    WrenchIconComponent,
    XIconComponent,
  ],
  providers: [ChatSessionService],
  templateUrl: './chat-page.component.html',
  styleUrl: './chat-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ChatPageComponent implements OnInit, OnDestroy {
  readonly session = inject(ChatSessionService);
  readonly sendGate = inject(ChatSendGate);
  private readonly chatService = inject(ChatService);
  private readonly fileAttachmentService = inject(FileAttachmentService);
  private readonly imageGallery = inject(ImageGalleryService);
  private readonly modelService = inject(ModelService);
  private readonly personalityService = inject(PersonalityService);
  private readonly ritualService = inject(RitualService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly document = inject(DOCUMENT);
  private readonly subscriptions = new Subscription();
  readonly threadList = inject(ThreadListService);
  readonly contextPanel = inject(ContextPanelService);
  private readonly scratchpad = inject(ScratchpadService);

  readonly models = toSignal(this.modelService.models$, { initialValue: [] });
  readonly groups = computed(() => {
    const messages = this.session.messages();
    let grouped = groupMessages(messages);
    const threadId = this.session.thread()?.id;
    if (
      threadId &&
      needsPendingAssistantPlaceholder(messages, this.session.assistantJobPending())
    ) {
      const name =
        this.selectedPersonalityName()?.trim() || this.session.thread()?.personality_name?.trim() || 'Assistant';
      const pending = pendingAssistantPlaceholderMessage({
        chatId: threadId,
        draftText: this.session.pendingAssistantDraftText(),
        generationPersonality: name,
        thinkingImageUrl: this.selectedPersonality()?.expressions_enabled === false
          ? null
          : this.thinkingExpressionThumbUrl(),
      });
      grouped = appendPendingAssistantGroup(grouped, pending);
    }
    return grouped;
  });

  /** User turn with a generation error and no assistant reply yet (composer retry affordance). */
  readonly retryableUserTurn = computed(() => lastUserTurnWithGenerationError(this.session.messages()));
  readonly loadedToolCalls = computed(() =>
    this.session.messages().flatMap(message => message.tool_calls ?? []),
  );
  /** Most recent assistant message that captured a Context X-ray, or null. */
  readonly latestContextMessage = computed(() => {
    const messages = this.session.messages();
    for (let i = messages.length - 1; i >= 0; i--) {
      const message = messages[i];
      const segments = message?.context_breakdown?.segments;
      if (Array.isArray(segments) && segments.length > 0) {
        return message;
      }
    }
    return null;
  });
  readonly selectedToolCall = signal<ToolCall | null>(null);
  /** Incremented when an `import` query param arrives (e.g. from the sidebar), to open the import modal in the thread panel. */
  readonly importTrigger = signal(0);
  readonly checkpointMessageId = signal<string | null>(null);
  readonly personalityExpressions = signal<readonly PersonalityExpression[]>([]);
  readonly copyFeedback = signal<string | null>(null);
  readonly personaPickerOpen = signal(false);
  readonly personaPickerMode = signal<'attach' | 'new-thread'>('attach');
  readonly personalities = signal<readonly Personality[]>([]);
  readonly selectedPersonality = computed<Personality | null>(() => {
    const personalityId = this.session.thread()?.personality_id;
    if (!personalityId) return null;
    return this.personalities().find(p => p.id === personalityId) ?? null;
  });
  readonly selectedPersonalityName = computed<string | null>(() => {
    return this.selectedPersonality()?.name ?? this.session.thread()?.personality_name ?? null;
  });
  readonly selectedPersonalityAccentColor = computed<string | null>(() => {
    const personality = this.selectedPersonality();
    if (personality) {
      return personalityAccent(personality);
    }
    const thread = this.session.thread();
    const fallbackName = thread?.personality_name?.trim();
    if (fallbackName) {
      return personalityAccent({ id: thread?.personality_id ?? '', name: fallbackName, accent_color: null });
    }
    return null;
  });
  readonly selectedPersonalityAccentSurface = computed<string>(() =>
    personalityAccentSurface(this.selectedPersonalityAccentColor() ?? DEFAULT_ASSISTANT_ACCENT),
  );
  readonly selectedPersonalityAvatarTransform = computed(() =>
    thumbnailCircleToCirclePreviewTransform(
      this.selectedPersonality()?.thumbnail_circle,
      PORTRAIT_SOURCE_WIDTH,
      PORTRAIT_SOURCE_HEIGHT,
      HEADER_AVATAR_SIZE,
    ),
  );
  readonly selectedPersonalityAvatarUrl = computed(() =>
    personalityCoverUrl(
      this.selectedPersonality(),
      [],
      this.imageGallery.getImageUrl.bind(this.imageGallery),
    ),
  );
  readonly selectedPersonalityPortraitUrl = computed(() => {
    const personality = this.selectedPersonality();
    if (!personality) return this.selectedPersonalityAvatarUrl();
    if (personality.cover_image_id) {
      return this.imageGallery.getImageUrl(personality.cover_image_id, 'full');
    }
    return personality.cover_image_url ?? this.selectedPersonalityAvatarUrl();
  });
  readonly threadParticipantLabel = computed(() =>
    this.selectedPersonalityName() ?? this.session.thread()?.personality_name ?? 'Pick personality',
  );
  readonly personalityTriggerAriaLabel = computed(() => {
    const personality = this.selectedPersonalityName() ?? this.session.thread()?.personality_name ?? null;
    return personality ? `Change personality (currently ${personality})` : 'Pick personality';
  });
  readonly personalityByNormalizedName = computed(() => {
    const map = new Map<string, Personality>();
    for (const personality of this.personalities()) {
      const key = personality.name.trim().toLowerCase();
      if (key && !map.has(key)) {
        map.set(key, personality);
      }
    }
    return map;
  });
  readonly exportFeedback = signal<string | null>(null);
  readonly editingThreadName = signal(false);
  readonly threadNameDraft = signal('');
  readonly threadSummary = signal('');
  readonly summaryModalOpen = signal(false);
  readonly pendingAttachments = signal<PendingFileAttachment[]>([]);
  private readonly pendingGalleryImageId = signal<string | null>(null);
  private readonly pendingWelcomeChatId = signal<string | null>(null);
  private galleryAttachInFlight = false;
  private welcomeRequestInFlight = false;
  /** Prevents the gallery-attach effect from starting duplicate reference calls. */
  private galleryAttachScheduled = false;
  /** Rituals (skills) to attach to the next outgoing user message. */
  readonly pendingRituals = signal<Ritual[]>([]);
  readonly hotkeyRituals = signal<Ritual[]>([]);
  private clearCopyFeedbackTimer: ReturnType<typeof setTimeout> | null = null;
  /** Coalesces the focus/visibility/online refresh triggers, which often fire together. */
  private lastThreadRefreshAt = 0;
  private static readonly THREAD_REFRESH_THROTTLE_MS = 1000;
  /**
   * Reload the active thread when the user returns to the app (tab visible, window
   * focused, or network back online), so messages produced in another tab or by a
   * background job appear without a manual refresh. Event-driven — no polling.
   */
  private readonly onReturnToApp = (): void => {
    if (this.document.visibilityState === 'hidden') return;
    const now = Date.now();
    if (now - this.lastThreadRefreshAt < ChatPageComponent.THREAD_REFRESH_THROTTLE_MS) return;
    this.lastThreadRefreshAt = now;
    this.session.reloadActiveThread();
  };
  private readonly activeHotkeyListeners = new Map<string, (event: KeyboardEvent) => void>();
  private readonly hotkeySequenceState = new Map<string, { keys: string[]; currentIndex: number; timeout?: ReturnType<typeof setTimeout> }>();

  private readonly syncContextPanelChat = effect(() => {
    this.contextPanel.setActiveChat(this.session.thread());
  });

  // When a background checkpoint (scratchpad + summary) completes for the active
  // thread, refresh the header summary and sidebar scratchpad in place so users
  // see the new context without a full page reload.
  private readonly refreshContextOnCheckpoint = effect(() => {
    const token = this.session.contextCheckpointToken();
    if (token === 0) return;
    const threadId = this.session.thread()?.id;
    if (!threadId) return;
    this.loadThreadSummary(threadId, false);
    void this.scratchpad.refresh();
  });
  private readonly syncContextPanelToolCalls = effect(() => {
    this.contextPanel.setToolCalls(this.loadedToolCalls());
  });
  private readonly syncContextPanelBreakdown = effect(() => {
    const message = this.latestContextMessage();
    this.contextPanel.setLatestBreakdown(message?.context_breakdown ?? null, message?.id ?? null);
  });
  private readonly syncActiveModel = effect(() => {
    const modelId = this.session.model()?.id ?? this.session.thread()?.model_id ?? null;
    const model = modelId ? this.models().find(item => item.id === modelId) : undefined;
    this.sendGate.setActiveModel(model ?? null);
  });

  private readonly syncComposerInsert = effect(() => {
    const pending = this.contextPanel.composerInsert();
    if (!pending) return;
    this.session.draft.set(pending);
    this.contextPanel.consumeComposerInsert();
  });

  private readonly loadPersonalityExpressions = effect(onCleanup => {
    const pid = this.session.thread()?.personality_id;
    if (!pid) {
      this.personalityExpressions.set([]);
      return;
    }
    const sub = this.personalityService.listExpressions(pid).subscribe({
      next: rows => this.personalityExpressions.set(rows ?? []),
      error: () => this.personalityExpressions.set([]),
    });
    onCleanup(() => sub.unsubscribe());
  });
  // When thread + route are ready, reference the gallery image into composer attachments.
  // No onCleanup unsubscribe: clearing pendingGalleryImageId inside the effect re-ran cleanup
  // and cancelled referenceImage before it completed.
  private readonly attachPendingGalleryImage = effect(() => {
    const imageId = this.pendingGalleryImageId();
    const thread = this.session.thread();
    const routeThreadId = this.route.snapshot.paramMap.get('id');
    if (!imageId || !thread?.id || thread.id !== routeThreadId) {
      this.galleryAttachScheduled = false;
      return;
    }
    if (this.galleryAttachInFlight || this.galleryAttachScheduled) {
      return;
    }
    this.galleryAttachScheduled = true;
    this.galleryAttachInFlight = true;
    this.imageGallery
      .referenceImage(imageId)
      .pipe(take(1))
      .subscribe({
        next: attachment => {
          this.onGalleryAttachment(attachment);
          this.pendingGalleryImageId.set(null);
          this.galleryAttachInFlight = false;
          this.galleryAttachScheduled = false;
        },
        error: () => {
          this.pendingGalleryImageId.set(null);
          this.galleryAttachInFlight = false;
          this.galleryAttachScheduled = false;
        },
      });
  });

  private readonly maybeTriggerWelcomeMessage = effect(() => {
    const pendingChatId = this.pendingWelcomeChatId();
    if (!pendingChatId || this.welcomeRequestInFlight || this.session.loading()) {
      return;
    }
    const activeThreadId = this.session.thread()?.id;
    if (!activeThreadId || activeThreadId !== pendingChatId) {
      return;
    }

    // If messages already exist, skip welcome trigger.
    if (this.session.messages().length > 0) {
      this.pendingWelcomeChatId.set(null);
      return;
    }

    this.welcomeRequestInFlight = true;
    this.chatService.createWelcomeMessage(pendingChatId).pipe(take(1)).subscribe({
      next: response => {
        if (response?.job_id) {
          this.session.startAssistantJobPolling(response.job_id, pendingChatId);
        }
        this.pendingWelcomeChatId.set(null);
        this.welcomeRequestInFlight = false;
      },
      error: () => {
        this.pendingWelcomeChatId.set(null);
        this.welcomeRequestInFlight = false;
      },
    });
  });

  private readonly loadHotkeyRituals = effect(onCleanup => {
    const threadId = this.session.thread()?.id;
    this.unregisterAllHotkeys();
    if (!threadId) {
      this.hotkeyRituals.set([]);
      return;
    }
    const sub = this.ritualService
      .getAvailableRituals(threadId, 1, HOTKEY_RITUALS_LIMIT, { has_hotkeys: true })
      .subscribe({
        next: response => {
          this.hotkeyRituals.set(response.results ?? []);
          this.registerHotkeys();
        },
        error: () => {
          this.hotkeyRituals.set([]);
        },
      });
    onCleanup(() => sub.unsubscribe());
  });

  readonly thinkingExpressionThumbUrl = computed(() => {
    const row = this.personalityExpressions().find(e => e.expression_key === 'thinking');
    if (!row) return null;
    if (row.image_id) {
      return this.imageGallery.getImageUrl(row.image_id, 'thumbnail');
    }
    return row.image_url ?? null;
  });

  private readonly syncActiveThreadPersonality = effect(onCleanup => {
    const personalityId = this.session.thread()?.personality_id;
    if (!personalityId) return;
    const sub = this.personalityService.getPersonality(personalityId).subscribe({
      next: personality => this.upsertPersonality(personality),
    });
    onCleanup(() => sub.unsubscribe());
  });

  ngOnInit(): void {
    this.sendGate.enterChat();
    this.sendGate.refresh();
    this.subscriptions.add(
      this.router.events.pipe(
        filter((event): event is NavigationEnd => event instanceof NavigationEnd),
        filter(event => /^\/chat(\/|$)/.test(event.urlAfterRedirects.split('?')[0] ?? '')),
      ).subscribe(() => {
        this.sendGate.refresh();
        this.reloadPersonalities();
      }),
    );
    this.contextPanel.setDesktopVisible(true);
    this.document.addEventListener('visibilitychange', this.onReturnToApp);
    const view = this.document.defaultView;
    view?.addEventListener('focus', this.onReturnToApp);
    view?.addEventListener('online', this.onReturnToApp);
    void this.threadList.refresh();
    this.subscriptions.add(this.modelService.getModels().subscribe());
    this.reloadPersonalities();
    this.subscriptions.add(
      this.route.paramMap.subscribe(params => {
        const id = params.get('id');
        if (id) {
          this.threadList.setActiveThreadId(id);
          this.session.setActive(id);
          this.loadThreadSummary(id);
          return;
        }

        const lastChatId = this.chatService.getLastChatId();
        if (lastChatId) {
          void this.router.navigate(['/chat', lastChatId], { replaceUrl: true });
          return;
        }

        this.session.clearActive();
        this.threadList.setActiveThreadId(null);
        this.threadSummary.set('');
        this.summaryModalOpen.set(false);
      }),
    );
    this.subscriptions.add(
      this.route.queryParamMap.subscribe(params => {
        this.checkpointMessageId.set(params.get('checkpoint')?.trim() || null);
        // Open the import modal when navigated here with ?import=… (cross-screen entry from the sidebar).
        if (params.get('import')?.trim()) {
          this.importTrigger.update(n => n + 1);
          void this.router.navigate([], {
            relativeTo: this.route,
            queryParams: { import: null },
            queryParamsHandling: 'merge',
            replaceUrl: true,
          });
          return;
        }
        const galleryImageId = params.get('galleryImageId')?.trim();
        const welcomeFlag = params.get('welcome')?.trim().toLowerCase() === 'true';
        const routeChatId = this.route.snapshot.paramMap.get('id')?.trim();
        if (!galleryImageId) {
          if (!welcomeFlag || !routeChatId) {
            return;
          }
          this.pendingWelcomeChatId.set(routeChatId);
          void this.router.navigate([], {
            relativeTo: this.route,
            queryParams: { welcome: null },
            queryParamsHandling: 'merge',
            replaceUrl: true,
          });
          return;
        }
        this.pendingGalleryImageId.set(galleryImageId);
        if (welcomeFlag && routeChatId) {
          this.pendingWelcomeChatId.set(routeChatId);
        }
        void this.router.navigate([], {
          relativeTo: this.route,
          queryParams: { galleryImageId: null, welcome: null },
          queryParamsHandling: 'merge',
          replaceUrl: true,
        });
      }),
    );
  }

  ngOnDestroy(): void {
    if (this.clearCopyFeedbackTimer) {
      clearTimeout(this.clearCopyFeedbackTimer);
      this.clearCopyFeedbackTimer = null;
    }
    this.contextPanel.setDesktopVisible(false);
    this.document.removeEventListener('visibilitychange', this.onReturnToApp);
    const view = this.document.defaultView;
    view?.removeEventListener('focus', this.onReturnToApp);
    view?.removeEventListener('online', this.onReturnToApp);
    this.subscriptions.unsubscribe();
    this.unregisterAllHotkeys();
  }

  async startConversation(): Promise<void> {
    const chat = await this.session.createThread();
    this.threadList.setActiveThreadId(chat.id);
    await this.router.navigate(['/chat', chat.id]);
    void this.threadList.refresh();
  }

  openPersonaPicker(mode: 'attach' | 'new-thread' = 'attach'): void {
    this.personaPickerMode.set(mode);
    this.personaPickerOpen.set(true);
  }

  closePersonaPicker(): void {
    this.personaPickerOpen.set(false);
  }

  async onPersonaPicked(personality: Personality): Promise<void> {
    this.personaPickerOpen.set(false);
    if (this.personaPickerMode() === 'new-thread') {
      const chat = await this.session.createThread({ personalityId: personality.id });
      this.threadList.setActiveThreadId(chat.id);
      await this.router.navigate(['/chat', chat.id]);
      void this.threadList.refresh();
      return;
    }
    this.session.setPersonality(personality.id);
  }

  async send(text: string): Promise<void> {
    if (this.sendGate.blocked()) {
      return;
    }
    const attachments = this.pendingAttachments();
    if (attachments.some(attachment => attachment.isUploading)) {
      this.session.draft.set(text);
      return;
    }
    const rituals = this.pendingRituals();
    const sendResult = await this.session.sendMessage(text, attachments, rituals);
    this.sendGate.refresh();
    if (isChatSendFailed(sendResult)) {
      this.sendGate.handleSendError(sendResult.error);
      return;
    }
    if (!isChatSendSucceeded(sendResult)) {
      return;
    }
    this.sendGate.sendSucceeded();
    this.pendingAttachments.set([]);
    this.pendingRituals.set([]);
  }

  onPendingRitualsChange(next: readonly Ritual[]): void {
    this.pendingRituals.set([...next]);
  }

  onRetryGeneration(): void {
    const target = this.retryableUserTurn();
    if (target) {
      void this.session.retryUserMessage(target);
    }
  }

  stopGeneration(): void {
    this.session.cancelGeneration();
  }

  retryUserMessageFromList(message: ChatMessage): void {
    void this.session.retryUserMessage(message);
  }

  onFilesSelected(files: File[]): void {
    const chat = this.session.thread();
    if (!chat || files.length === 0) return;

    const pending: PendingFileAttachment[] = files.map(file =>
      createPendingFileAttachment({ file, isUploading: true }),
    );
    this.pendingAttachments.update(current => [...current, ...pending]);

    for (const item of pending) {
      if (!item.file) continue;
      this.subscriptions.add(
        this.fileAttachmentService.uploadChatFileAttachment(chat.id, item.file).subscribe({
          next: attachment => {
            this.pendingAttachments.update(current =>
              current.map(existing => (existing === item ? { ...existing, attachment, isUploading: false } : existing)),
            );
          },
          error: error => {
            const message = error?.message ?? 'Upload failed';
            this.pendingAttachments.update(current =>
              current.map(existing => (existing === item ? { ...existing, isUploading: false, uploadError: message } : existing)),
            );
          },
        }),
      );
    }
  }

  onGalleryAttachment(attachment: FileAttachment): void {
    const pending = createPendingFileAttachment({
      clientKey: `attachment:${attachment.id}`,
      attachment,
      isUploading: false,
    });
    this.pendingAttachments.update(current => [...current, pending]);
  }

  onAttachmentRemoved(key: string): void {
    this.pendingAttachments.update(current =>
      current.filter(item => pendingAttachmentKey(item) !== key),
    );
  }

  async copyMessage(message: ChatMessage): Promise<void> {
    if (message.id === CHAT_PENDING_ASSISTANT_MESSAGE_ID) return;
    const rendered = this.session.getDisplayMessage(message);
    const text = rendered.trim() ? rendered : message.message;
    const copied = await this.writeToClipboard(text);
    this.setCopyFeedback(copied ? 'Message copied.' : 'Could not copy message.');
  }

  openToolCall(toolCall: ToolCall): void {
    this.selectedToolCall.set(toolCall);
  }

  closeToolCall(): void {
    this.selectedToolCall.set(null);
  }

  async openThread(threadId: string): Promise<void> {
    if (this.threadList.activeThreadId() === threadId) {
      this.threadList.setActiveThreadId(null);
      await this.router.navigate(['/chat']);
      return;
    }
    this.threadList.setActiveThreadId(threadId);
    await this.router.navigate(['/chat', threadId]);
  }

  openContextPanelMobile(): void {
    this.contextPanel.setDesktopVisible(true);
    this.contextPanel.openMobile();
  }

  openContextPanel(tab: ContextPanelTab): void {
    const sameTab = this.contextPanel.activeTab() === tab;
    if (sameTab && this.contextPanel.visible()) {
      this.contextPanel.setDesktopVisible(false);
      this.contextPanel.closeMobile();
      return;
    }
    this.contextPanel.setActiveTab(tab);
    this.contextPanel.setDesktopVisible(true);
    if (this.isMobileViewport()) {
      this.contextPanel.openMobile();
    }
  }

  // "Show context" on a past assistant message: pin that turn's breakdown and open the
  // Context tab. Unlike openContextPanel this never toggles closed — it always reveals.
  openContextForMessage(message: ChatMessage): void {
    const breakdown = message.context_breakdown;
    if (!breakdown) return;
    this.contextPanel.selectBreakdown(breakdown);
    this.contextPanel.setActiveTab('context');
    this.contextPanel.setDesktopVisible(true);
    if (this.isMobileViewport()) {
      this.contextPanel.openMobile();
    }
  }

  exportActiveChat(): void {
    const chat = this.session.thread();
    if (!chat) return;
    this.exportFeedback.set('Preparing export...');
    this.subscriptions.add(
      this.chatService.exportChat(chat.id).subscribe({
        next: () => this.exportFeedback.set('Export started.'),
        error: error => this.exportFeedback.set(error?.message ?? 'Export failed.'),
      }),
    );
  }

  async closeActiveChat(): Promise<void> {
    this.chatService.clearLastChatId();
    this.session.clearActive();
    this.threadList.setActiveThreadId(null);
    this.threadSummary.set('');
    this.summaryModalOpen.set(false);
    await this.router.navigate(['/chat']);
  }

  openSummaryModal(): void {
    if (!this.threadSummary().trim()) return;
    this.summaryModalOpen.set(true);
  }

  closeSummaryModal(): void {
    this.summaryModalOpen.set(false);
  }

  startThreadRename(): void {
    const name = this.session.thread()?.name ?? '';
    this.threadNameDraft.set(name);
    this.editingThreadName.set(true);
  }

  commitThreadRename(nextValue: string): void {
    const thread = this.session.thread();
    this.editingThreadName.set(false);
    if (!thread) return;
    const trimmed = nextValue.trim();
    if (!trimmed || trimmed === thread.name) return;
    this.session.setThreadName(trimmed);
    void this.threadList.refresh();
  }

  cancelThreadRename(): void {
    this.editingThreadName.set(false);
    this.threadNameDraft.set(this.session.thread()?.name ?? '');
  }

  selectedPersonalityInitials(): string {
    return (this.selectedPersonalityName() ?? 'Assistant')
      .split(/\s+/)
      .map(word => word.charAt(0))
      .join('')
      .toUpperCase()
      .slice(0, 2);
  }

  assistantVisualForMessage = (message: ChatMessage): { avatarUrl: string | null; accentColor: string | null; accentSurface: string | null; expressionsEnabled: boolean } => {
    const generationPersonality = message.generation_personality?.trim();
    const personalityFromMessage = generationPersonality
      ? this.personalityByNormalizedName().get(generationPersonality.toLowerCase()) ?? null
      : null;
    const personality = personalityFromMessage ?? (!generationPersonality ? this.selectedPersonality() : null);
    const avatarUrl = personalityCoverUrl(
      personality,
      [],
      this.imageGallery.getImageUrl.bind(this.imageGallery),
    );
    const accentColor = personality
      ? personalityAccent(personality)
      : generationPersonality
        ? personalityAccent({ id: '', name: generationPersonality, accent_color: null })
        : this.selectedPersonalityAccentColor();
    return {
      avatarUrl,
      accentColor,
      accentSurface: personalityAccentSurface(accentColor ?? DEFAULT_ASSISTANT_ACCENT),
      // Thread personality is authoritative for the images-in-chat display setting.
      expressionsEnabled: this.selectedPersonality()?.expressions_enabled ?? true,
    };
  };

  private reloadPersonalities(): void {
    this.subscriptions.add(
      this.personalityService.listPersonalities(1, 100).subscribe(response => {
        this.personalities.set(response.results ?? []);
      }),
    );
  }

  private upsertPersonality(personality: Personality): void {
    this.personalities.update(list => {
      const index = list.findIndex(row => row.id === personality.id);
      if (index < 0) return [...list, personality];
      const next = list.slice();
      next[index] = personality;
      return next;
    });
  }

  private setCopyFeedback(message: string): void {
    this.copyFeedback.set(message);
    if (this.clearCopyFeedbackTimer) {
      clearTimeout(this.clearCopyFeedbackTimer);
    }
    this.clearCopyFeedbackTimer = setTimeout(() => {
      this.copyFeedback.set(null);
      this.clearCopyFeedbackTimer = null;
    }, 1800);
  }

  private isMobileViewport(): boolean {
    const view = this.document.defaultView;
    return !!view?.matchMedia('(max-width: 1023px)').matches;
  }

  private async writeToClipboard(text: string): Promise<boolean> {
    const view = this.document.defaultView;
    const navigator = view?.navigator;
    if (!text || !navigator) return false;

    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
        return true;
      }
    } catch {
      // Fall back to execCommand path below.
    }

    const body = this.document.body;
    if (!body) return false;
    const textarea = this.document.createElement('textarea');
    textarea.value = text;
    textarea.setAttribute('readonly', '');
    textarea.style.position = 'fixed';
    textarea.style.top = '-9999px';
    textarea.style.left = '-9999px';
    body.appendChild(textarea);
    textarea.select();
    textarea.setSelectionRange(0, textarea.value.length);
    let copied = false;
    try {
      copied = this.document.execCommand('copy');
    } catch {
      copied = false;
    } finally {
      body.removeChild(textarea);
    }
    return copied;
  }

  private loadThreadSummary(threadId: string, resetView: boolean = true): void {
    // On initial thread load we blank the summary and close any open modal; on an
    // in-place refresh (post-checkpoint) we keep the current view and just swap text.
    if (resetView) {
      this.threadSummary.set('');
      this.summaryModalOpen.set(false);
    }
    this.subscriptions.add(
      this.chatService.getChatContext(threadId).subscribe({
        next: context => {
          if (this.threadList.activeThreadId() !== threadId) return;
          this.threadSummary.set(context.summary?.trim() ?? '');
        },
        error: () => {
          if (this.threadList.activeThreadId() !== threadId || !resetView) return;
          this.threadSummary.set('');
        },
      }),
    );
  }

  private registerHotkeys(): void {
    this.unregisterAllHotkeys();
    this.hotkeyRituals().forEach(ritual => {
      if (!ritual.hotkeys?.trim()) return;
      const hotkeyListener = (event: KeyboardEvent) => {
        if (this.isHotkeyMatch(event, ritual.hotkeys, ritual.id)) {
          event.preventDefault();
          event.stopPropagation();
          this.addRitualViaHotkey(ritual);
        }
      };
      this.activeHotkeyListeners.set(ritual.id, hotkeyListener);
      this.document.addEventListener('keydown', hotkeyListener);
    });
  }

  private unregisterAllHotkeys(): void {
    this.activeHotkeyListeners.forEach(listener => {
      this.document.removeEventListener('keydown', listener);
    });
    this.activeHotkeyListeners.clear();
    this.hotkeySequenceState.forEach(state => {
      if (state.timeout) {
        clearTimeout(state.timeout);
      }
    });
    this.hotkeySequenceState.clear();
  }

  private isHotkeyMatch(event: KeyboardEvent, hotkeyString: string, ritualId: string): boolean {
    const parsedHotkey = parseHotkey(hotkeyString);
    if (event.ctrlKey !== parsedHotkey.modifiers.ctrl ||
        event.shiftKey !== parsedHotkey.modifiers.shift ||
        event.altKey !== parsedHotkey.modifiers.alt ||
        event.metaKey !== parsedHotkey.modifiers.meta) {
      this.resetHotkeySequence(ritualId);
      return false;
    }

    let sequenceState = this.hotkeySequenceState.get(ritualId);
    if (!sequenceState) {
      sequenceState = { keys: parsedHotkey.keys, currentIndex: 0 };
      this.hotkeySequenceState.set(ritualId, sequenceState);
    }

    const actualKey = normalizeKey(event.key);
    const expectedKey = sequenceState.keys[sequenceState.currentIndex];
    if (actualKey === expectedKey) {
      sequenceState.currentIndex += 1;
      if (sequenceState.timeout) {
        clearTimeout(sequenceState.timeout);
      }
      if (sequenceState.currentIndex >= sequenceState.keys.length) {
        this.resetHotkeySequence(ritualId);
        return true;
      }
      sequenceState.timeout = setTimeout(() => {
        this.resetHotkeySequence(ritualId);
      }, HOTKEY_SEQUENCE_TIMEOUT);
      return false;
    }

    this.resetHotkeySequence(ritualId);
    return false;
  }

  private resetHotkeySequence(ritualId: string): void {
    const sequenceState = this.hotkeySequenceState.get(ritualId);
    if (!sequenceState) return;
    if (sequenceState.timeout) {
      clearTimeout(sequenceState.timeout);
    }
    sequenceState.currentIndex = 0;
    delete sequenceState.timeout;
  }

  private addRitualViaHotkey(ritual: Ritual): void {
    this.pendingRituals.update(current => {
      if (current.some(r => r.id === ritual.id)) {
        return current;
      }
      return [...current, ritual];
    });
  }
}
