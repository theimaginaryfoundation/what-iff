import { TestBed } from '@angular/core/testing';
import { ConstellationService } from '../../features/constellation/constellation.service';
import { AppSidebarHeaderComponent } from './app-sidebar-header.component';

describe('AppSidebarHeaderComponent constellation entry', () => {
  it('places a named constellation action immediately after Help and opens the public overlay', async () => {
    await TestBed.configureTestingModule({ imports: [AppSidebarHeaderComponent] }).compileComponents();
    const fixture = TestBed.createComponent(AppSidebarHeaderComponent);
    fixture.detectChanges();
    const actions = fixture.nativeElement.querySelector('.app-sidebar-header__actions') as HTMLElement;
    const help = actions.querySelector('[aria-label="Help and feedback"]')!;
    const trigger = actions.querySelector('[aria-label="Open skill constellation"]') as HTMLButtonElement;
    expect(help.nextElementSibling?.contains(trigger)).toBe(true);
    trigger.click();
    expect(TestBed.inject(ConstellationService).visible()).toBe(true);
  });
});
