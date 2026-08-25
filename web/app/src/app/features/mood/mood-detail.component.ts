import { Component, inject, signal, OnInit, computed, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Router, ActivatedRoute } from '@angular/router';
import { AuthImagePipe } from '../../core/pipes/auth-image.pipe';
import { MoodService } from '../../core/services/mood.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { RitualService } from '../../core/services/ritual.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ModelService } from '../../core/services/model.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { Mood, UpdateMoodRequest, AttachMoodToPersonalitiesRequest } from '../../core/models/mood.model';
import { MODE_PLURAL, MODE_SINGULAR } from './mode-ui.labels';
import { FileAttachment } from '../../core/models/file-attachment.model';
import { Ritual } from '../../core/models/ritual.model';
import { Personality } from '../../core/models/personality.model';
import { Model } from '../../core/models/model.model';

@Component({
  selector: 'app-mood-detail',
  standalone: true,
  imports: [CommonModule, FormsModule, AuthImagePipe],
  templateUrl: './mood-detail.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./mood-detail.component.scss']
})
export class MoodDetailComponent implements OnInit {
  readonly modeSingular = MODE_SINGULAR;
  readonly modePlural = MODE_PLURAL;
  private moodService = inject(MoodService);
  private galleryService = inject(ImageGalleryService);
  private ritualService = inject(RitualService);
  private personalityService = inject(PersonalityService);
  private modelService = inject(ModelService);
  private confirmationService = inject(ConfirmationService);
  private router = inject(Router);
  private route = inject(ActivatedRoute);

  mood = signal<Mood | null>(null);
  isLoading = signal(true);
  isSaving = signal(false);
  saveError = signal<string | null>(null);
  saveSuccess = signal(false);

  // Edit form state (mirrors the mood)
  formName = signal('');
  formDescription = signal('');
  formPromptSnippet = signal('');
  formRecommendedModel = signal('');
  formImageIds = signal<string[]>([]);
  formRitualIds = signal<string[]>([]);

  // Gallery picker
  galleryImages = signal<FileAttachment[]>([]);
  isGalleryPickerOpen = signal(false);
  isLoadingGallery = signal(false);

  // Ritual picker
  allRituals = signal<Ritual[]>([]);
  isRitualPickerOpen = signal(false);
  isLoadingRituals = signal(false);
  ritualNameById = computed(() => {
    const byID = new Map<string, string>();
    for (const ritual of this.allRituals()) {
      byID.set(ritual.id, ritual.name);
    }
    return byID;
  });

  // Personalities multi-select
  personalities = signal<Personality[]>([]);
  availableModels = signal<Model[]>([]);
  /** Set of personality IDs this mood is currently attached to. */
  selectedPersonalityIds = signal<Set<string>>(new Set());
  isSavingPersonalities = signal(false);

  thumbnailUrl = computed(() => {
    const k = this.mood();
    return k?.thumbnail_data ? `data:image/jpeg;base64,${k.thumbnail_data}` : null;
  });

  ngOnInit(): void {
    const id = this.route.snapshot.paramMap.get('id');
    if (!id) { this.router.navigate(['/mode']); return; }
    this.loadMood(id);
    this.loadPersonalities();
    this.loadModels();
    this.loadRitualsIfNeeded();
  }

  loadMood(id: string): void {
    this.isLoading.set(true);
    this.moodService.getMood(id).subscribe({
      next: (mood) => {
        this.mood.set(mood);
        this.formName.set(mood.name);
        this.formDescription.set(mood.description);
        this.formPromptSnippet.set(mood.prompt_snippet);
        this.formRecommendedModel.set(mood.recommended_model ?? '');
        this.formImageIds.set([...(mood.image_ids ?? [])]);
        this.formRitualIds.set([...(mood.ritual_ids ?? [])]);
        // Initialise personality selection from mood's personality_ids.
        this.selectedPersonalityIds.set(new Set(mood.personality_ids ?? []));
        this.isLoading.set(false);
      },
      error: () => {
        this.isLoading.set(false);
        this.router.navigate(['/mode']);
      }
    });
  }

  loadPersonalities(): void {
    this.personalityService.listPersonalities(1, 100).subscribe({
      next: (res) => {
        this.personalities.set(res.results as Personality[]);
      },
      error: () => {}
    });
  }

  loadModels(): void {
    this.modelService.getModels().subscribe({
      next: (models) => this.availableModels.set(models ?? []),
      error: () => this.availableModels.set([])
    });
  }

