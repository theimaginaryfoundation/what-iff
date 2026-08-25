// jsdom doesn't implement these browser APIs; Karma ran specs in real Chrome
// where they exist natively. Stub them so specs written against Chrome
// behavior don't need per-spec workarounds.

if (!window.matchMedia) {
  window.matchMedia = (query: string): MediaQueryList => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}

// Installed fresh before every test, not once at module load. The unit-test
// builder runs with `isolate: false`, so every spec file in a worker shares one
// `navigator` object; a spy left installed on a shared `clipboard.writeText`
// carries its call history into later specs and makes `not.toHaveBeenCalled()`
// fail depending on file order.
function installClipboardStub(): void {
  Object.defineProperty(navigator, 'clipboard', {
    value: {
      writeText: () => Promise.resolve(),
      readText: () => Promise.resolve(''),
    },
    writable: true,
    configurable: true,
  });
}

installClipboardStub();

beforeEach(() => {
  installClipboardStub();
});

if (!Element.prototype.scrollTo) {
  Element.prototype.scrollTo = () => {};
}

// Jasmine's spyOn auto-restores after each test; vi.spyOn doesn't unless told
// to. Without this, a spy created in one test (e.g. on navigator.clipboard)
// stays installed and accumulates call history in later tests.
afterEach(() => {
  vi.restoreAllMocks();
});

export {};
