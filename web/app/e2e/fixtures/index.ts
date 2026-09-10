import { expect, type Page } from '@playwright/test';
// Not `@playwright/test` directly: this base carries the frontend coverage
// fixtures, which are inert unless `E2E_COVERAGE=1`. See ./coverage.ts.
import { test as base } from './coverage';
import {
  AppShell,
  ChatImportModal,
  ChatPage,
  CommandPalette,
  CompactionLogPage,
  ConfirmationModal,
  DashboardPage,
  GalleryPage,
  IntegrationsPage,
  LoginPage,
  MemoriesPage,
  MemoryDetailPage,
  ModeEditModalPage,
  PersonalitiesPage,
  PersonalityDetailPage,
  ProfileSettingsModal,
  RegisterPage,
  SkillsPage,
  ThreadListPanel,
} from '../poms';
import {
  createApiClient,
  createChat,
  createMemoriesBatch,
  createPersonality,
  createRitual,
  createWebhookToken,
  deleteChat,
  deleteMemory,
  deletePersonality,
  deleteRitual,
  revokeWebhookToken,
  ApiError,
  type ApiClient,
  type Chat,
  type CreateChatInput,
  type Memory,
  type MemoryCreateRequest,
  type Ritual,
  type WebhookToken,
} from '../sdk/client';
import { deleteUser, newTestUserDetails, registerUser, type TestUser } from './api';
import { signInAsStaticAccount, staticTestAccountConfigured } from './static-account';
import { seedName, shortId, uniqueId } from './unique';

export type { TestUser };

/**
 * Registers a fresh user via the API. Prefer the `testUser` fixture; this
 * callable form exists for tests that need a *second* user beyond the one
 * the fixture provides. Returns the user and its access token (from
 * registration) so the caller can clean it up itself.
 */
export async function authenticateAsNewUser(): Promise<{
  user: TestUser;
  accessToken: string;
}> {
  const user = newTestUserDetails();
  const accessToken = await registerUser(user);
  return { user, accessToken };
}

/** Logs in via the real login form and waits for the redirect away from it. */
export async function signInAs(page: Page, user: Pick<TestUser, 'email' | 'password'>): Promise<void> {
  await new LoginPage(page).signIn(user);
  await expect(page).not.toHaveURL(/\/auth\/login/);
}

/** Re-exported so specs and POMs share one source of uniqueness — see ./unique.ts. */
export { seedName, shortId, uniqueId };

export interface SeededPersonality {
  id: string;
  name: string;
}

/**
 * Creates entities for the current test and remembers them so they can be
 * removed in teardown. Every creator is a thin call into the SDK; anything
 * needing UI has its own fixture instead.
 */
export interface Seed {
  /**
   * Creates one chat thread.
   *
   * `name` is narrowed to a plain `string`: the SDK's `Chat.name` is optional
   * because `openapi.yaml` allows a nameless chat, but this helper *supplies*
   * the name, so it always comes back. Without the narrowing every caller
   * threading a seeded name into a POM (`row(name)`, `narrowTo(name)`) has to
   * cast, which is noise on a value we already know.
   */
  thread(name?: string, input?: Omit<CreateChatInput, 'name'>): Promise<Chat & { name: string }>;
  /** Creates `count` chat threads. Names are narrowed as in `thread()`. */
  threads(count: number): Promise<(Chat & { name: string })[]>;
  /** Creates `count` global memories in a single batch call. */
  memories(count: number, overrides?: Partial<MemoryCreateRequest>): Promise<Memory[]>;
  /** Creates one ritual (a.k.a. skill). */
  ritual(
    overrides?: Partial<{
      name: string;
      description: string;
      content: string;
      personalityId: string;
    }>,
  ): Promise<Ritual>;
  /** Creates one webhook token; the raw token value is only available here. */
  webhookToken(name?: string): Promise<{ token: WebhookToken; apiToken: string }>;
  /** Creates one personality. */
  personality(name?: string): Promise<SeededPersonality>;
}

