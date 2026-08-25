import { AsyncPipe, NgComponentOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, OnDestroy, OnInit, computed, inject, output, signal } from '@angular/core';
import { Router, RouterLink, RouterLinkActive } from '@angular/router';
import { Subscription } from 'rxjs';

import { ConfirmationService } from '../../core/services/confirmation.service';
import { CommandPaletteService } from '../../core/services/command-palette.service';
import { NavService } from '../../core/services/nav.service';
import { Personality } from '../../core/models/personality.model';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ThreadListService } from '../../core/services/thread-list.service';
import { pickSidebarRecentThreads } from '../../features/chat/helpers/thread-list.helpers';
import { GalleryViewService } from '../../core/services/gallery-view.service';
import { MemoryViewService } from '../../core/services/memory-view.service';
import { ModeViewService } from '../../core/services/mode-view.service';
import { RitualViewService } from '../../core/services/ritual-view.service';
import {
  ArrowLeftIconComponent,
  ArrowRightIconComponent,
  GearIconComponent,
  PlusIconComponent,
  SearchIconComponent,
  SparkleIconComponent,
  UserIconComponent,
} from '../../shared/ui/icons/icons';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';
import { AuthImagePipe } from '../../core/pipes/auth-image.pipe';
import { personalityCoverUrl } from '../../features/personality/helpers/cover-image.helpers';
import { thumbnailCircleToCirclePreviewTransform } from '../../shared/ui/avatar/avatar-thumbnail.helpers';
import { personalityAccent, personalityAccentSurface } from '../../features/personality/helpers/personality-vm.helpers';
import { AvatarComponent } from '../../shared/ui/avatar/avatar.component';
import { PersonaAccentScopeComponent } from '../../features/personality/picker/persona-accent-scope.component';
import { AppSidebarHeaderComponent } from './app-sidebar-header.component';
import { appNavItems, configNavItems, NavItem } from './nav.helpers';

const PORTRAIT_SOURCE_WIDTH = 200;
const PORTRAIT_SOURCE_HEIGHT = 267;
const SIDEBAR_THREAD_AVATAR_SIZE = 30;

/**
 * Concept-D dual-sidebar shell. Composes the header, the active nav list, the
 * mode-switch gear, the collapse toggle, and the palette launcher row. Mode
 * is route-derived (see {@link NavService}); this component is presentation
 * only.
 */
