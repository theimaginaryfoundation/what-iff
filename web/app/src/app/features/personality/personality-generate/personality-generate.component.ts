import { Component, computed, DestroyRef, inject, input, OnInit, output, signal, ChangeDetectionStrategy } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { AsyncPipe, NgClass } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { GeneratePersonalityModalService } from '../../../core/services/generate-personality-modal.service';
import { PersonalityGenFlowService } from '../../../core/services/personality-gen-flow.service';
import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { ChatService } from '../../../core/services/chat.service';
import {
  CUSTOM_IMAGE_STYLE_MAX_LEN,
  effectiveImageStyle,
  IMAGE_STYLES,
  IMAGE_STYLE_NONE,
  IMAGE_STYLE_OTHER,
  parseStoredImageStyle,
  PersonalityGenFlow,
} from '../../../core/models/personality-gen-flow.model';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';

// ─── Question definitions (stub text — replace later) ───────────────────────

export interface WizardQuestion {
  id: string;
  label: string;
  placeholder: string;
}

export interface WizardPage {
  title: string;
  subtitle: string;
  questions: WizardQuestion[];
}

export const WIZARD_PAGES: readonly WizardPage[] = [
  {
    title: 'General',
    subtitle: 'Start with the big picture — who is this personality?',
    questions: [
      {
        id: 'general_description',
        label: 'Describe who they are',
        placeholder:
          'Appearance, vibe, quirks, shared history with you, how they usually show up… e.g. a sharp-witted fox in a cluttered study who remembers our inside jokes and pushes back when I spiral.',
      },
    ],
  },
  {
    title: 'Communication Style',
    subtitle: 'How should this personality communicate?',
    questions: [
      { id: 'formality', label: 'How formal or casual should it be?', placeholder: 'e.g., very casual like a friend, professional but approachable, academic...' },
      { id: 'vocabulary', label: 'What vocabulary and jargon should it use?', placeholder: 'e.g., keep it simple, physics jargon OK, use technical terms freely...' },
      { id: 'vibe', label: 'What should its general vibe or tone be?', placeholder: 'e.g., warm and encouraging, calm and analytical, artsy and metaphorical, edgy and witty...' },
      { id: 'communication_style', label: 'How should it explain complex topics?', placeholder: 'e.g., uses step-by-step breakdowns, speaks in analogies, asks lots of questions first...' },
    ],
  },
  {
    title: 'Personality',
    subtitle: 'What behaviors should this personality prefer or avoid?',
    questions: [
      { id: 'quirks', label: 'What personality quirks or unique traits should it have?', placeholder: 'e.g., occasionally uses fox metaphors, starts with a relevant quote, dry humor...' },
      { id: 'avoid_topics', label: 'What topics or behaviors should it avoid?', placeholder: 'e.g., no unsolicited advice, avoid politics, don\'t be condescending...' },
      { id: 'emotional_style', label: 'How should it handle emotions or sensitive topics?', placeholder: 'e.g., empathetic and validating, matter-of-fact, gently redirects...' },
      
    ],
  },
  {
    title: 'Final Touches',
    subtitle: 'Optional extras for deeper customization',
    questions: [
      { id: 'example_interaction', label: 'Describe an ideal interaction or sample exchange.', placeholder: 'e.g., I ask for help debugging code and it walks me through it step by step while cracking a joke...' },     
      { id: 'anything_else', label: 'Anything else you\'d like to add?', placeholder: 'Any other details, instructions, or preferences...' },
    ],
  },
];

@Component({
  selector: 'app-personality-generate',
  standalone: true,
  imports: [AsyncPipe, AuthImagePipe, FormsModule, NgClass],
  templateUrl: './personality-generate.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./personality-generate.component.scss'],
})
export class PersonalityGenerateComponent implements OnInit {
  readonly modalMode = input(false);
  readonly closeModal = output<void>();

  private genFlowService = inject(PersonalityGenFlowService);
  private generateModal = inject(GeneratePersonalityModalService);
  private mediaJobs = inject(PersonalityMediaJobService);
  readonly imageGallery = inject(ImageGalleryService);
  private router = inject(Router);
  private confirmationService = inject(ConfirmationService);
  private chatService = inject(ChatService);
  private readonly destroyRef = inject(DestroyRef);