interface Tracked {
  chats: string[];
  memories: string[];
  rituals: string[];
  webhookTokens: string[];
  personalities: string[];
}

/** Records an id for teardown; ids are optional in the schema, so skip missing ones. */
function track(bucket: string[], id: string | undefined): void {
  if (id) {
    bucket.push(id);
  }
}

function makeSeed(client: ApiClient, tracked: Tracked): Seed {
  return {
    async thread(name = seedName('thread'), input = {}) {
      const chat = await createChat(client, { name, ...input });
      track(tracked.chats, chat.id);
      return { ...chat, name: chat.name ?? name };
    },

    async threads(count) {
      const chats: (Chat & { name: string })[] = [];
      for (let i = 0; i < count; i++) {
        chats.push(await this.thread(`${seedName('thread')}-${i}`));
      }
      return chats;
    },

    async memories(count, overrides = {}) {
      const items: MemoryCreateRequest[] = Array.from({ length: count }, (_, i) => ({
        content: `${seedName('memory')}-${i}`,
        level: 'global',
        type: 'Context',
        starred: false,
        ...overrides,
      }));
      const result = await createMemoriesBatch(client, items);
      const created = result.results ?? [];
      for (const memory of created) {
        track(tracked.memories, memory.id);
      }
      return created;
    },

    async ritual(overrides = {}) {
      const name = overrides.name ?? seedName('ritual');
      const ritual = await createRitual(client, {
        name,
        description: overrides.description ?? 'Seeded by the e2e suite.',
        content: overrides.content ?? 'Respond with a single short sentence.',
        personalityId: overrides.personalityId,
      });
      track(tracked.rituals, ritual.id);
      return ritual;
    },

    async webhookToken(name = seedName('webhook')) {
      const created = await createWebhookToken(client, name);
      track(tracked.webhookTokens, created.token.id);
      return { token: created.token, apiToken: created.api_token };
    },

    async personality(name = seedName('personality')) {
      const personality = await createPersonality(client, {
        name,
        systemPrompt: 'You are a terse, friendly assistant used only for automated end-to-end testing.',
      });
      track(tracked.personalities, personality.id);
      return { id: personality.id as string, name };
    },
  };
}

/**
 * Deletes everything `seed` created. Individual failures are logged and the
 * sweep continues — one undeletable entity shouldn't leave the rest behind —
 * but the count is returned so the caller can decide whether to surface it.
 */
async function teardownSeed(client: ApiClient, tracked: Tracked): Promise<number> {
  const jobs: [string, string, (id: string) => Promise<void>][] = [
    ...tracked.chats.map((id): [string, string, (id: string) => Promise<void>] => ['chat', id, i => deleteChat(client, i)]),
    ...tracked.memories.map((id): [string, string, (id: string) => Promise<void>] => ['memory', id, i => deleteMemory(client, i)]),
    ...tracked.rituals.map((id): [string, string, (id: string) => Promise<void>] => ['ritual', id, i => deleteRitual(client, i)]),
    ...tracked.webhookTokens.map((id): [string, string, (id: string) => Promise<void>] => [
      'webhook token',
      id,
      i => revokeWebhookToken(client, i),
    ]),
    ...tracked.personalities.map((id): [string, string, (id: string) => Promise<void>] => [
      'personality',
      id,
      i => deletePersonality(client, i),
    ]),
  ];

  let failures = 0;
  for (const [kind, id, remove] of jobs) {
    try {
      await remove(id);
    } catch (err) {
      // A 404 means the test itself already deleted this entity (e.g. a
      // "delete chat" test) — that's success, not a teardown failure.
      if (err instanceof ApiError && err.status === 404) {
        continue;
      }
      failures++;
      console.error(`seed fixture: failed to delete ${kind} ${id}:`, err);
    }
  }
  return failures;
}

