import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of, throwError } from 'rxjs';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ShareLink } from '../../core/models/share-link.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { ShareLinkService } from '../../core/services/share-link.service';
import { SharedLinksPageComponent } from './shared-links-page.component';

const link = (key: string, name: string): ShareLink => ({
  key,
  file_id: `f-${key}`,
  file_name: name,
  folder: 'site',
  file_type: 'text/html',
  web_app: true,
  passcode_required: true,
  passcode: 'Abc123Def456',
  created_at: '2026-11-14T00:00:00Z',
  updated_at: '2026-11-14T00:00:00Z',
});

async function setup(service: Record<string, unknown>, confirm = vi.fn(async () => true)) {
  await TestBed.configureTestingModule({
    imports: [SharedLinksPageComponent],
    providers: [
      provideZonelessChangeDetection(),
      provideRouter([]),
      { provide: ShareLinkService, useValue: { enabled: true, urlOf: (l: ShareLink) => `https://share.test/s/${l.key}`, ...service } },
      { provide: ConfirmationService, useValue: { confirm } },
    ],
  }).compileComponents();
  const fixture = TestBed.createComponent(SharedLinksPageComponent);
  const settle = async () => {
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(r => setTimeout(r));
    fixture.detectChanges();
  };
  await settle();
  const el = fixture.nativeElement as HTMLElement;
  const button = (label: string) =>
    [...el.querySelectorAll('button')].find(
      b => b.getAttribute('aria-label') === label || b.textContent?.trim() === label,
    ) as HTMLButtonElement;
  return { el, button, settle, confirm };
}

describe('SharedLinksPageComponent', () => {
  afterEach(() => TestBed.resetTestingModule());

  it('lists the links; stopping one asks first, and a failure keeps the list', async () => {
    const stop = vi
      .fn()
      .mockReturnValueOnce(throwError(() => new Error('down')))
      .mockReturnValue(of(undefined));
    const confirm = vi.fn().mockResolvedValueOnce(false).mockResolvedValue(true);
    const { el, button, settle } = await setup({ list: () => of([link('a', 'index.html'), link('b', 'notes.md')]), stop }, confirm);
    expect(el.querySelectorAll('li')).toHaveLength(2);

    button('Stop sharing index.html').click();
    await settle();
    expect(stop).not.toHaveBeenCalled(); // not confirmed

    button('Stop sharing index.html').click();
    await settle();
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('Could not stop sharing index.html');
    expect(el.querySelectorAll('li')).toHaveLength(2);

    button('Stop sharing index.html').click();
    await settle();
    expect(el.querySelectorAll('li')).toHaveLength(1);
    expect(el.querySelector('[role="alert"]')).toBeNull();
  });

  it('says when it cannot load, and tries again', async () => {
    const list = vi
      .fn()
      .mockReturnValueOnce(throwError(() => new Error('down')))
      .mockReturnValue(of([]));
    const { el, button, settle } = await setup({ list });
    expect(el.textContent).toContain('Could not load your shared links');
    button('Try again').click();
    await settle();
    expect(el.textContent).toContain('Nothing shared yet');
  });

  it('says when sharing is not set up', async () => {
    const list = vi.fn();
    await TestBed.configureTestingModule({
      imports: [SharedLinksPageComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        { provide: ShareLinkService, useValue: { enabled: false, list } },
        { provide: ConfirmationService, useValue: {} },
      ],
    }).compileComponents();
    const fixture = TestBed.createComponent(SharedLinksPageComponent);
    fixture.detectChanges();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('Sharing is not set up');
    expect(list).not.toHaveBeenCalled();
  });
});
