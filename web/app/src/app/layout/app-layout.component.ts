import {
  ChangeDetectionStrategy,
  Component,
  HostListener,
  OnDestroy,
  OnInit,
  inject,
  signal,
} from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { Subscription } from 'rxjs';
import { firstValueFrom } from 'rxjs';
import { NavigationEnd, Router, RouterOutlet } from '@angular/router';
import { map } from 'rxjs/operators';

import { ChatService } from '../core/services/chat.service';
import { CommandPaletteService } from '../core/services/command-palette.service';
import { KeyboardShortcutService, ShortcutHandle } from '../core/services/keyboard-shortcut.service';
import { NavService } from '../core/services/nav.service';
import { RightPanelService } from '../core/services/right-panel.service';
import { SearchApiService } from '../core/services/search-api.service';
import { ThemeService } from '../core/services/theme.service';
import { GeneratePersonalityModalService } from '../core/services/generate-personality-modal.service';
import { ProfileSettingsModalService } from '../features/profile-settings/profile-settings-modal.service';
import { Personality } from '../core/models/personality.model';
import { Chat } from '../core/models/chat.model';
import { CommandPaletteComponent } from './command-palette/command-palette.component';
import { ContextPanelService } from '../features/chat/services/context-panel.service';
import { PersonaPickerDialogComponent } from '../features/personality/picker/persona-picker-dialog.component';
import { PersonalityGenerateComponent } from '../features/personality/personality-generate/personality-generate.component';
import { ProfileSettingsModalHostComponent } from '../features/profile-settings/profile-settings-modal-host.component';
import { HelpExtensionOutletComponent } from '../extensions/help-extension-outlet.component';
import { RightPanelHostComponent } from './right-panel/right-panel-host.component';
import { AppSidebarComponent } from './sidebar/app-sidebar.component';
import { ContextPanelComponent } from '../features/chat/components/context-panel/context-panel.component';
import { ModalComponent, ModalDismissReason } from '../shared/ui/modal/modal.component';
import { ConfirmationService } from '../core/services/confirmation.service';
import { SheetComponent } from '../shared/ui/sheet/sheet.component';
import {
  MenuIconComponent,
  ChatIconComponent,
  GearIconComponent,
  MoonIconComponent,
  SunIconComponent,
  UserIconComponent,
  XIconComponent,
} from '../shared/ui/icons/icons';

/**
 * Top-level shell. Owns the route outlet, the left sidebar, the right context
 * panel slot, and the command palette overlay. Wires the global ⌘K shortcut
 * and registers the built-in palette handlers (search-api + static commands).
 */