/** `testUser` plus a personality already created for them. */
export interface UserWithPersonality {
  user: TestUser;
  personality: SeededPersonality;
  /** A page already logged in as `user`. */
  page: Page;
}

/**
 * Every POM, constructed against this test's page.
 *
 * Playwright builds a fixture only for the tests that name it, so exposing
 * every one of them costs an unused one nothing. They depend on the built-in
 * `page`, which is the same object `authenticatedPage` and
 * `userWithPersonality.page` hand back — so a spec can ask for a POM
 * alongside whichever auth fixture it needs and they all drive the same tab.
 *
 * Construction only assigns locators (enforced by `no-restricted-syntax` over
 * `e2e/poms/` — see eslint.config.mjs), so naming a POM here never navigates,
 * waits, or touches the network. The test still decides when anything
 * happens by calling a method.
 */
interface PomFixtures {
  shell: AppShell;
  chatImportModal: ChatImportModal;
  chatPage: ChatPage;
  commandPalette: CommandPalette;
  compactionLogPage: CompactionLogPage;
  confirmationModal: ConfirmationModal;
  dashboardPage: DashboardPage;
  integrationsPage: IntegrationsPage;
  loginPage: LoginPage;
  galleryPage: GalleryPage;
  memoriesPage: MemoriesPage;
  memoryDetailPage: MemoryDetailPage;
  modeEditModalPage: ModeEditModalPage;
  personalitiesPage: PersonalitiesPage;
  personalityDetailPage: PersonalityDetailPage;
  profileSettingsModal: ProfileSettingsModal;
  registerPage: RegisterPage;
  skillsPage: SkillsPage;
  threadListPanel: ThreadListPanel;
}

interface Fixtures {
  /** A freshly registered account, unique to this test. Self-deletes via the API in teardown. */
  testUser: TestUser;
  /** An SDK client authenticated as `testUser`. */
  apiClient: ApiClient;
  /** Creates entities for `testUser` and deletes them in teardown. */
  seed: Seed;
  /** A page already signed in as `testUser`. */
  authenticatedPage: Page;
  /**
   * A logged-in user that already owns a personality. Needed by anything that
   * navigates the authenticated app: `personalitySetupGuard` bounces accounts
   * with zero personalities to `/personality/getting-started`.
   */
  userWithPersonality: UserWithPersonality;
}

interface InternalFixtures {
  /** Internal: the registered user plus its access token, shared by the fixtures above. */
  authenticatedTestUser: { user: TestUser; accessToken: string };
}