  readonly pages = WIZARD_PAGES;
  readonly imageStyles = IMAGE_STYLES;
  readonly customImageStyleMaxLen = CUSTOM_IMAGE_STYLE_MAX_LEN;
  readonly imageStyleOther = IMAGE_STYLE_OTHER;

  // Flow state from the backend.
  flow = signal<PersonalityGenFlow | null>(null);
  isLoading = signal(true);
  errorMessage = signal<string | null>(null);

  // Wizard navigation.
  currentStep = signal(0);
  answers = signal<Record<string, string>>({});

  // Image style + reference image (page 1 options).
  /** Selected style pill ('auto', 'none', preset slug, or 'other'). */
  imageStyleSelection = signal<string>('auto');
  /** Free-form hint when imageStyleSelection is 'other'. */
  customImageStyle = signal('');
  readonly persistedImageStyle = computed(() =>
    effectiveImageStyle(this.imageStyleSelection(), this.customImageStyle()),
  );
  referenceImageId = signal<string | null>(null);
  imageUploadLoading = signal(false);

  // Generation state.
  isGenerating = signal(false);
  isAccepting = signal(false);
  isSaving = signal(false);
  isResetting = signal(false);
  isEditingAnswers = signal(false);

  coverImageId = signal<string | null>(null);
  portraitGenerating = signal(false);
  private portraitJobStarted = false;

  // Name selection/editing on the review screen.
  selectedNameIndex = signal(0);
  isManualNameMode = signal(false);
  manualName = signal('');

  // Computed: are we on the review screen (post-generation)?
  isReviewScreen = computed(() => {
    const f = this.flow();
    return f?.status === 'generated' && !this.isEditingAnswers();
  });

  generatedNames = computed(() => {
    return this.flow()?.generated_names ?? [];
  });

  selectedGeneratedName = computed(() => {
    const names = this.generatedNames();
    if (names.length === 0) return 'Generated Personality';
    return names[this.selectedNameIndex() % names.length];
  });

  selectedName = computed(() => {
    const custom = this.manualName().trim();
    if (this.isManualNameMode() && custom) return custom;
    return this.selectedGeneratedName();
  });

  showSkipCta = computed(() => {
    const step = this.currentStep();
    return !this.isReviewScreen() && !this.isGenerating() && step >= 1;
  });

  /**
   * True when answers typed on the current step haven't been persisted to the
   * draft flow yet (progress autosaves on navigation, not per keystroke). Used
   * by the modal host to guard backdrop/Escape dismissal. Review/generation
   * phases are already persisted, so they never count as unsaved.
   */
  readonly hasUnsavedAnswers = computed(() => {
    const f = this.flow();
    if (!f || this.isReviewScreen() || this.isGenerating()) return false;
    return !this.areAnswersEquivalent(this.answers(), f.answers);
  });

  totalSteps = this.pages.length;

  ngOnInit(): void {
    this.loadFlow();
  }

  loadFlow(): void {
    this.isLoading.set(true);
    this.errorMessage.set(null);

    this.genFlowService.getOrCreateFlow().subscribe({
      next: (flow) => {
        this.flow.set(flow);
        this.currentStep.set(flow.current_step);
        this.answers.set({ ...flow.answers });
        const parsed = parseStoredImageStyle(flow.image_style);
        this.imageStyleSelection.set(parsed.selection);
        this.customImageStyle.set(parsed.custom);
        this.referenceImageId.set(flow.reference_image_id ?? null);
        // If a reference image exists, use it as the cover immediately.
        if (flow.reference_image_id) {
          this.coverImageId.set(flow.reference_image_id);
        }
        this.isEditingAnswers.set(false);
        this.isManualNameMode.set(false);
        this.manualName.set('');
        this.isLoading.set(false);
        if (flow.status === 'generated') {
          this.maybeStartPortrait();
        } else {
          this.resumeGenerationPolling(flow.id);
        }
      },
      error: (error) => {
        console.error('Failed to load generation flow:', error);
        this.errorMessage.set('Failed to load personality generation flow. Please try again.');
        this.isLoading.set(false);
      },
    });
  }

  getAnswer(questionId: string): string {
    return this.answers()[questionId] || '';
  }

  setAnswer(questionId: string, value: string): void {
    this.answers.update((prev) => ({ ...prev, [questionId]: value }));
  }

