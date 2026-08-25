import type { Locator, Page } from '@playwright/test';
import { OPTIONAL_UI_PROBE_TIMEOUT } from '../timeouts';

/** A chat conversation (`/chat/<id>`). */
export class ChatPage {
  constructor(private readonly page: Page) {
    this.personalityNameButton = this.page.getByRole('button', {
      name: /^Change personality/,
    });
    this.lastAssistantBubble = this.page.locator('article.bubble[aria-label="Assistant message"]').last();
    this.lastAssistantBody = this.lastAssistantBubble.locator('.bubble__body');
    this.lastAssistantContextAction = this.lastAssistantBubble.getByRole('button', { name: 'Context' });
    this.renameButton = this.page.locator('button.chat-page__thread-name-trigger');
    this.renameInput = this.page.locator('input.chat-page__thread-name-input');
    this.headingText = this.page.locator('.chat-page__thread-name-trigger h1');
    this.exportButton = this.page.getByRole('button', {
      name: 'Export thread',
    });
    this.closeThreadButton = this.page.getByRole('button', {
      name: 'Close thread',
    });
    this.statusMessage = this.page.locator('.chat-page__status');
    this.emptyState = this.page.getByLabel('No conversation selected');
    this.typingIndicator = this.page.getByLabel('Assistant is typing');
    this.stopButton = this.page.getByRole('button', { name: 'Stop response' });
    this.contextPanelToggle = this.page.getByRole('button', {
      name: 'Open conversation context panel',
    });
    this.contextPanel = this.page.getByLabel('Conversation context', {
      exact: true,
    });
    this.contextBreakdown = this.page.getByLabel('Context breakdown', {
      exact: true,
    });
    this.scratchpadTab = this.page.getByLabel('Thread scratchpad');
    this.scratchpadInput = this.page.getByPlaceholder('Capture thread-specific notes');
    this.composerInput = this.page.locator('#chat-composer-input');
    this.plusMenuButton = this.page.getByRole('button', { name: 'Open chat options' });
    this.plusMenu = this.page.getByRole('menu');
    // Accessible name is the dynamic `modeMenuAriaLabel()` (e.g. "Choose mode.",
    // "Mode set to Auto.", "Mode locked to <name>."), so match loosely rather
    // than pin the exact copy.
    this.modeMenuItem = this.plusMenu.getByRole('menuitem', { name: /mode/i });
    // aria-label is `'Choose ' + MODE_SINGULAR` (chat-composer.component.ts),
    // constant regardless of which mode is currently active.
    this.modePickerDialog = this.page.getByRole('dialog', { name: 'Choose Mode' });
    this.modeFilterInput = this.modePickerDialog.getByLabel('Filter modes');
    // Title bound in chat-page.component.html is 'Set chat personality' for the
    // 'attach' picker mode that `personalityNameButton` opens (vs. 'New chat with
    // personality' for the new-thread mode opened elsewhere).
    this.personaPickerDialog = this.page.getByRole('dialog', {
      name: 'Set chat personality',
    });
    this.modelPickerTrigger = this.page.locator('.model-picker__trigger');
    this.modelPickerOptions = this.page.locator('.model-picker__options');
    this.composerPersonaMenuItem = this.plusMenu.getByRole('menuitem').nth(4);
    this.skillMenuItem = this.plusMenu.getByRole('menuitem', { name: 'Skill', exact: true });
    this.skillPickerDialog = this.page.getByRole('dialog', { name: 'Choose skills' });
    this.skillFilterInput = this.skillPickerDialog.getByLabel('Filter skills');
    this.skillOptionList = this.skillPickerDialog.getByRole('listbox', { name: 'Available skills' });
    this.skillPickerStatus = this.skillPickerDialog.locator('.composer__skill-status');
    this.pendingSkills = this.page.getByLabel('Skills to send with this message');
  }

  readonly composerInput: Locator;

  /**
   * Header button showing the active personality's name (and the matching
   * "Message <name>..." composer placeholder). Visual specs mask this: it
   * re-renders with subtly different subpixel/font-hinting each run even
   * with the same fixed personality name, causing 1-line antialiasing
   * flakes in `toHaveScreenshot`.
   */
  readonly personalityNameButton: Locator;

  /** The most recent assistant bubble; empty until a reply streams in. */
  readonly lastAssistantBubble: Locator;

  /** Body of the most recent assistant bubble — what the reply text lands in. */
  readonly lastAssistantBody: Locator;

  /** Opens the token breakdown captured for the most recent assistant reply. */
  readonly lastAssistantContextAction: Locator;

  async sendMessage(text: string): Promise<void> {
    await this.composerInput.fill(text);
    await this.page.getByRole('button', { name: 'Send message' }).click();
  }