export const test = base.extend<Fixtures & InternalFixtures & PomFixtures>({
  shell: async ({ page }, use) => use(new AppShell(page)),
  chatImportModal: async ({ page }, use) => use(new ChatImportModal(page)),
  chatPage: async ({ page }, use) => use(new ChatPage(page)),
  commandPalette: async ({ page }, use) => use(new CommandPalette(page)),
  compactionLogPage: async ({ page }, use) => use(new CompactionLogPage(page)),
  confirmationModal: async ({ page }, use) => use(new ConfirmationModal(page)),
  dashboardPage: async ({ page }, use) => use(new DashboardPage(page)),
  integrationsPage: async ({ page }, use) => use(new IntegrationsPage(page)),
  loginPage: async ({ page }, use) => use(new LoginPage(page)),
  galleryPage: async ({ page }, use) => use(new GalleryPage(page)),
  memoriesPage: async ({ page }, use) => use(new MemoriesPage(page)),
  memoryDetailPage: async ({ page }, use) => use(new MemoryDetailPage(page)),
  modeEditModalPage: async ({ page }, use) => use(new ModeEditModalPage(page)),
  personalitiesPage: async ({ page }, use) => use(new PersonalitiesPage(page)),
  personalityDetailPage: async ({ page }, use) => use(new PersonalityDetailPage(page)),
  profileSettingsModal: async ({ page }, use) => use(new ProfileSettingsModal(page)),
  registerPage: async ({ page }, use) => use(new RegisterPage(page)),
  skillsPage: async ({ page }, use) => use(new SkillsPage(page)),
  threadListPanel: async ({ page }, use) => use(new ThreadListPanel(page)),

  authenticatedTestUser: async ({}, use) => {
    // With a static account configured (see fixtures/static-account.ts): one
    // pre-existing account, shared across the run and never deleted.
    // mock/local: a fresh account per test, self-deleted below.
    if (staticTestAccountConfigured()) {
      const registered = await signInAsStaticAccount();
      await use(registered);
      return;
    }

    const registered = await authenticateAsNewUser();

    await use(registered);

    try {
      await deleteUser(registered.accessToken);
    } catch (err) {
      // Same policy as teardownSeed below: log locally, fail in CI. An account
      // is the *owner* of everything a test created, so leaking one leaks more
      // than a stray seeded row — it would be backwards for seeded entities to
      // fail loudly while the accounts holding them stayed silent. There is no
      // cleanup sweep to fall back on; see e2e/README.md.
      console.error(`testUser fixture: failed to delete ${registered.user.username}:`, err);
      if (process.env['CI']) {
        throw new Error(`testUser fixture: failed to delete account ${registered.user.username} (see logged error above).`);
      }
    }
  },

  testUser: async ({ authenticatedTestUser }, use) => {
    await use(authenticatedTestUser.user);
  },

  apiClient: async ({ authenticatedTestUser }, use) => {
    await use(createApiClient(authenticatedTestUser.accessToken));
  },

  seed: async ({ apiClient }, use) => {
    const tracked: Tracked = {
      chats: [],
      memories: [],
      rituals: [],
      webhookTokens: [],
      personalities: [],
    };

    await use(makeSeed(apiClient, tracked));

    const failures = await teardownSeed(apiClient, tracked);
    // Locally a noisy log is enough. In CI a systematically broken teardown is
    // invisible until the database fills up, so fail the test there instead —
    // after the assertions have run, so it can't mask a real failure.
    if (failures > 0 && process.env['CI']) {
      throw new Error(`seed fixture: ${failures} entities could not be deleted in teardown (see logs above).`);
    }
  },

  authenticatedPage: async ({ page, testUser }, use) => {
    // A static-account config authenticates once in its `setup` project and
    // hands every test the resulting storage state, so the session is already
    // present before the first navigation — driving the login form again here
    // would re-authenticate per test, which is exactly what once locked a
    // shared account out of its identity provider. mock/local have no such
    // state and log in normally.
    if (!staticTestAccountConfigured()) {
      await signInAs(page, testUser);
    }
    await use(page);
  },

  userWithPersonality: async ({ authenticatedPage, testUser, seed }, use) => {
    const personality = await seed.personality(`E2E Persona ${shortId()}`);

    // `authenticatedPage` has already booted the app with zero personalities, so
    // `personalitySetupGuard` has parked it on `/personality?setup=1` — a
    // route that opens the "Choose a Personality" modal and swallows every
    // click on the chrome behind it. Navigate somewhere clean now that the
    // personality exists, so tests that never navigate themselves (sidebar
    // nav, command palette) start from a usable app.
    await authenticatedPage.goto('/chat');
    await new AppShell(authenticatedPage).dismissAnnouncementIfPresent();

    await use({ user: testUser, personality, page: authenticatedPage });
  },
});

export { expect };
// For tests that build their own context: the fixtures only reach pages the
// `page`/`context` fixtures created, so a page from `browser.newContext()` has
// to enrol itself. No-op unless `E2E_COVERAGE=1`.
export { attachCoverage } from './coverage';
// Only `tests/functional/coverage/collection.spec.ts` needs these two, to assert
// the pipeline is doing what it claims.
export { coverageEntryCount, coverageLostNavigations, expectedRecordedScripts } from './coverage';
