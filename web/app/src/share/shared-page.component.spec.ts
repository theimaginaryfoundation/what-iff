import { Component, input, provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it, vi } from 'vitest';

import { ShareApiService } from './share-api.service';
import { SharedFileViewComponent } from './shared-file-view.component';
import { SharedPageComponent } from './shared-page.component';
import { SharedWebAppComponent } from './shared-web-app.component';

@Component({ selector: 'share-file-view', standalone: true, template: 'file:{{ view().name }}' })
class FakeFileView {
  readonly shareKey = input('');
  readonly view = input.required<{ name: string }>();
}
@Component({ selector: 'share-web-app', standalone: true, template: 'app' })
class FakeWebApp {
  readonly shareKey = input('');
  readonly view = input<unknown>();
}

async function setup(api: Partial<ShareApiService>) {
  await TestBed.configureTestingModule({
    imports: [SharedPageComponent],
    providers: [provideZonelessChangeDetection(), { provide: ShareApiService, useValue: api }],
  })
    .overrideComponent(SharedPageComponent, {
      remove: { imports: [SharedFileViewComponent, SharedWebAppComponent] },
      add: { imports: [FakeFileView, FakeWebApp] },
    })
    .compileComponents();
  const fixture = TestBed.createComponent(SharedPageComponent);
  fixture.componentRef.setInput('key', 'k'.repeat(22));
  const settle = async () => {
    for (let i = 0; i < 3; i++) {
      fixture.detectChanges();
      await fixture.whenStable();
      await new Promise(r => setTimeout(r));
    }
    fixture.detectChanges();
  };
  await settle();
  return { fixture, el: fixture.nativeElement as HTMLElement, settle };
}

describe('SharedPageComponent', () => {
  it('asks for the passcode, says how many tries are left, and opens the share once it is right', async () => {
    const open = vi
      .fn()
      .mockResolvedValueOnce({ state: 'locked' })
      .mockResolvedValue({ state: 'open', view: { name: 'notes.md', web_app: false } });
    const unlock = vi.fn().mockResolvedValueOnce({ state: 'wrong', attemptsLeft: 4 }).mockResolvedValue({ state: 'unlocked' });
    const { el, settle } = await setup({ open, unlock });
    const input = el.querySelector('#share-passcode') as HTMLInputElement;
    const submit = () => el.querySelector('form')!.dispatchEvent(new Event('submit'));

    input.value = 'nope';
    input.dispatchEvent(new Event('input'));
    await settle();
    submit();
    await settle();
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('4 tries left');

    input.value = '  Abc123Def456\n';
    input.dispatchEvent(new Event('input'));
    await settle();
    submit();
    await settle();
    expect(unlock).toHaveBeenLastCalledWith('k'.repeat(22), 'Abc123Def456');
    expect(el.textContent).toContain('file:notes.md');
  });

  it('says how long a ban lasts, and when a link does not work', async () => {
    const { el, settle } = await setup({
      open: vi.fn().mockResolvedValue({ state: 'locked' }),
      unlock: vi.fn().mockResolvedValue({ state: 'banned', retryAfterSeconds: 3600 }),
    });
    const input = el.querySelector('#share-passcode') as HTMLInputElement;
    input.value = 'x';
    input.dispatchEvent(new Event('input'));
    await settle();
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle();
    expect(el.textContent).toContain('Too many tries');
    expect(el.textContent).toContain('in 60 minutes');

    TestBed.resetTestingModule();
    const missing = await setup({ open: vi.fn().mockResolvedValue({ state: 'missing' }) });
    expect(missing.el.textContent).toContain('This link does not work');
  });
});