  /**
   * A *user* message bubble containing `text`.
   *
   * Scoped to `aria-label="User message"` rather than a bare `getByText`,
   * which matched the assistant's bubble too: the mock backend runs in echo
   * mode, so the reply repeats the sent text verbatim and the page genuinely
   * holds two matching elements. That made the unscoped locator a race — it
   * passed whenever the assertion beat the echo and raised a strict-mode
   * violation whenever it didn't, which is what WebKit did in CI while every
   * local run happened to win the race.
   */
  messageText(text: string) {
    return this.page.locator('article.bubble[aria-label="User message"]').filter({ hasText: text });
  }

  async navigateTo(chatId: string): Promise<void> {
    await this.page.goto(`/chat/${chatId}`);
  }

  // --- title bar -----------------------------------------------------------

  /**
   * Both the title button and the inline input it swaps in are labelled
   * "Rename chat"; `renameButton` is the button form specifically.
   */
  readonly renameButton: Locator;

  readonly renameInput: Locator;

  readonly headingText: Locator;

  /** Click-to-rename, commit with Enter. */
  async rename(newName: string): Promise<void> {
    await this.renameButton.click();
    await this.renameInput.fill(newName);
    await this.renameInput.press('Enter');
  }

  /** Desktop only — `display: none` below 1024px (chat-page.component.scss). */
  readonly exportButton: Locator;

  async exportThread(): Promise<void> {
    await this.exportButton.click();
  }

  readonly closeThreadButton: Locator;

  /** Closes the open thread; the app falls back to the Thread Manager. */
  async closeThread(): Promise<void> {
    await this.closeThreadButton.click();
  }

  /** Transient toast under the title bar (copy/export feedback). */
  readonly statusMessage: Locator;

  // --- states --------------------------------------------------------------

  /** Shown by `/chat/<id>` before a conversation is selected. */
  readonly emptyState: Locator;

  readonly typingIndicator: Locator;

  /** Replaces Send while a reply is streaming. */
  readonly stopButton: Locator;

  async stopResponse(): Promise<void> {
    await this.stopButton.click();
  }

  /**
   * Fires a synthetic `visibilitychange` event without actually backgrounding
   * the tab (Playwright has no reliable cross-browser way to simulate real OS
   * tab-switch/focus loss). `chat-page.component.ts`'s `onReturnToApp` only
   * checks `document.visibilityState !== 'hidden'` before refreshing, so
   * dispatching the event while the page stays genuinely visible still drives
   * the same "returned to app" code path the app uses for real tab returns.
   */
  async simulateReturnToTab(): Promise<void> {
    await this.page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  }

  // --- context panel -------------------------------------------------------
  //
  // Two entry points for the same component. The title-bar toggle
  // ("Open conversation context panel") is styled mobile-only — on a desktop
  // viewport it is in the DOM but not visible, and the panel is opened from
  // the vertical rail beside the conversation instead. `openContextPanel()`
  // picks whichever the current viewport offers.

  readonly contextPanelToggle: Locator;

  readonly contextPanel: Locator;

  /** Token meter and segment legend for the Context X-ray tab. */
  readonly contextBreakdown: Locator;

  async openLastAssistantContext(): Promise<void> {
    await this.lastAssistantContextAction.click();
  }

  /**
   * Ensures the panel is showing. On desktop chat-page opens it on init, so
   * this is usually a no-op — and clicking the rail shortcut for the tab that
   * is already active would *close* it again (`openContextPanel` in
   * chat-page.component.ts toggles).
   */
  async openContextPanel(): Promise<void> {
    // Wait for the conversation shell first: called straight after a
    // navigation, the visibility probe below would otherwise run before the
    // already-open panel has rendered and toggle it *shut*. The title bar is
    // the one part of it that exists at every viewport.
    await this.renameButton.waitFor({ state: 'visible' });
    if (await this.contextPanel.isVisible().catch(() => false)) {
      return;
    }
    if (await this.contextPanelToggle.isVisible().catch(() => false)) {
      await this.contextPanelToggle.click();
      return;
    }
    await this.page.getByRole('button', { name: 'Open scratchpad' }).click();
  }

  /**
   * The panel's own close button is hidden below 1024px, where the panel is
   * hosted in a `ui-sheet` that supplies its own "Close" instead.
   */
  async closeContextPanel(): Promise<void> {
    const panelClose = this.page.getByRole('button', {
      name: 'Close context panel',
    });
    if (await panelClose.isVisible().catch(() => false)) {
      await panelClose.click();
      return;
    }
    await this.page.locator('button.ui-sheet__close').click();
  }

