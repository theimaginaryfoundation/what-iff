import { TestBed } from '@angular/core/testing';

import { MemoryProvenanceBadgeComponent } from './memory-provenance-badge.component';

describe('MemoryProvenanceBadgeComponent', () => {
  function render(provenance: 'user' | 'external' | null, speaker: string | null = null): HTMLElement {
    const fixture = TestBed.createComponent(MemoryProvenanceBadgeComponent);
    fixture.componentRef.setInput('provenance', provenance);
    fixture.componentRef.setInput('speaker', speaker);
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('names the speaker of an external memory', () => {
    expect(render('external', 'alice').textContent?.trim()).toBe('External · alice');
  });

  it('says External when the speaker is unknown', () => {
    expect(render('external').textContent?.trim()).toBe('External');
  });

  it('renders nothing for your own memories', () => {
    expect(render('user', 'alice').querySelector('.provenance-badge')).toBeNull();
    expect(render(null).querySelector('.provenance-badge')).toBeNull();
  });
});
