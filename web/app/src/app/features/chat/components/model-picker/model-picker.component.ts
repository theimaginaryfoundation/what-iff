import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, ElementRef, HostListener, computed, inject, input, output, signal } from '@angular/core';

import { Model } from '../../../../core/models/model.model';
import {
  compareModelsByName,
  modelTierCompactLabel,
  modelTierRank,
  sortedModelsByName,
} from '../../../../core/utils/model-display.helpers';
import {
  ModelTierGroup,
  groupModelsByTier,
  modelProvider,
  providerLabel as formatProviderLabel,
  sortedProviders,
} from '../../helpers/model-picker.helpers';
import { ModelFavoritesService } from '../../services/model-favorites.service';

type VendorStep = 'vendor' | 'model';
type TierStep = 'tier' | 'model';
/** Selected tier bucket: 1–4, or `'other'` for untiered models. */
type TierKey = number | 'other';
type ViewMode = 'favorites' | 'vendor' | 'tier';

@Component({
  selector: 'app-model-picker',
  standalone: true,
  imports: [CommonModule],
  template: `
    <div class="model-picker">
      <button
        class="model-picker__trigger"
        type="button"
        role="combobox"
        aria-controls="model-picker-options"
        [attr.aria-expanded]="open()"
        [disabled]="disabled()"
        (click)="toggle()"
      >
        <span>{{ selectedModel()?.display_name || 'Model' }}</span>
      </button>
      @if (open()) {
        <div
          id="model-picker-options"
          class="model-picker__options"
          [class.model-picker__options--below]="menuPlacement() === 'below'"
          [class.model-picker__options--above]="menuPlacement() === 'above'"
        >
          <div class="model-picker__tabs" role="tablist" aria-label="Browse models by">
            <button
              type="button"
              role="tab"
              class="model-picker__tab"
              [class.model-picker__tab--active]="viewMode() === 'favorites'"
              [attr.aria-selected]="viewMode() === 'favorites'"
              (click)="setViewMode('favorites')"
            >
              <span aria-hidden="true">★</span> Favorites
            </button>
            <button
              type="button"
              role="tab"
              class="model-picker__tab"
              [class.model-picker__tab--active]="viewMode() === 'vendor'"
              [attr.aria-selected]="viewMode() === 'vendor'"
              (click)="setViewMode('vendor')"
            >
              Vendor
            </button>
            <button
              type="button"
              role="tab"
              class="model-picker__tab"
              [class.model-picker__tab--active]="viewMode() === 'tier'"
              [attr.aria-selected]="viewMode() === 'tier'"
              (click)="setViewMode('tier')"
            >
              Tier
            </button>
          </div>

          @if (favoritesError(); as favoritesError) {
            <p class="model-picker__error" role="alert">{{ favoritesError }}</p>
          }

          @if (showBack()) {
            <button type="button" class="model-picker__back" (click)="onBack()">
              ← {{ backLabel() }}
            </button>
          }

          <div class="model-picker__panel" role="listbox">
            @switch (viewMode()) {
              @case ('favorites') {
                @if (favoriteModels().length) {
                  @for (model of favoriteModels(); track model.id) {
                    <ng-container [ngTemplateOutlet]="modelRow" [ngTemplateOutletContext]="{ $implicit: model }" />
                  }
                } @else {
                  <p class="model-picker__empty">
                    No favorites yet. Tap the ★ next to any model to pin it here.
                  </p>
                }
              }
              @case ('tier') {
                @if (tierStep() === 'tier') {
                  @for (group of tierGroups(); track group.label) {
                    <button
                      type="button"
                      class="model-picker__vendor"
                      (click)="chooseTier(group)"
                    >
                      <span class="model-picker__vendor-label">{{ group.label }}</span>
                      <span class="model-picker__vendor-count">{{ group.models.length }} models</span>
                    </button>
                  }
                } @else {
                  @for (model of tierModels(); track model.id) {
                    <ng-container [ngTemplateOutlet]="modelRow" [ngTemplateOutletContext]="{ $implicit: model }" />
                  }
                }
              }
              @default {
                @if (vendorStep() === 'vendor') {
                  @for (provider of availableProviders(); track provider) {
                    <button
                      type="button"
                      class="model-picker__vendor"
                      (click)="chooseProvider(provider)"
                    >
                      <span class="model-picker__vendor-label">{{ providerLabel(provider) }}</span>
                      <span class="model-picker__vendor-count">{{ providerModelCount(provider) }} models</span>
                    </button>
                  }
                } @else {
                  @for (model of providerModels(); track model.id) {
                    <ng-container [ngTemplateOutlet]="modelRow" [ngTemplateOutletContext]="{ $implicit: model }" />
                  }
                }
              }
            }
          </div>
        </div>
      }
    </div>

    <ng-template #modelRow let-model>
      <div class="model-picker__row" [class.model-picker__row--selected]="model.id === selectedId()">
        <button
          type="button"
          role="option"
          class="model-picker__option"
          [attr.aria-selected]="model.id === selectedId()"
          (click)="choose(model)"
        >
          <span class="model-picker__option-details">
            <span class="model-picker__option-name">{{ model.display_name }}</span>
            @if (model.capabilities; as capabilities) {
              <span class="model-picker__capabilities" [attr.aria-label]="capabilityAriaLabel(model)">
                @if (capabilities.tool_calling) {
                  <span class="model-picker__capability" [attr.title]="toolCapabilityTitle(model)">
                    Tools {{ capabilities.tools.length }}
                  </span>
                } @else {
                  <span class="model-picker__capability">No tools</span>
                }
                @if (capabilities.vision) {
                  <span class="model-picker__capability">Vision</span>
                }
                @if (capabilities.mcp) {
                  <span class="model-picker__capability">MCP</span>
                }
              </span>
            }
          </span>
          @if (tierLabel(model); as tier) {
            <span class="model-picker__tier">{{ tier }}</span>
          }
        </button>
        <button
          type="button"
          class="model-picker__star"
          [class.model-picker__star--on]="isFavorite(model)"
          [attr.aria-pressed]="isFavorite(model)"
          [attr.aria-label]="(isFavorite(model) ? 'Remove ' : 'Add ') + model.display_name + (isFavorite(model) ? ' from favorites' : ' to favorites')"
          (click)="toggleFavorite(model, $event)"
        >{{ isFavorite(model) ? '★' : '☆' }}</button>
      </div>
    </ng-template>
  `,
  styles: [`
    :host {
      display: block;
      height: 2.25rem;
    }

    .model-picker {
      height: 2.25rem;
      position: relative;
    }

    .model-picker__trigger {
      align-items: center;
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      box-sizing: border-box;
      color: var(--color-text-secondary);
      cursor: pointer;
      display: inline-flex;
      font-size: 0.6875rem;
      height: 2.25rem;
      line-height: 1;
      max-width: 8rem;
      padding: 0 0.5rem;
      white-space: nowrap;
    }

    .model-picker__trigger span {
      overflow: hidden;
      text-overflow: ellipsis;
    }

    button:disabled {
      cursor: not-allowed;
      opacity: 0.5;
    }

    .model-picker__options {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 1rem;
      box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 20%, transparent);
      display: flex;
      flex-direction: column;
      gap: 0.25rem;
      left: 0;
      max-height: min(60dvh, 18rem);
      min-width: min(18rem, calc(100vw - 1.5rem));
      overflow: hidden;
      padding: 0.375rem;
      position: absolute;
      z-index: 50;
    }

    .model-picker__options--above {
      bottom: calc(100% + 0.5rem);
    }

    .model-picker__options--below {
      top: calc(100% + 0.5rem);
    }

    .model-picker__panel {
      display: grid;
      gap: 0.25rem;
      min-height: 0;
      overflow-y: auto;
      overscroll-behavior: contain;
      -webkit-overflow-scrolling: touch;
    }

    .model-picker__tabs {
      background: var(--color-surface-muted);
      border-radius: 0.625rem;
      display: grid;
      flex-shrink: 0;
      gap: 0.125rem;
      grid-template-columns: repeat(3, 1fr);
      padding: 0.1875rem;
    }

    .model-picker__tab {
      background: transparent;
      border: 0;
      border-radius: 0.5rem;
      color: var(--color-text-secondary);
      cursor: pointer;
      font-size: 0.6875rem;
      font-weight: 700;
      min-height: 1.875rem;
      padding: 0.25rem 0.375rem;
      white-space: nowrap;
    }

    .model-picker__tab:hover:not(.model-picker__tab--active) {
      color: var(--color-text-primary);
    }

    .model-picker__tab--active {
      background: var(--color-surface-base);
      box-shadow: 0 1px 2px color-mix(in srgb, black 12%, transparent);
      color: var(--color-text-primary);
    }

    .model-picker__empty {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      line-height: 1.5;
      margin: 0;
      padding: 0.75rem;
      text-align: center;
    }

    .model-picker__error {
      color: var(--color-danger, #dc2626);
      font-size: 0.75rem;
      line-height: 1.5;
      margin: 0;
      padding: 0.5rem 0.75rem;
      text-align: center;
    }

    .model-picker__row {
      align-items: center;
      border-radius: 0.75rem;
      display: flex;
      gap: 0.125rem;
    }

    .model-picker__row--selected,
    .model-picker__row:hover {
      background: var(--color-surface-muted);
    }

    .model-picker__star {
      align-items: center;
      background: transparent;
      border: 0;
      border-radius: 0.5rem;
      color: var(--color-text-muted);
      cursor: pointer;
      display: flex;
      flex-shrink: 0;
      font-size: 1rem;
      justify-content: center;
      line-height: 1;
      margin-right: 0.25rem;
      min-height: 2rem;
      width: 2rem;
    }

    .model-picker__star:hover {
      color: var(--color-accent, #8c87ff);
    }

    .model-picker__star--on {
      color: var(--color-accent, #8c87ff);
    }

    .model-picker__vendor,
    .model-picker__option,
    .model-picker__back {
      align-items: center;
      background: transparent;
      border: 0;
      border-radius: 0.75rem;
      color: var(--color-text-primary);
      display: flex;
      font-size: 0.8125rem;
      font-weight: 600;
      gap: 0.5rem;
      justify-content: space-between;
      min-height: 2.75rem;
      padding: 0.625rem 0.75rem;
      text-align: left;
      width: 100%;
    }

    .model-picker__option {
      cursor: pointer;
      flex: 1;
      min-width: 0;
    }

    .model-picker__back {
      background: var(--color-surface-base);
      color: var(--color-text-secondary);
      flex-shrink: 0;
      font-size: 0.75rem;
      font-weight: 700;
      justify-content: flex-start;
      min-height: 2.25rem;
    }

    .model-picker__vendor-label {
      font-size: 0.875rem;
      font-weight: 700;
    }

    .model-picker__vendor-count {
      color: var(--color-text-muted);
      font-size: 0.6875rem;
      font-weight: 600;
    }

    .model-picker__option-details {
      display: grid;
      gap: 0.25rem;
      min-width: 0;
    }

    .model-picker__option-name {
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .model-picker__capabilities {
      display: flex;
      flex-wrap: wrap;
      gap: 0.25rem;
    }

    .model-picker__capability {
      background: var(--color-surface-muted);
      border: 1px solid var(--color-border-base);
      border-radius: 999px;
      color: var(--color-text-muted);
      font-size: 0.5625rem;
      font-weight: 700;
      line-height: 1.4;
      padding: 0.05rem 0.3rem;
      white-space: nowrap;
    }

    .model-picker__tier {
      color: var(--color-text-primary);
      flex-shrink: 0;
      font-size: 0.625rem;
      font-weight: 700;
      letter-spacing: 0.02em;
    }

    .model-picker__vendor:hover,
    .model-picker__back:hover {
      background: var(--color-surface-muted);
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ModelPickerComponent {
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly favoritesService = inject(ModelFavoritesService);
  readonly models = input<readonly Model[]>([]);
  readonly selectedId = input<string | null>(null);
  readonly disabled = input(false);
  readonly selected = output<Model>();
  readonly open = signal(false);
  readonly viewMode = signal<ViewMode>('vendor');
  readonly vendorStep = signal<VendorStep>('vendor');
  readonly activeProvider = signal<string | null>(null);
  readonly tierStep = signal<TierStep>('tier');
  readonly activeTier = signal<TierKey | null>(null);
  readonly menuPlacement = signal<'above' | 'below'>('above');
  readonly selectedModel = computed(() => this.models().find(model => model.id === this.selectedId()));
  readonly sortedModels = computed(() => sortedModelsByName(this.models()));
  readonly availableProviders = computed(() => {
    const providers = this.sortedModels().map(model => modelProvider(model));
    return sortedProviders(providers);
  });
  readonly providerModels = computed(() => {
    const provider = this.activeProvider();
    if (!provider) {
      return [];
    }
    return this.sortedModels().filter(model => modelProvider(model) === provider);
  });
  readonly tierGroups = computed<ModelTierGroup[]>(() => groupModelsByTier(this.models()));
  readonly tierModels = computed(() => {
    const key = this.activeTier();
    if (key === null) {
      return [];
    }
    const group = this.tierGroups().find(g => tierGroupKey(g) === key);
    return group?.models ?? [];
  });
  readonly activeTierLabel = computed(() => {
    const key = this.activeTier();
    if (key === null) {
      return 'Tiers';
    }
    return key === 'other' ? 'Other' : `Tier ${key}`;
  });
  /** True when drilled into a vendor or tier model list (back sits outside the scroll panel). */
  readonly showBack = computed(
    () =>
      (this.viewMode() === 'vendor' && this.vendorStep() === 'model') ||
      (this.viewMode() === 'tier' && this.tierStep() === 'model'),
  );
  readonly backLabel = computed(() => {
    if (this.viewMode() === 'tier') {
      return this.activeTierLabel();
    }
    const provider = this.activeProvider();
    return provider ? formatProviderLabel(provider) : 'Vendors';
  });
  private readonly favoriteIdSet = computed(() => this.favoritesService.favoriteIds());
  /**
   * Surfaced inside the panel rather than as a toast, matching how the thread list reports
   * a failed optimistic write. The panel is a dropdown, so a failure that lands after it
   * closes is shown the next time it opens — the state persists on the service.
   */
  readonly favoritesError = computed(() => this.favoritesService.error());
  readonly favoriteModels = computed(() => {
    const ids = this.favoriteIdSet();
    return this.models().filter(model => ids.has(model.id)).sort(compareModelsByName);
  });

  isFavorite(model: Model): boolean {
    return this.favoriteIdSet().has(model.id);
  }

  toggleFavorite(model: Model, event: Event): void {
    event.stopPropagation();
    this.favoritesService.toggle(model.id);
  }

  setViewMode(mode: ViewMode): void {
    this.viewMode.set(mode);
    if (mode === 'vendor') {
      this.resetVendorStep();
    } else if (mode === 'tier') {
      this.resetTierStep();
    }
  }

  tierLabel(model: Model): string {
    return modelTierCompactLabel(model.subscription_tier);
  }

  capabilityAriaLabel(model: Model): string {
    const capabilities = model.capabilities;
    if (!capabilities) return '';
    const parts = [
      capabilities.tool_calling ? `${capabilities.tools.length} tools` : 'no tools',
      capabilities.vision ? 'vision' : '',
      capabilities.mcp ? 'MCP' : '',
    ].filter(Boolean);
    return `Capabilities: ${parts.join(', ')}`;
  }

  toolCapabilityTitle(model: Model): string {
    const tools = model.capabilities?.tools ?? [];
    return tools.length ? `Available tools: ${tools.join(', ')}` : 'Tool calling supported';
  }

  providerLabel(provider: string): string {
    return formatProviderLabel(provider);
  }

  providerModelCount(provider: string): number {
    return this.sortedModels().filter(model => modelProvider(model) === provider).length;
  }

  toggle(): void {
    if (this.disabled()) return;
    if (this.open()) {
      this.close();
      return;
    }
    this.resetPickerStep();
    this.updateMenuPlacement();
    this.open.set(true);
  }

  chooseProvider(provider: string): void {
    this.activeProvider.set(provider);
    this.vendorStep.set('model');
  }

  backToProviders(): void {
    this.vendorStep.set('vendor');
    this.activeProvider.set(null);
  }

  chooseTier(group: ModelTierGroup): void {
    this.activeTier.set(tierGroupKey(group));
    this.tierStep.set('model');
  }

  backToTiers(): void {
    this.tierStep.set('tier');
    this.activeTier.set(null);
  }

  onBack(): void {
    if (this.viewMode() === 'tier') {
      this.backToTiers();
      return;
    }
    this.backToProviders();
  }

  choose(model: Model): void {
    this.selected.emit(model);
    this.close();
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: Event): void {
    const target = event.target;
    if (!(target instanceof Node)) return;
    if (!this.host.nativeElement.contains(target)) {
      this.close();
    }
  }

  private close(): void {
    this.open.set(false);
    this.vendorStep.set('vendor');
    this.activeProvider.set(null);
    this.tierStep.set('tier');
    this.activeTier.set(null);
  }

  /** Pick the default view on open: Favorites when the personality has any, else Vendor. */
  private resetPickerStep(): void {
    this.resetVendorStep();
    this.resetTierStep();
    this.viewMode.set(this.favoriteModels().length > 0 ? 'favorites' : 'vendor');
  }

  /** Drill straight to the selected model's provider when one is chosen. */
  private resetVendorStep(): void {
    const current = this.selectedModel();
    if (current) {
      this.activeProvider.set(modelProvider(current));
      this.vendorStep.set('model');
      return;
    }
    this.activeProvider.set(null);
    this.vendorStep.set('vendor');
  }

  /** Drill straight to the selected model's tier when one is chosen. */
  private resetTierStep(): void {
    const current = this.selectedModel();
    if (current) {
      const rank = modelTierRank(current.subscription_tier);
      this.activeTier.set(rank === null ? 'other' : rank);
      this.tierStep.set('model');
      return;
    }
    this.activeTier.set(null);
    this.tierStep.set('tier');
  }

  private updateMenuPlacement(): void {
    if (typeof window === 'undefined') {
      this.menuPlacement.set('above');
      return;
    }
    const rect = this.host.nativeElement.getBoundingClientRect();
    const spaceAbove = rect.top;
    const spaceBelow = window.innerHeight - rect.bottom;
    const estimatedMenuHeight = Math.min(window.innerHeight * 0.6, 288);
    this.menuPlacement.set(
      spaceAbove >= estimatedMenuHeight || spaceAbove >= spaceBelow ? 'above' : 'below',
    );
  }
}

function tierGroupKey(group: ModelTierGroup): TierKey {
  return group.rank === null ? 'other' : group.rank;
}