  /**
   * Switches the panel's tab. Two different controls depending on viewport:
   * the panel's own header nav ("Context sections") is `display:none` on
   * desktop, where the vertical rail beside the conversation drives it
   * instead. The rail buttons *toggle*, so re-selecting the active tab would
   * close the panel — that case is skipped rather than clicked.
   *
   * A plain `isVisible()` snapshot right after `openContextPanel()` can catch
   * the mobile `ui-sheet` mid-transition and read as not-yet-visible, which
   * used to fall through to the rail button — one that doesn't exist on
   * mobile at all (`chat-page.component.scss` hides `.chat-page__context-rail`
   * below 1024px), hanging forever. `waitFor` gives it a real, bounded chance
   * to settle before deciding which control this viewport actually offers.
   */
  async selectContextTab(tab: 'Scratchpad' | 'Memories' | 'Tools'): Promise<void> {
    const inPanelTab = this.contextPanel.getByRole('button', {
      name: tab,
      exact: true,
    });
    const inPanelVisible = await inPanelTab
      .waitFor({ state: 'visible', timeout: OPTIONAL_UI_PROBE_TIMEOUT })
      .then(() => true)
      .catch(() => false);
    if (inPanelVisible) {
      await inPanelTab.click();
      return;
    }

    const railButton = this.page.getByRole('button', {
      name: `Open ${tab.toLowerCase()}`,
    });
    if ((await railButton.getAttribute('aria-current')) === 'true') {
      return;
    }
    await railButton.click();
  }

  readonly scratchpadTab: Locator;

  readonly scratchpadInput: Locator;

  async fillScratchpad(text: string): Promise<void> {
    await this.scratchpadInput.fill(text);
  }

  // --- composer "+" menu / mode picker --------------------------------------

  /** The composer's "+" button (`aria-label="Open chat options"`). */
  readonly plusMenuButton: Locator;

  /** The `role="menu"` opened by `plusMenuButton`. */
  readonly plusMenu: Locator;

  /** The menu's "Mode" item — opens the mode picker (`pickMode`/`openModePicker` on the component). */
  readonly modeMenuItem: Locator;

  /** The mode picker popover, `role="dialog"` aria-labelled "Choose Mode". */
  readonly modePickerDialog: Locator;

  /** "Filter modes" search input inside the mode picker. */
  readonly modeFilterInput: Locator;

  async openPlusMenu(): Promise<void> {
    await this.plusMenuButton.click();
  }

  /** Opens the mode picker via the "+" menu's "Mode" item. */
  async openModePickerFromMenu(): Promise<void> {
    await this.openPlusMenu();
    await this.modeMenuItem.click();
  }

  /**
   * Opens the mode picker by typing the `/mode` slash command and pressing
   * Enter — the composer highlights the top-ranked match in the slash menu
   * and Enter runs it (`onKeydown`/`runCommand` in chat-composer.component.ts).
   */
  async openModePickerViaSlashCommand(): Promise<void> {
    await this.composerInput.fill('/mode');
    await this.composerInput.press('Enter');
  }

  /** An option row in the open mode picker (`role="option"`), by mode name or "Auto". */
  modeOption(name: string): Locator {
    return this.modePickerDialog.getByRole('option', { name, exact: true });
  }

  async pickMode(name: string): Promise<void> {
    await this.modeOption(name).click();
  }

  async pickAutoMode(): Promise<void> {
    await this.modeOption('Auto').click();
  }

  // --- composer skill picker -------------------------------------------------

  /** The "+" menu's "Skill" item — opens the skill picker (`openSkillPicker()`). */
  readonly skillMenuItem: Locator;

  /** The skill picker popover, `role="dialog"` aria-labelled "Choose skills". */
  readonly skillPickerDialog: Locator;

  /** "Filter skills" search input inside the skill picker. */
  readonly skillFilterInput: Locator;

  /** The picker's `role="listbox"` of selectable skills. */
  readonly skillOptionList: Locator;

  /**
   * The picker's single status line — loading, load error, or "No skills match
   * this chat or filter." Only one is ever rendered, and which one it is
   * distinguishes an empty result from a failed load, so the spec reads the
   * text rather than this POM deciding.
   */
  readonly skillPickerStatus: Locator;

  /** Region holding the chips for skills staged to send with the next message. */
  readonly pendingSkills: Locator;

  async openSkillPickerFromMenu(): Promise<void> {
    await this.openPlusMenu();
    await this.skillMenuItem.click();
  }

  /**
   * Opens the skill picker by typing the `/skill` slash command and pressing
   * Enter — the same `runCommand` path `openModePickerViaSlashCommand` uses,
   * against the composer's `skill` command entry.
   */
  async openSkillPickerViaSlashCommand(): Promise<void> {
    await this.composerInput.fill('/skill');
    await this.composerInput.press('Enter');
  }

