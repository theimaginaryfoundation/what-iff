import { TestBed } from '@angular/core/testing';
import { provideMarkdown } from 'ngx-markdown';

import { BuzzTermsComponent } from './buzz-terms.component';
import { BuzzJoinPolicy } from './buzz.service';

const policy: BuzzJoinPolicy = { terms_markdown: 'Be kind.', privacy_markdown: '', age_attestation_required: true, version: 'v3' };

describe('BuzzTermsComponent', () => {
  it('starts unticked again when it is given new terms', async () => {
    TestBed.configureTestingModule({ imports: [BuzzTermsComponent], providers: [provideMarkdown()] });
    const fixture = TestBed.createComponent(BuzzTermsComponent);
    fixture.componentRef.setInput('policy', policy);
    fixture.detectChanges();
    await fixture.whenStable();
    const box = (id: string) => fixture.nativeElement.querySelector(`[data-testid=${id}]`) as HTMLInputElement;
    for (const id of ['buzz-terms-accept', 'buzz-terms-age']) {
      box(id).checked = true;
      box(id).dispatchEvent(new Event('change'));
    }
    fixture.detectChanges();
    expect(fixture.componentInstance.canAccept()).toBe(true);

    fixture.componentRef.setInput('policy', { ...policy, terms_markdown: 'Be kinder.', version: 'v4' });
    fixture.detectChanges();
    await fixture.whenStable();

    expect(fixture.componentInstance.accepted()).toBe(false);
    expect(fixture.componentInstance.ageConfirmed()).toBe(false);
    expect(box('buzz-terms-accept').checked).toBe(false);
    expect(box('buzz-terms-age').checked).toBe(false);
    expect((fixture.nativeElement.querySelector('[data-testid=buzz-terms-submit]') as HTMLButtonElement).disabled).toBe(true);
  });
});
