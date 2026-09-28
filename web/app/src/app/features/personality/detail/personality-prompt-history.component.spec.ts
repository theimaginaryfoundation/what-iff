import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';

import { PersonalityPromptChange } from '../../../core/models/personality.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { PersonalityPromptHistoryComponent } from './personality-prompt-history.component';

describe('PersonalityPromptHistoryComponent', () => {
  let fixture: ComponentFixture<PersonalityPromptHistoryComponent>;
  let component: PersonalityPromptHistoryComponent;
  let personalityService: { listPromptChanges: ReturnType<typeof vi.fn>; revertPromptChange: ReturnType<typeof vi.fn> };
  let confirmation: { confirm: ReturnType<typeof vi.fn> };

  const change: PersonalityPromptChange = {
    id: 'change-1',
    user_id: 'user-1',
    personality_id: 'personality-9',
    old_prompt: 'Old prompt text',
    new_prompt: 'New prompt text',
    action: 'edit',
    created_at: '2026-08-12T12:00:00Z',
  };

  beforeEach(async () => {
    personalityService = {
      listPromptChanges: vi.fn().mockReturnValue(of([change])),
      revertPromptChange: vi.fn().mockReturnValue(of({ ...change, id: 'change-2', action: 'revert' })),
    };
    confirmation = { confirm: vi.fn().mockResolvedValue(true) };

    await TestBed.configureTestingModule({
      imports: [PersonalityPromptHistoryComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: PersonalityService, useValue: personalityService },
        { provide: ConfirmationService, useValue: confirmation },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(PersonalityPromptHistoryComponent);
    component = fixture.componentInstance;
    fixture.componentRef.setInput('personalityId', 'personality-9');
    fixture.componentRef.setInput('personalityName', 'Vera');
    fixture.componentRef.setInput('currentPrompt', 'New prompt text');
    fixture.detectChanges();
  });

  function toggle(): HTMLButtonElement {
    return fixture.nativeElement.querySelector('[data-testid="prompt-change-toggle"]') as HTMLButtonElement;
  }

  it('starts collapsed and does not fetch until expanded', () => {
    expect(toggle().getAttribute('aria-expanded')).toBe('false');
    expect(fixture.nativeElement.textContent).toContain('Prompt changes');
    expect(fixture.nativeElement.textContent).not.toContain('Old prompt text');
    expect(personalityService.listPromptChanges).not.toHaveBeenCalled();
  });

  it('loads this personality’s changes on expand and renders the before/after diff', () => {
    toggle().click();
    fixture.detectChanges();

    expect(personalityService.listPromptChanges).toHaveBeenCalledWith('personality-9');
    expect(toggle().getAttribute('aria-expanded')).toBe('true');
    const list = fixture.nativeElement.querySelector('[aria-label="Personality prompt changes"]') as HTMLElement;
    expect(list.querySelector('.diff-pane--old .diff-pane__content')?.textContent).toBe('Old prompt text');
    expect(list.querySelector('.diff-pane--new .diff-pane__content')?.textContent).toBe('New prompt text');
    expect(list.textContent).toContain('Edited');
    expect(list.textContent).toContain('Restore previous');
  });

  it('shows an empty state when nothing has been logged', () => {
    personalityService.listPromptChanges.mockReturnValue(of([]));
    toggle().click();
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('No prompt changes logged yet.');
  });

  it('shows the load error in place of the list', () => {
    personalityService.listPromptChanges.mockReturnValue(throwError(() => new Error('boom')));
    toggle().click();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('boom');
  });

  it('refetches when the saved prompt changes while expanded, but not while collapsed', () => {
    fixture.componentRef.setInput('currentPrompt', 'Edited while collapsed');
    fixture.detectChanges();
    expect(personalityService.listPromptChanges).not.toHaveBeenCalled();

    toggle().click();
    fixture.detectChanges();
    expect(personalityService.listPromptChanges).toHaveBeenCalledTimes(1);

    fixture.componentRef.setInput('currentPrompt', 'Edited while open');
    fixture.detectChanges();
    expect(personalityService.listPromptChanges).toHaveBeenCalledTimes(2);
  });

  it('restores through a confirmed revert, reloads the list and notifies the host', async () => {
    const restored = vi.fn();
    component.restored.subscribe(restored);
    toggle().click();
    fixture.detectChanges();
    personalityService.listPromptChanges.mockClear();

    await component.restore(change);

    expect(confirmation.confirm).toHaveBeenCalled();
    expect(personalityService.revertPromptChange).toHaveBeenCalledWith('personality-9', 'change-1');
    expect(personalityService.listPromptChanges).toHaveBeenCalledWith('personality-9');
    expect(restored).toHaveBeenCalledTimes(1);
    expect(component.notice()).toBe('Previous prompt restored.');
  });

  it('does nothing when the restore is not confirmed', async () => {
    confirmation.confirm.mockResolvedValue(false);
    const restored = vi.fn();
    component.restored.subscribe(restored);

    await component.restore(change);

    expect(personalityService.revertPromptChange).not.toHaveBeenCalled();
    expect(restored).not.toHaveBeenCalled();
  });
});