  /**
   * Narrows the open picker to skills whose name contains `query`.
   *
   * The narrowing affordance this list needs on a shared account: the picker
   * lists every skill the account owns, which on a deployed run is every
   * other worker's seeded skills plus the residue of previous runs. Filtering
   * by the caller's UUID-suffixed name makes those rows structurally absent
   * instead of incidentally off-screen.
   */
  async narrowSkillsTo(query: string): Promise<void> {
    await this.skillFilterInput.fill(query);
  }

  /** An option row in the open skill picker (`role="option"`), by skill name. */
  skillOption(name: string): Locator {
    return this.skillPickerDialog.getByRole('option', { name });
  }

  /** Stages `name` to be sent with the next message. Picker must already be open. */
  async attachSkill(name: string): Promise<void> {
    await this.skillOption(name).click();
  }

  /** The staged chip for `name`, if it is currently staged. */
  pendingSkillChip(name: string): Locator {
    return this.pendingSkills.locator('.composer__skill-chip').filter({ hasText: name });
  }

  /** Unstages `name` via its chip's remove button. */
  async removePendingSkill(name: string): Promise<void> {
    await this.page.getByRole('button', { name: `Remove skill ${name}` }).click();
  }

  // --- personality picker ---------------------------------------------------

  /** The "Set chat personality" dialog opened by `personalityNameButton` or the composer's "+" menu. */
  readonly personaPickerDialog: Locator;

  /**
   * The composer's "+" menu persona row (`personaButtonClicked` in
   * chat-composer.component.ts). Its accessible name is the active
   * personality's own name (or "Pick personality" when none is set —
   * `personaButtonLabel()`), so unlike its fixed-label siblings (Emoji,
   * Skill, Mode, Attach file, Add from Gallery) it has no constant name to
   * match on; it's the 5th item in that template's fixed row order.
   */
  readonly composerPersonaMenuItem: Locator;

  /**
   * Opens the "Change personality" dialog and picks `name` from the list.
   * Goes through the composer's "+" menu rather than the header's
   * `personalityNameButton`, which is `display: none` below 768px
   * (chat-page.component.scss) and so would skip the mobile projects.
   */
  async changePersonality(name: string): Promise<void> {
    await this.openPlusMenu();
    await this.composerPersonaMenuItem.click();
    await this.personaPickerDialog.getByRole('option', { name }).click();
  }

  // --- model picker ----------------------------------------------------------

  /** The composer's model-picker trigger button; shows the active model's display name, or "Model" when none is selected. */
  readonly modelPickerTrigger: Locator;

  /** The model-picker's open dropdown panel. */
  readonly modelPickerOptions: Locator;

  async openModelPicker(): Promise<void> {
    await this.modelPickerTrigger.click();
  }

  /**
   * Picks a model other than `currentName` and returns its display name. A
   * fresh account has no favorited models, so the picker opens straight to
   * the Vendor view (`resetPickerStep` in model-picker.component.ts); this
   * walks each vendor's model list in turn (going "back" to try the next
   * vendor when a vendor's only model is the current one) until it finds a
   * different model to pick.
   */
  async selectModelOtherThan(currentName: string): Promise<string> {
    await this.openModelPicker();
    await this.modelPickerOptions.waitFor({ state: 'visible' });
    // When a model is already selected, `resetVendorStep` in
    // model-picker.component.ts drills straight into *that* model's vendor
    // group instead of opening on the vendor list — back out first so every
    // vendor's models get considered, not just the current one's.
    const backButton = this.modelPickerOptions.locator('.model-picker__back');
    const backVisible = await backButton
      .waitFor({ state: 'visible', timeout: OPTIONAL_UI_PROBE_TIMEOUT })
      .then(() => true)
      .catch(() => false);
    const vendorList = this.modelPickerOptions.locator('.model-picker__vendor');
    if (backVisible) {
      await backButton.click();
      // The panel re-renders asynchronously; wait for the vendor list to
      // actually land before counting it, or a `count()` taken mid-transition
      // reads 0 and the loop below never runs.
      await vendorList.first().waitFor({ state: 'visible' });
    }
    const vendorCount = await vendorList.count();
    for (let vendor = 0; vendor < vendorCount; vendor++) {
      await vendorList.nth(vendor).click();
      const options = this.modelPickerOptions.locator('.model-picker__option');
      await options.first().waitFor({ state: 'visible' });
      const optionCount = await options.count();
      for (let i = 0; i < optionCount; i++) {
        const name = (await options.nth(i).locator('.model-picker__option-name').innerText()).trim();
        if (name !== currentName) {
          await options.nth(i).click();
          return name;
        }
      }
      await this.modelPickerOptions.locator('.model-picker__back').click();
      await vendorList.first().waitFor({ state: 'visible' });
    }
    throw new Error(`No model other than "${currentName}" is available to pick.`);
  }
}