@Component({
  selector: 'app-layout',
  standalone: true,
  imports: [
    AppSidebarComponent,
    CommandPaletteComponent,
    ContextPanelComponent,
    HelpExtensionOutletComponent,
    ModalComponent,
    PersonaPickerDialogComponent,
    PersonalityGenerateComponent,
    ProfileSettingsModalHostComponent,
    RightPanelHostComponent,
    RouterOutlet,
    SheetComponent,
    MenuIconComponent,
    XIconComponent,
  ],
  templateUrl: './app-layout.component.html',
  styleUrl: './app-layout.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AppLayoutComponent implements OnInit, OnDestroy {
  readonly nav = inject(NavService);
  private readonly palette = inject(CommandPaletteService);
  private readonly shortcuts = inject(KeyboardShortcutService);
  private readonly searchApi = inject(SearchApiService);
  private readonly router = inject(Router);
  private readonly theme = inject(ThemeService);
  private readonly chatService = inject(ChatService);
  readonly generatePersonalityModal = inject(GeneratePersonalityModalService);
  readonly profileSettingsModal = inject(ProfileSettingsModalService);
  private readonly confirmation = inject(ConfirmationService);

  readonly personaPickerOpen = signal(false);
  readonly showRightPanel = signal(false);
  readonly showMobileSheet = signal(false);
  readonly isMobileNav = signal(false);
  readonly rightPanelService = inject(RightPanelService);
  readonly rightPanelContext = inject(ContextPanelService);
  private readonly document = inject(DOCUMENT);
  private mobileNavInitialized = false;

  private shortcutHandle: ShortcutHandle | null = null;
  private disposers: Array<() => void> = [];
  private recentThreadCommandDisposers: Array<() => void> = [];
  private readonly subscriptions = new Subscription();
  private static readonly CHAT_THREAD_PATH_PATTERN = /^\/chat\/[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

  ngOnInit(): void {
    this.shortcutHandle = this.shortcuts.register(
      { key: 'k', metaOrCtrl: true, allowInInputs: true },
      event => {
        event.preventDefault();
        this.palette.toggle();
      },
    );

    this.disposers.push(
      this.palette.register(query =>
        this.searchApi.search(query, { limitPerType: 5 }).pipe(map(resp => [...resp.sections])),
      ),
    );

    this.subscriptions.add(
      this.chatService.chats$.subscribe(chats => this.syncRecentThreadCommands(chats)),
    );
    this.setRightPanelVisibility(this.router.url);
    this.subscriptions.add(
      this.router.events.subscribe(event => {
        if (event instanceof NavigationEnd) {
          this.setRightPanelVisibility(event.urlAfterRedirects);
        }
      }),
    );

    this.disposers.push(
      this.palette.registerCommand({
        id: 'new-chat',
        label: 'New chat',
        description: 'Start a new conversation',
        icon: ChatIconComponent,
        keywords: ['thread', 'message'],
        run: () => this.openPersonaPicker(),
      }),
      this.palette.registerCommand({
        id: 'new-chat-with-personality',
        label: 'New chat with personality…',
        description: 'Pick a persona, then start a fresh thread',
        icon: ChatIconComponent,
        keywords: ['persona', 'character', 'thread'],
        run: () => this.openPersonaPicker(),
      }),
      this.palette.registerCommand({
        id: 'open-profile',
        label: 'Open profile',
        description: 'Account, preferences and integrations',
        icon: UserIconComponent,
        keywords: ['settings', 'account'],
        run: () => this.profileSettingsModal.open('profile'),
      }),
      this.palette.registerCommand({
        id: 'switch-config',
        label: 'Switch to configuration mode',
        icon: GearIconComponent,
        keywords: ['memories', 'modes', 'moods', 'skills', 'jobs'],
        run: () => this.nav.setMode('config'),
      }),
      this.palette.registerCommand({
        id: 'switch-app',
        label: 'Switch to app mode',
        icon: ChatIconComponent,
        keywords: ['chat', 'personalities'],
        run: () => this.nav.setMode('app'),
      }),
      this.palette.registerCommand({
        id: 'toggle-theme',
        label: 'Toggle theme',
        description: 'Switch between light and dark',
        icon: this.theme.theme() === 'dark' ? SunIconComponent : MoonIconComponent,
        keywords: ['dark', 'light', 'appearance'],
        run: () => this.theme.setTheme(this.theme.theme() === 'dark' ? 'light' : 'dark'),
      }),
    );
  }

  ngOnDestroy(): void {
    this.subscriptions.unsubscribe();
    this.shortcutHandle?.();
    this.shortcutHandle = null;
    for (const dispose of this.disposers.splice(0)) {
      dispose();
    }
    this.clearRecentThreadCommands();
  }

  openPersonaPicker(): void {
    this.personaPickerOpen.set(true);
  }

  openGeneratePersonality(): void {
    this.generatePersonalityModal.show();
  }

  closeGeneratePersonality(): void {
    this.generatePersonalityModal.hide();
  }

  /**
   * Backdrop/Escape dismissal of the generate modal: confirm before discarding
   * unsaved current-step answers. The header ✕ dismisses immediately.
   */
  async onGenerateModalDismiss(reason: ModalDismissReason, hasUnsavedAnswers: boolean): Promise<void> {
    if (reason !== 'close-button' && hasUnsavedAnswers && !(await this.confirmation.confirmDiscardChanges())) {
      return;
    }
    this.closeGeneratePersonality();
  }

  closePersonaPicker(): void {
    this.personaPickerOpen.set(false);
  }

  async onPersonaPicked(personality: Personality): Promise<void> {
    this.personaPickerOpen.set(false);
    // On mobile the sidebar is an overlay drawer. Collapse it so the freshly
    // created thread isn't left hidden behind an open drawer (issue #36).
    if (this.isMobileNav()) {
      this.nav.setCollapsed(true);
    }
    try {
      const chat = await firstValueFrom(
        this.chatService.createChat({
          name: 'New Chat',
          personality_id: personality.id,
        }),
      );
      this.chatService.setLastChatId(chat.id);
      await this.router.navigate(['/chat', chat.id]);
    } catch (error) {
      console.error('Failed to start chat with personality', error);
    }
  }

  onPersonaPickerCreate(): void {
    this.personaPickerOpen.set(false);
  }

  private syncRecentThreadCommands(chats: readonly Chat[]): void {
    this.clearRecentThreadCommands();
    for (const chat of chats.slice(0, 8)) {
      this.recentThreadCommandDisposers.push(
        this.palette.registerCommand({
          id: `recent-thread-${chat.id}`,
          label: `Open thread: ${chat.name}`,
          description: chat.last_message_time ? `Last message ${new Date(chat.last_message_time).toLocaleString()}` : 'Open chat thread',
          icon: ChatIconComponent,
          keywords: ['thread', 'chat', ...(chat.tags ?? [])],
          run: () => this.router.navigate(['/chat', chat.id]),
        }),
      );
    }
  }

  private clearRecentThreadCommands(): void {
    for (const dispose of this.recentThreadCommandDisposers.splice(0)) {
      dispose();
    }
  }

  @HostListener('window:resize')
  onWindowResize(): void {
    this.setRightPanelVisibility(this.router.url);
  }

  toggleMobileSidebar(): void {
    this.nav.setCollapsed(!this.nav.collapsed());
  }

  closeMobileSidebar(): void {
    this.nav.setCollapsed(true);
  }

  private setRightPanelVisibility(url: string): void {
    const path = url.split('?')[0].split('#')[0];
    const isDesktop = this.document.defaultView?.matchMedia('(min-width: 1024px)').matches ?? true;
    this.isMobileNav.set(!isDesktop);
    if (!isDesktop && !this.mobileNavInitialized) {
      this.nav.setCollapsed(true);
      this.mobileNavInitialized = true;
    }
    if (isDesktop) {
      this.mobileNavInitialized = false;
    }
    this.showRightPanel.set(isDesktop && AppLayoutComponent.CHAT_THREAD_PATH_PATTERN.test(path));
    this.showMobileSheet.set(AppLayoutComponent.CHAT_THREAD_PATH_PATTERN.test(path));
  }
}