  currentPageData(): WizardPage {
    return this.pages[this.currentStep()];
  }

  canGoNext(): boolean {
    return this.currentStep() < this.totalSteps - 1;
  }

  canGoBack(): boolean {
    return this.currentStep() > 0;
  }

  isLastPage(): boolean {
    return this.currentStep() === this.totalSteps - 1;
  }

  jumpToStep(step: number): void {
    if (this.isReviewScreen() || step === this.currentStep()) return;
    this.currentStep.set(step);
    this.saveProgress(step);
  }

  goNext(): void {
    if (!this.canGoNext()) return;
    const nextStep = this.currentStep() + 1;
    this.currentStep.set(nextStep);
    this.saveProgress(nextStep);
  }

  goBack(): void {
    if (!this.canGoBack()) return;
    const prevStep = this.currentStep() - 1;
    this.currentStep.set(prevStep);
    this.saveProgress(prevStep);
  }

  private saveProgress(step: number): void {
    const f = this.flow();
    if (!f) return;

    this.isSaving.set(true);
    const refId = this.referenceImageId();
    this.genFlowService.updateFlow(f.id, {
      current_step: step,
      answers: this.answers(),
      image_style: this.persistedImageStyle(),
      ...(refId ? { reference_image_id: refId } : {}),
    }).subscribe({
      next: (updated) => {
        this.flow.set(updated);
        this.isSaving.set(false);
      },
      error: (err) => {
        console.error('Failed to save progress:', err);
        this.isSaving.set(false);
      },
    });
  }

  finishAndGenerate(): void {
    const f = this.flow();
    if (!f) return;
    const currentAnswers = this.answers();

    if (!this.hasAtLeastOneAnswer(currentAnswers)) {
      this.errorMessage.set('You must set at least one field to generate a personality.');
      return;
    }

    if (
      this.isEditingAnswers() &&
      f.status === 'generated' &&
      this.areAnswersEquivalent(currentAnswers, f.answers)
    ) {
      // Answers are unchanged — reuse existing generated output.
      this.isEditingAnswers.set(false);
      this.errorMessage.set(null);
      return;
    }

    // Save the last page first, then trigger generation.
    this.isGenerating.set(true);
    this.errorMessage.set(null);

    const refIdForGen = this.referenceImageId();
    this.genFlowService.updateFlow(f.id, {
      current_step: this.currentStep(),
      answers: this.answers(),
      image_style: this.persistedImageStyle(),
      ...(refIdForGen ? { reference_image_id: refIdForGen } : {}),
    }).subscribe({
      next: () => {
        this.genFlowService.completeFlow(f.id).subscribe({
          next: (enqueued) => {
            this.pollGenerationJob(f.id, enqueued.job_id);
          },
          error: (err) => {
            if (err?.status === 409 && err?.error?.active?.job_id) {
              this.pollGenerationJob(f.id, err.error.active.job_id);
              return;
            }
            console.error('Generation failed:', err);
            this.errorMessage.set('AI generation failed. Please try again.');
            this.isGenerating.set(false);
          },
        });
      },
      error: (err) => {
        console.error('Failed to save before generation:', err);
        this.errorMessage.set('Failed to save your answers. Please try again.');
        this.isGenerating.set(false);
      },
    });
  }

  nextName(): void {
    const names = this.generatedNames();
    if (names.length === 0) return;
    this.isManualNameMode.set(false);
    this.selectedNameIndex.update((i) => (i + 1) % names.length);
  }

  prevName(): void {
    const names = this.generatedNames();
    if (names.length === 0) return;
    this.isManualNameMode.set(false);
    this.selectedNameIndex.update((i) => (i - 1 + names.length) % names.length);
  }

  toggleManualNameMode(): void {
    if (this.isManualNameMode()) {
      this.isManualNameMode.set(false);
      return;
    }
    this.manualName.set(this.selectedGeneratedName());
    this.isManualNameMode.set(true);
  }

  regenerate(): void {
    const f = this.flow();
    if (!f) return;

    this.isGenerating.set(true);
    this.errorMessage.set(null);
    this.selectedNameIndex.set(0);
    this.resetPortraitJob();

    this.genFlowService.regenerateFlow(f.id).subscribe({
      next: (enqueued) => {
        this.pollGenerationJob(f.id, enqueued.job_id);
      },
      error: (err) => {
        if (err?.status === 409 && err?.error?.active?.job_id) {
          this.pollGenerationJob(f.id, err.error.active.job_id);
          return;
        }
        console.error('Regeneration failed:', err);
        this.errorMessage.set('AI generation failed. Please try again.');
        this.isGenerating.set(false);
      },
    });
  }