  saveMood(): void {
    const mood = this.mood();
    if (!mood) return;
    const name = this.formName().trim();
    if (!name) { this.saveError.set('Name is required.'); return; }

    this.isSaving.set(true);
    this.saveError.set(null);
    const req: UpdateMoodRequest = {
      name,
      description: this.formDescription(),
      prompt_snippet: this.formPromptSnippet(),
      recommended_model: this.formRecommendedModel(),
      image_ids: this.formImageIds(),
      ritual_ids: this.formRitualIds()
    };

    this.moodService.updateMood(mood.id, req).subscribe({
      next: (updated) => {
        this.mood.set(updated);
        this.isSaving.set(false);
        this.saveSuccess.set(true);
        setTimeout(() => this.saveSuccess.set(false), 2500);
      },
      error: () => {
        this.isSaving.set(false);
        this.saveError.set(`Failed to save ${MODE_SINGULAR.toLowerCase()}. Please try again.`);
      }
    });
  }

  // ── Gallery picker ──────────────────────────────────────────────────────────

  openGalleryPicker(): void {
    this.isGalleryPickerOpen.set(true);
    if (this.galleryImages().length === 0) {
      this.isLoadingGallery.set(true);
      this.galleryService.listImages(1, 40).subscribe({
        next: (res) => {
          this.galleryImages.set(res.results as FileAttachment[]);
          this.isLoadingGallery.set(false);
        },
        error: () => this.isLoadingGallery.set(false)
      });
    }
  }

  closeGalleryPicker(): void {
    this.isGalleryPickerOpen.set(false);
  }

  selectImage(id: string): void {
    // Moods support exactly one image; toggling the same one deselects it.
    this.formImageIds.set(this.formImageIds().includes(id) ? [] : [id]);
    this.closeGalleryPicker();
  }

  isImageSelected(id: string): boolean {
    return this.formImageIds().includes(id);
  }

  getGalleryThumbUrl(image: FileAttachment): string {
    return this.galleryService.getImageUrl(image.id, 'thumbnail');
  }

  getGalleryThumbUrlById(id: string): string {
    return this.galleryService.getImageUrl(id, 'thumbnail');
  }

  // ── Ritual picker ───────────────────────────────────────────────────────────

  openRitualPicker(): void {
    this.isRitualPickerOpen.set(true);
    this.loadRitualsIfNeeded();
  }

  closeRitualPicker(): void {
    this.isRitualPickerOpen.set(false);
  }

  toggleRitual(id: string): void {
    this.formRitualIds.update(ids =>
      ids.includes(id) ? ids.filter(i => i !== id) : [...ids, id]
    );
  }

  isRitualSelected(id: string): boolean {
    return this.formRitualIds().includes(id);
  }

  getRitualNameById(id: string): string {
    return this.ritualNameById().get(id) ?? `Skill ${id.slice(0, 8)}`;
  }

  private loadRitualsIfNeeded(): void {
    if (this.allRituals().length > 0 || this.isLoadingRituals()) {
      return;
    }
    this.isLoadingRituals.set(true);
    this.ritualService.listRituals(1, 100).subscribe({
      next: (res) => {
        this.allRituals.set(res.results as Ritual[]);
        this.isLoadingRituals.set(false);
      },
      error: () => this.isLoadingRituals.set(false)
    });
  }

  // ── Personality multi-select ─────────────────────────────────────────────────

  togglePersonality(id: string): void {
    this.selectedPersonalityIds.update(set => {
      const next = new Set(set);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  }

  isPersonalitySelected(id: string): boolean {
    return this.selectedPersonalityIds().has(id);
  }

  savePersonalities(): void {
    const mood = this.mood();
    if (!mood) return;
    this.isSavingPersonalities.set(true);
    const req: AttachMoodToPersonalitiesRequest = {
      personality_ids: Array.from(this.selectedPersonalityIds())
    };
    this.moodService.attachToPersonalities(mood.id, req).subscribe({
      next: () => {
        this.isSavingPersonalities.set(false);
        this.saveSuccess.set(true);
        setTimeout(() => this.saveSuccess.set(false), 2500);
      },
      error: async () => {
        this.isSavingPersonalities.set(false);
        await this.confirmationService.alert({ message: 'Failed to update personality associations.', type: 'danger' });
      }
    });
  }

  goBack(): void {
    this.router.navigate(['/mode']);
  }
}
