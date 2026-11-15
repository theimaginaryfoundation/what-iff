import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideMarkdown } from 'ngx-markdown';
import { afterEach, describe, expect, it } from 'vitest';

import { NotebookOutputComponent } from './notebook-output.component';
import { NOTEBOOK_INTERACTIVE_OUTPUT } from './notebook-run.config';

const scripted = [{ output_type: 'display_data', data: { 'text/html': '<div></div><script>x()</script>' }, metadata: {} }];

async function render(mayRun: boolean) {
  await TestBed.configureTestingModule({
    imports: [NotebookOutputComponent],
    providers: [provideZonelessChangeDetection(), provideMarkdown(), { provide: NOTEBOOK_INTERACTIVE_OUTPUT, useValue: mayRun }],
  }).compileComponents();
  const fixture = TestBed.createComponent(NotebookOutputComponent);
  fixture.componentRef.setInput('outputs', scripted);
  fixture.detectChanges();
  return fixture.nativeElement as HTMLElement;
}

describe('NotebookOutputComponent', () => {
  afterEach(() => TestBed.resetTestingModule());

  it('offers to run script-driven output where it can run', async () => {
    expect((await render(true)).textContent).toContain('Show interactive output');
  });

  it('says script-driven output cannot be shown where it cannot run (the share site), offering nothing', async () => {
    const el = await render(false);
    expect(el.textContent).not.toContain('Show interactive output');
    expect(el.textContent).toContain('cannot be shown here');
  });
});
