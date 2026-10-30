import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';

import { AgentEntity } from '../../../core/models/agent-entity.model';
import { AgentEntityService } from '../../../core/services/agent-entity.service';
import { GLOBAL_PERSONALITY_FILTER } from '../helpers/memory-filter.helpers';
import { EntityListComponent } from './entity-list.component';

const CHRIS: AgentEntity = {
  id: 'e-1',
  user_id: 'u-1',
  key: 'chris smith',
  title: 'Chris Smith',
  body: 'Works with [[Dana Lee]].',
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-02T00:00:00Z',
};

const PINNED: AgentEntity = {
  ...CHRIS,
  id: 'e-2',
  key: 'garden',
  title: 'Garden',
  body: 'Tomatoes.',
  pinned_personality_id: 'p-1',
  pinned_personality_name: 'Iris',
};

function makeService() {
  return {
    list: vi.fn().mockReturnValue(of({ results: [CHRIS, PINNED], total_count: 2, page: 1 })),
    updateBody: vi.fn((id: string, body: string) => of({ ...CHRIS, id, body })),
    delete: vi.fn().mockReturnValue(of(undefined)),
  };
}

async function setup(
  service = makeService(),
): Promise<{ fixture: ComponentFixture<EntityListComponent>; service: ReturnType<typeof makeService> }> {
  TestBed.resetTestingModule();
  await TestBed.configureTestingModule({
    imports: [EntityListComponent],
    providers: [{ provide: AgentEntityService, useValue: service }],
  }).compileComponents();
  const fixture = TestBed.createComponent(EntityListComponent);
  fixture.detectChanges();
  await fixture.whenStable();
  fixture.detectChanges();
  return { fixture, service };
}

function cards(fixture: ComponentFixture<EntityListComponent>): HTMLElement[] {
  return Array.from((fixture.nativeElement as HTMLElement).querySelectorAll('[data-testid="entity-card"]'));
}

function buttonIn(el: HTMLElement, label: string): HTMLButtonElement {
  const btn = Array.from(el.querySelectorAll('button')).find(b => b.textContent?.trim() === label);
  if (!btn) throw new Error(`no ${label} button`);
  return btn as HTMLButtonElement;
}

describe('EntityListComponent', () => {
  it('lists entities with their scope and renders [[links]] as buttons', async () => {
    const { fixture } = await setup();
    const [chris, garden] = cards(fixture);
    expect(chris.querySelector('.entity-card__title')?.textContent).toContain('Chris Smith');
    expect(chris.querySelector('.entity-card__scope')?.textContent).toContain('Global');
    expect(garden.querySelector('.entity-card__scope')?.textContent).toContain('Iris');
    expect(chris.querySelector('.entity-card__link')?.textContent?.trim()).toBe('Dana Lee');
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('2 entities');
  });

  it('follows a link by searching for that entity, listing it first', async () => {
    const { fixture, service } = await setup();
    const dana: AgentEntity = { ...CHRIS, id: 'e-3', key: 'dana lee', title: 'Dana Lee', body: 'Pairs with [[Chris Smith]].' };
    service.list.mockReturnValue(of({ results: [CHRIS, dana], total_count: 2, page: 1 }));
    (cards(fixture)[0].querySelector('.entity-card__link') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(service.list).toHaveBeenLastCalledWith(1, 50, { query: 'Dana Lee' });
    expect(cards(fixture)[0].querySelector('.entity-card__title')?.textContent).toContain('Dana Lee');
  });

  it('filters by personality or global only', async () => {
    const { fixture, service } = await setup();
    fixture.componentInstance.onPersonalityFilterChange(GLOBAL_PERSONALITY_FILTER);
    expect(service.list).toHaveBeenLastCalledWith(1, 50, { query: '', global_only: true });
    fixture.componentInstance.onPersonalityFilterChange('p-1');
    expect(service.list).toHaveBeenLastCalledWith(1, 50, { query: '', personality_id: 'p-1' });
  });

  it('edits an entity and blocks text over the limit', async () => {
    const { fixture, service } = await setup();
    buttonIn(cards(fixture)[0], 'Edit').click();
    fixture.detectChanges();

    const component = fixture.componentInstance;
    component.draft.set('x'.repeat(component.maxChars + 1));
    fixture.detectChanges();
    expect(buttonIn(cards(fixture)[0], 'Save').disabled).toBe(true);

    component.draft.set('Moved to Oslo.');
    fixture.detectChanges();
    buttonIn(cards(fixture)[0], 'Save').click();
    fixture.detectChanges();
    expect(service.updateBody).toHaveBeenCalledWith('e-1', 'Moved to Oslo.');
    expect(cards(fixture)[0].querySelector('.entity-card__body')?.textContent).toContain('Moved to Oslo.');
  });

  it('asks before deleting, then reloads', async () => {
    const { fixture, service } = await setup();
    buttonIn(cards(fixture)[1], 'Delete').click();
    fixture.detectChanges();
    expect(service.delete).not.toHaveBeenCalled();
    buttonIn(cards(fixture)[1], 'Delete').click();
    expect(service.delete).toHaveBeenCalledWith('e-2');
    expect(service.list).toHaveBeenCalledTimes(2);
  });

  it('shows a load error', async () => {
    const service = makeService();
    service.list.mockReturnValue(throwError(() => new Error('boom')));
    const { fixture } = await setup(service);
    expect((fixture.nativeElement as HTMLElement).querySelector('[role="alert"]')?.textContent).toContain('boom');
  });
});