  accept(): void {
    const f = this.flow();
    if (!f || this.isAccepting()) return;

    this.isAccepting.set(true);
    this.errorMessage.set(null);

    const acceptReq: { name: string; cover_image_id?: string } = { name: this.selectedName() };
    const coverId = this.coverImageId();
    if (coverId) {
      acceptReq.cover_image_id = coverId;
    }

    this.genFlowService.acceptFlow(f.id, acceptReq).subscribe({
      next: async (personality) => {
        try {
          const chat = await firstValueFrom(
            this.chatService.createChat({
              name: 'New Chat',
              personality_id: personality.id,
            }),
          );
          this.chatService.setLastChatId(chat.id);
          if (this.modalMode()) {
            this.generateModal.notifyPersonalityCreated(personality);
            this.closeModal.emit();
          }
          this.isAccepting.set(false);
          await this.router.navigate(['/chat', chat.id], {
            queryParams: { welcome: 'true' },
          });
        } catch (chatError) {
          console.error('Failed to create chat after personality accept:', chatError);
          this.isAccepting.set(false);
          try {
            await this.confirmationService.alert({
              message: 'Personality was created, but we could not open a new chat automatically.',
              type: 'danger',
            });
          } catch (alertError) {
            console.error('Error showing alert:', alertError);
          }
          await this.router.navigate(['/personality', personality.id]);
        }
      },
      error: async (err) => {
        console.error('Accept failed:', err);
        this.isAccepting.set(false);
        try {
          await this.confirmationService.alert({
            message: 'Failed to create personality. Please try again.',
            type: 'danger',
          });
        } catch (alertError) {
          console.error('Error showing alert:', alertError);
        }
      },
    });
  }

  async resetDraft(): Promise<void> {
    const f = this.flow();
    if (!f || this.isResetting() || this.isSaving() || this.isGenerating() || this.isAccepting()) return;

    let confirmed = false;
    try {
      confirmed = await this.confirmationService.confirm({
        title: 'Start Over?',
        message: 'This will discard your current draft personality and start from step 1.',
        confirmText: 'Start Over',
        cancelText: 'Keep Draft',
        type: 'warning',
      });
    } catch {
      confirmed = false;
    }
    if (!confirmed) return;

    this.isResetting.set(true);
    this.errorMessage.set(null);

    this.genFlowService.resetFlow(f.id).subscribe({
      next: (resetFlow) => {
        this.flow.set(resetFlow);
        this.currentStep.set(resetFlow.current_step);
        this.answers.set({ ...resetFlow.answers });
        const parsed = parseStoredImageStyle(resetFlow.image_style);
        this.imageStyleSelection.set(parsed.selection);
        this.customImageStyle.set(parsed.custom);
        this.referenceImageId.set(resetFlow.reference_image_id ?? null);
        this.resetPortraitJob();
        this.selectedNameIndex.set(0);
        this.isManualNameMode.set(false);
        this.manualName.set('');
        this.isEditingAnswers.set(false);
        this.isGenerating.set(false);
        this.isAccepting.set(false);
        this.isSaving.set(false);
        this.isResetting.set(false);
      },
      error: async (err) => {
        console.error('Reset flow failed:', err);
        this.isResetting.set(false);
        this.errorMessage.set('Failed to reset draft. Please try again.');
        try {
          await this.confirmationService.alert({
            message: 'Failed to reset draft personality. Please try again.',
            type: 'danger',
          });
        } catch {
          // Ignore alert errors.
        }
      },
    });
  }

  goBackToEdit(): void {
    const f = this.flow();
    if (!f) return;

    // Keep the generated flow untouched until answers actually change.
    // This lets users return to the existing output without re-generating.
    this.isEditingAnswers.set(true);
    this.currentStep.set(this.totalSteps - 1);
    this.errorMessage.set(null);
  }

  goBackToList(): void {
    this.router.navigate(['/personality']);
  }

