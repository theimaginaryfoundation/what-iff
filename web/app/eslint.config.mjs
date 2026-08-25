// ESLint flat config scoped ONLY to the Playwright e2e suite (e2e/).
// The Angular app source is not linted here — it has no ESLint config of
// its own yet, and this file is intentionally scoped to avoid implying it
// covers app code too.
import tseslint from 'typescript-eslint';
import playwright from 'eslint-plugin-playwright';

export default tseslint.config(
  {
    ignores: [
      // Generated from openapi.yaml via `npm run sdk:generate` — not
      // hand-written, don't lint it.
      'e2e/sdk/schema.d.ts',
    ],
  },
  {
    files: ['e2e/**/*.ts'],
    extends: [...tseslint.configs.recommended],
  },
  {
    // Type-aware rules for the single most costly e2e bug class: a
    // Playwright call that is never awaited, which silently passes.
    // tsconfig.e2e.json exists only to give ESLint a program for e2e/.
    // Kept to a targeted set rather than all of recommendedTypeChecked so
    // the type-aware cost stays small.
    files: ['e2e/**/*.ts'],
    languageOptions: {
      parserOptions: {
        project: './tsconfig.e2e.json',
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      '@typescript-eslint/no-floating-promises': 'error',
      '@typescript-eslint/await-thenable': 'error',
      '@typescript-eslint/no-misused-promises': 'error',
      '@typescript-eslint/require-await': 'error',
    },
  },
  {
    // Playwright's recommended rules are aimed at test files (expect
    // usage, no-conditional-in-test, etc). Apply them across all of e2e/
    // rather than just tests/ so POMs/fixtures get the same safety net;
    // any rule that misfires on a POM/fixture pattern is disabled locally
    // with a comment rather than excluded wholesale here.
    files: ['e2e/**/*.ts'],
    plugins: { playwright },
    rules: {
      ...playwright.configs['flat/recommended'].rules,

      // Rules the plugin ships but leaves off in flat/recommended that we
      // opt into. All of these have zero violations in the suite today —
      // they are ratchets that keep new tests consistent, not cleanups.
      // Deliberately NOT enabled: no-raw-locators / no-nth-methods (the app
      // has no data-testids, so POMs legitimately use CSS containers and
      // .first() disambiguation), require-top-level-describe / require-tags
      // (would restructure or re-title ~33 existing tests for no safety
      // gain), max-expects and require-soft-assertions (both push toward
      // weaker or less fail-fast assertions), require-hook (misfires on
      // playwright.config.*.ts top-level code), no-hooks (we rely on hooks).
      'playwright/no-commented-out-tests': 'error',
      'playwright/no-get-by-title': 'error',
      'playwright/no-slowed-test': 'error',
      'playwright/prefer-comparison-matcher': 'error',
      'playwright/prefer-equality-matcher': 'error',
      'playwright/prefer-lowercase-title': 'error',
      'playwright/prefer-native-locators': 'error',
      'playwright/prefer-strict-equal': 'error',
      'playwright/prefer-to-be': 'error',
      'playwright/prefer-to-contain': 'error',
      'playwright/require-to-pass-timeout': 'error',
      'playwright/require-to-throw-message': 'error',

      'no-restricted-syntax': [
        'error',
        {
          selector: 'CallExpression[callee.property.name="waitForTimeout"]',
          message:
            'Static timeouts are not allowed — wait on observable DOM state instead. See "Never use static timeouts" in e2e/README.md.',
        },
        {
          selector: 'Literal[value="networkidle"]',
          message:
            'networkidle is deprecated and unreliable — wait on observable DOM state instead. See "Never wait for networkidle" in e2e/README.md.',
        },
      ],
    },
  },
  {
    // Naming a Playwright fixture is what activates it, so a spec that wants
    // only a fixture's *setup* — `userWithPersonality` registers an account,
    // seeds a personality and lands on /chat — names it and never reads it.
    // That is correct usage, not dead code, but no-unused-vars can't tell.
    // Exempt those two by name rather than switching `args` off, so a
    // genuinely unused parameter is still an error.
    files: ['e2e/tests/**/*.ts'],
    rules: {
      '@typescript-eslint/no-unused-vars': ['error', { args: 'after-used', argsIgnorePattern: '^(userWithPersonality|authenticatedPage)$' }],
      // A test parked on a known defect must name the ticket that will
      // re-enable it.
      //
      // `test.fixme` means "this should pass and doesn't" — an assertion the
      // suite has stopped making because the product is broken. Without an
      // issue key the reason lives only in the reviewer's memory: nothing
      // links the disabled test to the fix, and nobody scanning the backlog
      // can tell that shipping WET-nnn also turns coverage back on. The reason
      // string is where that link goes, because it is what Playwright prints
      // in the report next to the skip.
      //
      // Deliberately *not* applied to `test.skip`, which marks a test as not
      // applicable to the current project or viewport (export chrome is
      // desktop-only, say). That is a capability gate, not a defect, and has
      // no ticket to cite.
      //
      // Matches the reason argument of the conditional form
      // `test.fixme(cond, 'reason')`, which is the only form that should
      // appear here; a bare `test.fixme()` with no reason is caught too.
      'no-restricted-syntax': [
        'error',
        {
          selector:
            'CallExpression[callee.object.name="test"][callee.property.name="fixme"]:not([arguments.1.value=/#[0-9]+/])',
          message:
            'A test parked on a defect must cite the filed GitHub issue that will re-enable it, e.g. test.fixme(true, "… — see #123"). File the bug first. See "Parking a test on a filed bug" in e2e/README.md.',
        },
      ],
    },
  },
  {
    // POM constructors assign locators and compose other POMs — nothing else.
    //
    // POMs are exposed as fixtures (e2e/fixtures/index.ts), and a fixture is
    // constructed for every test that names it, at setup time, whether or not
    // the test goes on to use it. Work hidden in a constructor is therefore
    // work the call site can neither see nor opt out of — and it runs earlier
    // than the reader expects. Anything touching the page's runtime state
    // (`page.on`, navigation, network, anything awaited) belongs in a method,
    // where the test decides when it happens.
    //
    // The selector matches a bare call *statement* in a constructor body, so
    // `this.x = page.getByRole(...)` and `this.shell = new AppShell(page)` are
    // fine — they are assignments. It is syntactic and so won't catch a side
    // effect smuggled through an assignment; it catches the case that actually
    // occurs, which is a line added to a constructor without thinking about
    // when constructors run.
    files: ['e2e/poms/**/*.ts'],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: 'MethodDefinition[kind="constructor"] > FunctionExpression > BlockStatement > ExpressionStatement > CallExpression',
          message:
            'POM constructors assign locators only — move this side effect into a method. See "POM and fixture conventions" in e2e/README.md.',
        },
      ],
    },
  },
);
