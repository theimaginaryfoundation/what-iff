import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ActivatedRouteSnapshot, RouterStateSnapshot, convertToParamMap } from '@angular/router';

import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileWorkspaceService } from './file-workspace.service';
import { unsavedWorkspaceGuard } from './unsaved-workspace.guard';

function nav(url: string): { route: ActivatedRouteSnapshot; state: RouterStateSnapshot } {
  const query = new URLSearchParams(url.split('?')[1] ?? '');
  const queryParamMap = convertToParamMap(Object.fromEntries(query));
  const route = { queryParamMap } as ActivatedRouteSnapshot;
  return { route, state: { url, root: route } as RouterStateSnapshot };
}

describe('unsavedWorkspaceGuard', () => {
  let unsaved: boolean;
  const confirmDiscardChanges = vi.fn();

  beforeEach(() => {
    unsaved = true;
    confirmDiscardChanges.mockReset().mockResolvedValue(false);
    TestBed.configureTestingModule({
      providers: [
        provideZonelessChangeDetection(),
        { provide: FileWorkspaceService, useValue: { unsaved: () => unsaved } },
        { provide: ConfirmationService, useValue: { confirmDiscardChanges } },
      ],
    });
  });

  function run(from: string, to: string): unknown {
    const current = nav(from);
    return TestBed.runInInjectionContext(() =>
      unsavedWorkspaceGuard(null, current.route, current.state, nav(to).state),
    );
  }

  it('lets everything through when nothing is unsaved', () => {
    unsaved = false;
    expect(run('/chat/1?file=a', '/gallery')).toBe(true);
    expect(confirmDiscardChanges).not.toHaveBeenCalled();
  });

  it('does not ask when the open file stays open', () => {
    expect(run('/chat/1?file=a', '/chat/1?file=a&tab=x')).toBe(true);
    expect(confirmDiscardChanges).not.toHaveBeenCalled();
  });

  it('asks when another file opens, the file closes, or the page changes', async () => {
    for (const to of ['/chat/1?file=b', '/chat/1', '/gallery?file=a']) {
      await expect(Promise.resolve(run('/chat/1?file=a', to))).resolves.toBe(false);
    }
    expect(confirmDiscardChanges).toHaveBeenCalledTimes(3);
  });
});