  closeOrBack(): void {
    if (this.modalMode()) {
      this.closeModal.emit();
      return;
    }
    this.goBackToList();
  }

  progressPercent(): number {
    if (this.isReviewScreen()) return 100;
    return Math.round(((this.currentStep() + 1) / this.totalSteps) * 100);
  }

  private hasAtLeastOneAnswer(answers: Record<string, string>): boolean {
    return Object.values(answers).some((value) => !!value?.trim());
  }

  private resetPortraitJob(): void {
    this.portraitJobStarted = false;
    this.portraitGenerating.set(false);
    // Preserve the reference image as cover when clearing the generated portrait state.
    const refId = this.referenceImageId();
    this.coverImageId.set(refId ?? null);
  }

  private maybeStartPortrait(): void {
    const f = this.flow();
    if (!f || f.status !== 'generated' || this.portraitJobStarted) {
      return;
    }

    // When style is "none" or a reference image is provided, no generation is needed.
    // The reference image is already set as coverImageId in loadFlow / uploadReferenceImage.
    if (this.imageStyleSelection() === IMAGE_STYLE_NONE || this.referenceImageId() !== null) {
      return;
    }

    this.portraitJobStarted = true;
    this.portraitGenerating.set(true);
    this.mediaJobs
      .startFlowPortrait(f.id)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
      next: enqueued => this.pollPortraitJob(enqueued.job_id),
      error: err => {
        if (err?.status === 409 && err?.error?.active?.job_id) {
          this.pollPortraitJob(err.error.active.job_id);
          return;
        }
        this.portraitGenerating.set(false);
        this.portraitJobStarted = false;
      },
    });
  }

  private resumeGenerationPolling(flowId: string): void {
    this.genFlowService
      .getActiveGenerationJob(flowId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (active) => {
          if (active?.job_type === 'personality_generation') {
            this.pollGenerationJob(flowId, active.job_id);
          }
        },
        error: () => {
          // Non-blocking: the wizard can still function even if resume lookup fails.
        },
      });
  }

  private pollGenerationJob(flowId: string, jobId: string): void {
    this.isGenerating.set(true);
    this.mediaJobs
      .pollUntilTerminal(jobId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (job) => {
          if (job.status === 'complete') {
            this.refreshFlowAfterGeneration(flowId);
          } else if (job.status === 'failed') {
            this.isGenerating.set(false);
            this.errorMessage.set(job.error || 'AI generation failed. Please try again.');
          }
        },
        error: () => {
          this.isGenerating.set(false);
          this.errorMessage.set('Failed to track generation status. Please try again.');
        },
      });
  }

  private refreshFlowAfterGeneration(flowId: string): void {
    this.genFlowService.getFlow(flowId).subscribe({
      next: (updated) => {
        this.flow.set(updated);
        this.currentStep.set(updated.current_step);
        this.answers.set({ ...updated.answers });
        this.isEditingAnswers.set(false);
        this.isManualNameMode.set(false);
        this.manualName.set('');
        this.isGenerating.set(false);
        this.resetPortraitJob();
        this.maybeStartPortrait();
      },
      error: (err) => {
        console.error('Failed to refresh generated flow:', err);
        this.errorMessage.set('Generation finished, but we could not load the result. Please refresh.');
        this.isGenerating.set(false);
      },
    });
  }

  private pollPortraitJob(jobId: string): void {
    this.mediaJobs
      .pollUntilTerminal(jobId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
      next: job => {
        if (job.status === 'complete' && job.result_id) {
          this.coverImageId.set(job.result_id);
          this.portraitGenerating.set(false);
        } else if (job.status === 'failed') {
          this.portraitGenerating.set(false);
        }
      },
      error: () => {
        this.portraitGenerating.set(false);
      },
    });
  }

  selectImageStyle(value: string): void {
    this.imageStyleSelection.set(value);
    if (value !== IMAGE_STYLE_OTHER) {
      this.customImageStyle.set('');
    }
  }

  isImageStyleSelected(value: string): boolean {
    return this.imageStyleSelection() === value;
  }

  imageStylePillClass(style: (typeof IMAGE_STYLES)[number]): string {
    const selected = this.isImageStyleSelected(style.value);
    const emphasis = 'emphasis' in style ? style.emphasis : undefined;
    if (emphasis === 'auto') {
      return selected
        ? 'bg-emerald-600 text-white border-emerald-600 dark:bg-emerald-500 dark:border-emerald-500'
        : 'bg-emerald-50 text-emerald-900 border-emerald-300 dark:bg-emerald-950 dark:text-emerald-200 dark:border-emerald-700';
    }
    if (emphasis === 'none') {
      return selected
        ? 'bg-amber-600 text-white border-amber-600 dark:bg-amber-500 dark:border-amber-500'
        : 'bg-amber-50 text-amber-900 border-amber-300 dark:bg-amber-950 dark:text-amber-200 dark:border-amber-700';
    }
    if (emphasis === 'other') {
      return selected
        ? 'bg-violet-600 text-white border-violet-600 dark:bg-violet-500 dark:border-violet-500'
        : 'bg-violet-50 text-violet-900 border-violet-300 dark:bg-violet-950 dark:text-violet-200 dark:border-violet-700';
    }
    return selected
      ? 'bg-indigo-600 text-white border-indigo-600 dark:bg-indigo-500 dark:border-indigo-500'
      : 'bg-white text-gray-700 border-gray-300 dark:bg-gray-700 dark:text-gray-300 dark:border-gray-600';
  }

  portraitImageUrl(): string | null {
    const id = this.coverImageId();
    return id ? this.imageGallery.getImageUrl(id, 'full') : null;
  }

  /** Whether to show the portrait section on the review screen. */
  showPortraitSection(): boolean {
    return this.imageStyleSelection() !== IMAGE_STYLE_NONE;
  }

  /** Label for the portrait section header. */
  portraitSectionLabel(): string {
    return this.referenceImageId() ? 'Reference Image' : 'Generated Portrait';
  }

  uploadReferenceImage(event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;

    const f = this.flow();
    if (!f) return;

    this.imageUploadLoading.set(true);
    this.imageGallery.importImage(file, { title: 'Personality reference image' })
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (att) => {
          this.referenceImageId.set(att.id);
          this.coverImageId.set(att.id);
          this.imageUploadLoading.set(false);
          // Persist to backend; rollback UI signals if the save fails.
          this.genFlowService.updateFlow(f.id, {
            current_step: this.currentStep(),
            answers: this.answers(),
            image_style: this.persistedImageStyle(),
            reference_image_id: att.id,
          }).subscribe({
            error: async (err) => {
              console.error('Failed to save reference image:', err);
              this.referenceImageId.set(null);
              this.coverImageId.set(null);
              try {
                await this.confirmationService.alert({
                  message: 'Failed to save reference image. Please try again.',
                  type: 'danger',
                });
              } catch { /* ignore */ }
            },
          });
        },
        error: (err) => {
          console.error('Failed to upload reference image:', err);
          this.imageUploadLoading.set(false);
        },
      });
  }

  clearReferenceImage(): void {
    const f = this.flow();
    const prevRefId = this.referenceImageId();
    const prevCoverId = this.coverImageId();
    // Optimistically clear, then rollback if the backend save fails.
    this.referenceImageId.set(null);
    this.coverImageId.set(null);
    if (f) {
      this.genFlowService.updateFlow(f.id, {
        current_step: this.currentStep(),
        answers: this.answers(),
        image_style: this.persistedImageStyle(),
      }).subscribe({
        error: async (err) => {
          console.error('Failed to clear reference image:', err);
          this.referenceImageId.set(prevRefId);
          this.coverImageId.set(prevCoverId);
          try {
            await this.confirmationService.alert({
              message: 'Failed to remove reference image. Please try again.',
              type: 'danger',
            });
          } catch { /* ignore */ }
        },
      });
    }
  }

  private areAnswersEquivalent(a: Record<string, string>, b: Record<string, string>): boolean {
    const normalize = (answers: Record<string, string>): Record<string, string> => {
      const out: Record<string, string> = {};
      for (const [key, value] of Object.entries(answers)) {
        const trimmed = value?.trim() ?? '';
        if (trimmed !== '') {
          out[key] = trimmed;
        }
      }
      return out;
    };

    const aNorm = normalize(a);
    const bNorm = normalize(b);
    const aKeys = Object.keys(aNorm);
    const bKeys = Object.keys(bNorm);
    if (aKeys.length !== bKeys.length) return false;
    return aKeys.every((key) => aNorm[key] === bNorm[key]);
  }
}
