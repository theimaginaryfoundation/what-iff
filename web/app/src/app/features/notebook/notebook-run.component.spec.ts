import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { NotebookRunComponent } from './notebook-run.component';

describe('NotebookRunComponent', () => {
  beforeEach(() => TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection()] }));

  function render(url: string): HTMLElement {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const fixture = TestBed.createComponent(NotebookRunComponent);
    fixture.componentRef.setInput('url', url);
    fixture.componentRef.setInput('name', 'a.ipynb');
    fixture.componentRef.setInput('notebook', {});
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('frames a runner on another https origin', () => {
    expect(render('https://notebooks.example.test/lab/index.html').querySelector('iframe')).not.toBeNull();
  });

  it('refuses a runner on this origin or over plain http, whoever passed it in', () => {
    for (const url of [`${location.origin}/lab/index.html`, 'http://notebooks.example.test/lab/index.html']) {
      const el = render(url);
      expect(el.querySelector('iframe')).toBeNull();
      expect(el.querySelector('[role="alert"]')).not.toBeNull();
    }
  });
});
