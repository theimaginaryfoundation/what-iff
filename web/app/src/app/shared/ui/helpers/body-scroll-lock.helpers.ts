const LOCK_CLASS = 'overflow-hidden';
let lockCount = 0;
let bodyHadClassBeforeFirstLock = false;

export interface BodyScrollLockHandle {
  readonly body: HTMLElement;
  readonly hadClassBeforeLock: boolean;
  released: boolean;
}

export function lockBodyScroll(body: HTMLElement | null | undefined): BodyScrollLockHandle | null {
  if (!body) {
    return null;
  }

  const hadClassBeforeLock = body.classList.contains(LOCK_CLASS);
  if (lockCount === 0) {
    bodyHadClassBeforeFirstLock = hadClassBeforeLock;
  }

  lockCount += 1;
  body.classList.add(LOCK_CLASS);

  return {
    body,
    hadClassBeforeLock,
    released: false,
  };
}

export function releaseBodyScroll(handle: BodyScrollLockHandle | null | undefined): void {
  if (!handle || handle.released) {
    return;
  }

  handle.released = true;
  lockCount = Math.max(0, lockCount - 1);

  if (lockCount === 0 && !bodyHadClassBeforeFirstLock) {
    handle.body.classList.remove(LOCK_CLASS);
  }
}

export function resetBodyScrollLockForTests(): void {
  lockCount = 0;
  bodyHadClassBeforeFirstLock = false;
}