@Component({
  selector: 'app-sidebar',
  standalone: true,
  imports: [
    RouterLink,
    RouterLinkActive,
    AsyncPipe,
    AuthImagePipe,
    AvatarComponent,
    PersonaAccentScopeComponent,
    NgComponentOutlet,
    AppSidebarHeaderComponent,
    ArrowLeftIconComponent,
    ArrowRightIconComponent,
    GearIconComponent,
    PlusIconComponent,
    SearchIconComponent,
    SparkleIconComponent,
    UserIconComponent,
    TooltipDirective,
  ],
  templateUrl: './app-sidebar.component.html',
  styleUrl: './app-sidebar.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AppSidebarComponent implements OnInit, OnDestroy {
  readonly nav = inject(NavService);
  readonly palette = inject(CommandPaletteService);
  readonly threads = inject(ThreadListService);
  readonly galleryView = inject(GalleryViewService);
  readonly memoryView = inject(MemoryViewService);
  readonly modeView = inject(ModeViewService);
  readonly ritualView = inject(RitualViewService);
  private readonly imageGallery = inject(ImageGalleryService);
  private readonly personalityService = inject(PersonalityService);
  private readonly router = inject(Router);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly subscriptions = new Subscription();
  /** Thread to return to when the Chat button closes the Thread Manager (set when it opens it). */
  private threadManagerReturnId: string | null = null;

  readonly mode = this.nav.mode;
  readonly collapsed = this.nav.collapsed;
  readonly personalityQuery = signal('');
  readonly personalityDropdownOpen = signal(false);
  readonly selectedPersonalityIds = signal<string[]>([]);
  readonly galleryPersonalityQuery = signal('');
  readonly galleryPersonalityDropdownOpen = signal(false);
  readonly personalityCatalog = signal<readonly Personality[]>([]);
  readonly newThread = output<void>();
  readonly generatePersonality = output<void>();

  readonly items = computed<ReadonlyArray<NavItem>>(() => {
    return this.mode() === 'config'
      ? configNavItems()
      : appNavItems();
  });

  readonly sidebarFilteredThreads = computed(() => {
    const selected = new Set(this.selectedPersonalityIds());
    const threads = this.threads.filteredThreads();
    if (selected.size === 0) return threads;
    return threads.filter(thread => !!thread.personality_id && selected.has(thread.personality_id));
  });

  readonly sidebarPinnedThreads = computed(() =>
    this.sidebarFilteredThreads().filter(thread => !!thread.is_favorite && !!thread.id),
  );
  readonly sidebarRecentThreads = computed(() =>
    pickSidebarRecentThreads(this.sidebarFilteredThreads(), this.threads.recentOpenedIds()),
  );
  readonly sidebarThreadAvatarMeta = computed(() => {
    const byId = new Map(this.personalityCatalog().map(personality => [personality.id, personality]));
    const threadIds = new Set([
      ...this.sidebarPinnedThreads().map(thread => thread.id),
      ...this.sidebarRecentThreads().map(thread => thread.id),
    ]);
    return new Map(
      this.sidebarFilteredThreads()
        .filter(thread => threadIds.has(thread.id))
        .map(thread => {
        const personality = thread.personality_id ? byId.get(thread.personality_id) ?? null : null;
        return [
          thread.id,
          {
            coverUrl: personalityCoverUrl(
              personality,
              [],
              this.imageGallery.getImageUrl.bind(this.imageGallery),
            ),
            accentColor: personality
              ? personalityAccent(personality)
              : personalityAccent({
                id: thread.personality_id ?? '',
                name: thread.personality_name ?? thread.name,
                accent_color: null,
              }),
            accentSurface: personalityAccentSurface(
              personality
                ? personalityAccent(personality)
                : personalityAccent({
                  id: thread.personality_id ?? '',
                  name: thread.personality_name ?? thread.name,
                  accent_color: null,
                }),
            ),
            transform: thumbnailCircleToCirclePreviewTransform(
              personality?.thumbnail_circle,
              PORTRAIT_SOURCE_WIDTH,
              PORTRAIT_SOURCE_HEIGHT,
              SIDEBAR_THREAD_AVATAR_SIZE,
            ),
          },
        ] as const;
      }),
    );
  });
  readonly personalitiesById = computed(() =>
    new Map(this.personalityCatalog().map(personality => [personality.id, personality] as const)),
  );

  readonly filteredPersonalityOptions = computed(() => {
    const selected = new Set(this.selectedPersonalityIds());
    const query = this.personalityQuery().trim().toLowerCase();
    const options = this.personalityCatalog()
      .map(personality => ({ id: personality.id, label: personality.name }))
      .filter(option => !selected.has(option.id));
    if (!query) return options;
    return options.filter(option => option.label.toLowerCase().includes(query));
  });

  readonly selectedPersonalityCards = computed(() => {
    const byId = new Map(this.personalityCatalog().map(persona => [persona.id, persona]));
    return this.selectedPersonalityIds()
      .map(id => {
        const personality = byId.get(id);
        const label = personality?.name ?? 'Unknown personality';
        return {
          id,
          label,
          coverUrl: personalityCoverUrl(
            personality,
            [],
            this.imageGallery.getImageUrl.bind(this.imageGallery),
          ),
          initials: toInitials(label),
          placeholderColor: colorFromLabel(label),
        };
      })
      .filter((value): value is {
        id: string;
        label: string;
        coverUrl: string | null;
        initials: string;
        placeholderColor: string;
      } => value !== null);
  });
  readonly galleryFilteredPersonalityOptions = computed(() => {
    const selected = new Set(this.gallerySelectedPersonalityIds());
    const query = this.galleryPersonalityQuery().trim().toLowerCase();
    const options = this.personalityCatalog().filter(option => !selected.has(option.id));
    if (!query) return options;
    return options.filter(option => option.name.toLowerCase().includes(query));
  });
  readonly gallerySelectedPersonalityCards = computed(() => {
    const byId = new Map(this.personalityCatalog().map(persona => [persona.id, persona]));
    return this.gallerySelectedPersonalityIds()
      .map(id => {
        const personality = byId.get(id);
        if (!personality) return null;
        return {
          id,
          label: personality.name,
          coverUrl: personalityCoverUrl(
            personality,
            [],
            this.imageGallery.getImageUrl.bind(this.imageGallery),
          ),
          initials: toInitials(personality.name),
          placeholderColor: colorFromLabel(personality.name),
        };
      })
      .filter((value): value is {
        id: string;
        label: string;
        coverUrl: string | null;
        initials: string;
        placeholderColor: string;
      } => value !== null);
  });
  readonly galleryFilterMode = computed(() => this.galleryView.associationFilterMode());
  readonly gallerySelectedPersonalityIds = computed(() => this.galleryView.selectedPersonalityIds());
  readonly galleryGlobalDisabled = computed(() => this.galleryView.mode() === 'expressions');
  readonly modePersonalityQuery = signal('');
  readonly modePersonalityDropdownOpen = signal(false);
  readonly modeFilterMode = computed(() => this.modeView.associationFilterMode());
  readonly modeSelectedPersonalityIds = computed(() => this.modeView.selectedPersonalityIds());
  readonly modeFilteredPersonalityOptions = computed(() => {
    const selected = new Set(this.modeSelectedPersonalityIds());
    const query = this.modePersonalityQuery().trim().toLowerCase();
    const options = this.personalityCatalog().filter(option => !selected.has(option.id));
    if (!query) return options;
    return options.filter(option => option.name.toLowerCase().includes(query));
  });
  readonly modeSelectedPersonalityCards = computed(() => {
    const byId = new Map(this.personalityCatalog().map(persona => [persona.id, persona]));
    return this.modeSelectedPersonalityIds()
      .map(id => {
        const personality = byId.get(id);
        if (!personality) return null;
        return {
          id,
          label: personality.name,
          coverUrl: personalityCoverUrl(
            personality,
            [],
            this.imageGallery.getImageUrl.bind(this.imageGallery),
          ),
          initials: toInitials(personality.name),
          placeholderColor: colorFromLabel(personality.name),
        };
      })
      .filter((value): value is {
        id: string;
        label: string;
        coverUrl: string | null;
        initials: string;
        placeholderColor: string;
      } => value !== null);
  });
  readonly memoryPersonalityQuery = signal('');
  readonly memoryPersonalityDropdownOpen = signal(false);
  readonly skillPersonalityQuery = signal('');
  readonly skillPersonalityDropdownOpen = signal(false);
  readonly memoryFilterMode = computed(() => this.memoryView.associationFilterMode());
  readonly memorySelectedPersonalityIds = computed(() => this.memoryView.selectedPersonalityIds());
  readonly memoryAssociationInactive = computed(() => {
    const level = this.memoryView.filters().level;
    return level === 'personality' || level === 'thread' || level === 'summary';
  });
  readonly memoryFilteredPersonalityOptions = computed(() => {
    const selected = new Set(this.memorySelectedPersonalityIds());
    const query = this.memoryPersonalityQuery().trim().toLowerCase();
    const options = this.personalityCatalog().filter(option => !selected.has(option.id));
    if (!query) return options;
    return options.filter(option => option.name.toLowerCase().includes(query));
  });
  readonly memorySelectedPersonalityCards = computed(() => {
    const byId = new Map(this.personalityCatalog().map(persona => [persona.id, persona]));
    return this.memorySelectedPersonalityIds()
      .map(id => {
        const personality = byId.get(id);
        if (!personality) return null;
        return {
          id,
          label: personality.name,
          coverUrl: personalityCoverUrl(
            personality,
            [],
            this.imageGallery.getImageUrl.bind(this.imageGallery),
          ),
          initials: toInitials(personality.name),
          placeholderColor: colorFromLabel(personality.name),
        };
      })
      .filter((value): value is {
        id: string;
        label: string;
        coverUrl: string | null;
        initials: string;
        placeholderColor: string;
      } => value !== null);
  });
  readonly skillFilterMode = computed<'all' | 'global' | 'personality'>(() => {
    const filters = this.ritualView.filters();
    if (filters.globalOnly) return 'global';
    const selectedIds = filters.personalityIds.length
      ? filters.personalityIds
      : (filters.personalityId.trim() ? [filters.personalityId.trim()] : []);
    return selectedIds.length > 0 ? 'personality' : 'all';
  });
  readonly skillSelectedPersonalityIds = computed(() => {
    const filters = this.ritualView.filters();
    if (filters.globalOnly) return [];
    return filters.personalityIds.length
      ? filters.personalityIds
      : (filters.personalityId.trim() ? [filters.personalityId.trim()] : []);
  });
  readonly skillFilteredPersonalityOptions = computed(() => {
    const selected = new Set(this.skillSelectedPersonalityIds());
    const query = this.skillPersonalityQuery().trim().toLowerCase();
    const options = this.personalityCatalog().filter(option => !selected.has(option.id));
    if (!query) return options;
    return options.filter(option => option.name.toLowerCase().includes(query));
  });
  readonly skillSelectedPersonalityCards = computed(() => {
    const byId = new Map(this.personalityCatalog().map(persona => [persona.id, persona]));
    return this.skillSelectedPersonalityIds()
      .map(id => {
        const personality = byId.get(id);
        if (!personality) return null;
        return {
          id,
          label: personality.name,
          coverUrl: personalityCoverUrl(
            personality,
            [],
            this.imageGallery.getImageUrl.bind(this.imageGallery),
          ),
          initials: toInitials(personality.name),
          placeholderColor: colorFromLabel(personality.name),
        };
      })
      .filter((value): value is {
        id: string;
        label: string;
        coverUrl: string | null;
        initials: string;
        placeholderColor: string;
      } => value !== null);
  });

  ngOnInit(): void {
    this.syncThreadListPersonalityFilter();
    void this.threads.refresh();
    this.subscriptions.add(
      this.personalityService.listPersonalities(1, 100).subscribe({
        next: response => this.personalityCatalog.set(response.results ?? []),
        error: () => this.personalityCatalog.set([]),
      }),
    );
  }

  ngOnDestroy(): void {
    this.subscriptions.unsubscribe();
  }

  toggleMode(): void {
    this.nav.setMode(this.mode() === 'config' ? 'app' : 'config');
  }

  toggleCollapsed(): void {
    this.nav.toggleCollapsed();
  }

  openPalette(): void {
    this.palette.open();
  }

  onItemSelected(_item: NavItem): void {
    // Placeholder for mobile-drawer auto-close behaviour wired by the parent
    // layout. The list emits even when collapsed for consistency.
  }

  isChatView(): boolean {
    return this.router.url.startsWith('/chat');
  }

  /** True when the chat section is showing the Thread Manager (no specific thread open). */
  isThreadManagerView(): boolean {
    return this.isChatView() && !this.threads.activeThreadId();
  }

  /**
   * Chat nav button: toggles the Thread Manager instead of re-navigating to (and reloading) the
   * currently open thread. From a thread — or any other view — it opens the manager; from the manager
   * it returns to the thread you came from, if there is one.
   */
  toggleThreadManager(): void {
    if (this.isThreadManagerView()) {
      const target = this.threadManagerReturnId;
      this.threadManagerReturnId = null;
      if (target) {
        this.threads.setActiveThreadId(target);
        void this.router.navigate(['/chat', target]);
      }
      return;
    }
    this.threadManagerReturnId = this.threads.activeThreadId();
    this.navigateToThreadManager();
  }

  isPersonalityView(): boolean {
    return this.router.url.startsWith('/personality');
  }

  isGalleryView(): boolean {
    return this.router.url.startsWith('/gallery') || this.router.url.startsWith('/image-gallery');
  }

  isMemoryView(): boolean {
    return this.router.url.startsWith('/memories') || this.router.url.startsWith('/memory');
  }

  isSkillsView(): boolean {
    return this.router.url.startsWith('/skills') || this.router.url.startsWith('/rituals') || this.router.url.startsWith('/ritual');
  }

  isModeView(): boolean {
    return this.router.url.startsWith('/mode');
  }

  openThread(threadId: string): void {
    const activeId = this.threads.activeThreadId();
    if (activeId === threadId) {
      this.threads.setActiveThreadId(null);
      void this.router.navigate(['/chat']);
      return;
    }
    this.threads.setActiveThreadId(threadId);
    void this.router.navigate(['/chat', threadId]);
  }

  threadAvatarMeta(threadId: string): { coverUrl: string | null; transform: string | null; accentColor: string | null; accentSurface: string | null } {
    return this.sidebarThreadAvatarMeta().get(threadId) ?? { coverUrl: null, transform: null, accentColor: null, accentSurface: null };
  }

  threadAvatarBackground(threadId: string): string {
    return this.threadAvatarMeta(threadId).accentSurface ?? 'var(--bg-secondary, var(--bg-base))';
  }

  threadAvatarForeground(threadId: string): string {
    return this.threadAvatarMeta(threadId).accentColor || 'var(--text-muted)';
  }

  async unpinAll(): Promise<void> {
    const confirmed = await this.confirmationService.confirm({
      title: 'Unstar all threads',
      message: 'Are you sure you want to unstar all threads?',
      confirmText: 'Unstar all',
      cancelText: 'Cancel',
      type: 'warning',
    });
    if (!confirmed) {
      return;
    }

    await this.threads.unpinAll();
  }

  startNewThread(): void {
    this.newThread.emit();
  }

  openGeneratePersonality(): void {
    this.generatePersonality.emit();
  }

  openCreatePersonalityManually(): void {
    void this.router.navigate(['/personality'], { queryParams: { create: '1' } });
  }

  onPersonalityQueryChange(value: string): void {
    this.personalityQuery.set(value);
    this.personalityDropdownOpen.set(true);
  }

  openPersonalityDropdown(): void {
    this.personalityDropdownOpen.set(true);
  }

  closePersonalityDropdown(): void {
    setTimeout(() => this.personalityDropdownOpen.set(false), 100);
  }

  togglePersonalityFilter(personalityId: string, event?: Event): void {
    event?.preventDefault();
    this.selectedPersonalityIds.update(current => {
      if (current.includes(personalityId)) {
        return current.filter(id => id !== personalityId);
      }
      return [...current, personalityId];
    });
    this.personalityQuery.set('');
    this.personalityDropdownOpen.set(true);
    this.syncThreadListPersonalityFilter();
  }

  removePersonalityFilter(personalityId: string): void {
    this.selectedPersonalityIds.update(current => current.filter(id => id !== personalityId));
    this.syncThreadListPersonalityFilter();
  }

  clearPersonalityFilters(): void {
    this.selectedPersonalityIds.set([]);
    this.syncThreadListPersonalityFilter();
  }

  navigateToThreadManager(): void {
    localStorage.removeItem('lastChatId');
    void this.router.navigate(['/chat']);
  }

  /** Navigate to the Thread Manager and open the import modal (handled by chat-page via ?import). */
  navigateToImport(): void {
    localStorage.removeItem('lastChatId');
    void this.router.navigate(['/chat'], { queryParams: { import: Date.now() } });
  }

  private syncThreadListPersonalityFilter(): void {
    this.threads.setSidebarPersonalityFilter(this.selectedPersonalityIds());
  }

  clearGalleryPersonalityFilters(): void {
    this.galleryView.selectAllAssociations();
  }

  clearModePersonalityFilters(): void {
    this.modeView.selectAllAssociations();
  }

  onModePersonalityQueryChange(value: string): void {
    this.modePersonalityQuery.set(value);
    this.modePersonalityDropdownOpen.set(true);
  }

  openModePersonalityDropdown(): void {
    this.modePersonalityDropdownOpen.set(true);
  }

  closeModePersonalityDropdown(): void {
    setTimeout(() => this.modePersonalityDropdownOpen.set(false), 100);
  }

  toggleModePersonalityFilter(personalityId: string, event?: Event): void {
    event?.preventDefault();
    const current = this.modeSelectedPersonalityIds();
    const next = current.includes(personalityId)
      ? current.filter(id => id !== personalityId)
      : [...current, personalityId];
    this.modeView.setSelectedPersonalityIds(next);
    this.modePersonalityQuery.set('');
    this.modePersonalityDropdownOpen.set(true);
  }

  removeModePersonalityFilter(personalityId: string): void {
    const next = this.modeSelectedPersonalityIds().filter(id => id !== personalityId);
    this.modeView.setSelectedPersonalityIds(next);
  }

  openCreateMode(): void {
    this.modeView.requestCreateModalOpen();
  }

  onGalleryPersonalityQueryChange(value: string): void {
    this.galleryPersonalityQuery.set(value);
    this.galleryPersonalityDropdownOpen.set(true);
  }

  openGalleryPersonalityDropdown(): void {
    this.galleryPersonalityDropdownOpen.set(true);
  }

  closeGalleryPersonalityDropdown(): void {
    setTimeout(() => this.galleryPersonalityDropdownOpen.set(false), 100);
  }

  toggleGalleryPersonalityFilter(personalityId: string, event?: Event): void {
    event?.preventDefault();
    const current = this.gallerySelectedPersonalityIds();
    const next = current.includes(personalityId)
      ? current.filter(id => id !== personalityId)
      : [...current, personalityId];
    this.galleryView.setSelectedPersonalityIds(next);
    this.galleryPersonalityQuery.set('');
    this.galleryPersonalityDropdownOpen.set(true);
  }

  selectGlobalGalleryFilter(): void {
    if (this.galleryGlobalDisabled()) {
      return;
    }
    this.galleryView.selectGlobalAssociations();
  }

  removeGalleryPersonalityFilter(personalityId: string): void {
    const next = this.gallerySelectedPersonalityIds().filter(id => id !== personalityId);
    this.galleryView.setSelectedPersonalityIds(next);
  }

  openGalleryImportModal(): void {
    this.galleryView.requestImportModalOpen();
  }

  clearMemoryPersonalityFilters(): void {
    this.memoryView.setFilters({ level: 'all' });
    this.memoryView.selectAllAssociations();
  }

  onMemoryPersonalityQueryChange(value: string): void {
    this.memoryPersonalityQuery.set(value);
    this.memoryPersonalityDropdownOpen.set(true);
  }

  openMemoryPersonalityDropdown(): void {
    this.memoryPersonalityDropdownOpen.set(true);
  }

  closeMemoryPersonalityDropdown(): void {
    setTimeout(() => this.memoryPersonalityDropdownOpen.set(false), 100);
  }

  toggleMemoryPersonalityFilter(personalityId: string, event?: Event): void {
    event?.preventDefault();
    this.memoryView.setFilters({ level: 'all' });
    const current = this.memorySelectedPersonalityIds();
    const next = current.includes(personalityId)
      ? current.filter(id => id !== personalityId)
      : [...current, personalityId];
    this.memoryView.setSelectedPersonalityIds(next);
    this.memoryPersonalityQuery.set('');
    this.memoryPersonalityDropdownOpen.set(true);
  }

  selectGlobalMemoryFilter(): void {
    this.memoryView.setFilters({ level: 'all' });
    this.memoryView.selectGlobalAssociations();
  }

  removeMemoryPersonalityFilter(personalityId: string): void {
    const next = this.memorySelectedPersonalityIds().filter(id => id !== personalityId);
    this.memoryView.setSelectedPersonalityIds(next);
  }

  openAddMemory(): void {
    void this.router.navigate(['/memories'], { queryParams: { create: '1' }, queryParamsHandling: 'merge' });
  }

  clearSkillPersonalityFilters(): void {
    this.ritualView.setFilters({ globalOnly: false, personalityIds: [], personalityId: '' });
  }

  onSkillPersonalityQueryChange(value: string): void {
    this.skillPersonalityQuery.set(value);
    this.skillPersonalityDropdownOpen.set(true);
  }

  openSkillPersonalityDropdown(): void {
    this.skillPersonalityDropdownOpen.set(true);
  }

  closeSkillPersonalityDropdown(): void {
    setTimeout(() => this.skillPersonalityDropdownOpen.set(false), 100);
  }

  toggleSkillPersonalityFilter(personalityId: string, event?: Event): void {
    event?.preventDefault();
    const current = this.skillSelectedPersonalityIds();
    const next = current.includes(personalityId)
      ? current.filter(id => id !== personalityId)
      : [...current, personalityId];
    this.ritualView.setFilters({ globalOnly: false, personalityIds: next, personalityId: '' });
    this.skillPersonalityQuery.set('');
    this.skillPersonalityDropdownOpen.set(true);
  }

  selectGlobalSkillFilter(): void {
    this.ritualView.setFilters({ globalOnly: true, personalityIds: [], personalityId: '' });
  }

  removeSkillPersonalityFilter(personalityId: string): void {
    const next = this.skillSelectedPersonalityIds().filter(id => id !== personalityId);
    this.ritualView.setFilters({ globalOnly: false, personalityIds: next, personalityId: '' });
  }

  openCreateSkill(): void {
    void this.router.navigate(['/skills'], { queryParams: { create: '1' }, queryParamsHandling: 'merge' });
  }

  personalityForFilter(personalityId: string): Personality | null {
    return this.personalitiesById().get(personalityId) ?? null;
  }
}

function toInitials(label: string): string {
  const parts = label
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2);
  return parts.map(part => part[0]?.toUpperCase() ?? '').join('') || '?';
}

function colorFromLabel(label: string): string {
  const colors = ['#1f9d8e', '#8b5cf6', '#f97316', '#3b82f6', '#e11d48', '#0ea5e9', '#16a34a'];
  let hash = 0;
  for (let i = 0; i < label.length; i += 1) {
    hash = (hash * 31 + label.charCodeAt(i)) >>> 0;
  }
  return colors[hash % colors.length];
}
