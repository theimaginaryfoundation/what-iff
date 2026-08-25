import { Component, signal, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TabComponent } from './tab.component';
import { TabPanelComponent } from './tab-panel.component';
import { TabsComponent } from './tabs.component';

@Component({
  standalone: true,
  imports: [TabsComponent, TabComponent, TabPanelComponent],
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <ui-tabs ariaLabel="Example tabs">
      <ui-tab id="one" controls="panel-one">One</ui-tab>
      <ui-tab id="two" controls="panel-two">Two</ui-tab>
      <ui-tab-panel id="panel-one" labelledBy="one">Panel one</ui-tab-panel>
      <ui-tab-panel id="panel-two" labelledBy="two">Panel two</ui-tab-panel>
    </ui-tabs>
  `,
})
class TabsHostComponent {}

@Component({
  standalone: true,
  imports: [TabsComponent, TabComponent, TabPanelComponent],
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <ui-tabs ariaLabel="Controlled tabs" [activeId]="activeId()" (activeIdChange)="activeId.set($event)">
      <ui-tab id="one" controls="panel-one">One</ui-tab>
      <ui-tab id="two" controls="panel-two">Two</ui-tab>
      <ui-tab-panel id="panel-one" labelledBy="one">Panel one</ui-tab-panel>
      <ui-tab-panel id="panel-two" labelledBy="two">Panel two</ui-tab-panel>
    </ui-tabs>
  `,
})
class ControlledTabsHostComponent {
  readonly activeId = signal('one');
}

describe('TabsComponent', () => {
  let fixture: ComponentFixture<TabsHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [TabsHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(TabsHostComponent);
    fixture.detectChanges();
  });

  it('renders aria tab roles and activates tabs', () => {
    const tabs = fixture.nativeElement.querySelectorAll('[role="tab"]') as NodeListOf<HTMLButtonElement>;
    expect(tabs[0].getAttribute('aria-selected')).toBe('true');
    expect(fixture.nativeElement.textContent).toContain('Panel one');

    tabs[1].click();
    fixture.detectChanges();

    expect(tabs[1].getAttribute('aria-selected')).toBe('true');
    expect(fixture.nativeElement.textContent).toContain('Panel two');
  });

  it('moves focus with arrow keys', () => {
    const tabs = fixture.nativeElement.querySelectorAll('[role="tab"]') as NodeListOf<HTMLButtonElement>;
    tabs[0].focus();
    tabs[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));

    expect(document.activeElement).toBe(tabs[1]);
  });

  it('updates selected tab when the activeId input changes', () => {
    const controlledFixture = TestBed.createComponent(ControlledTabsHostComponent);
    controlledFixture.detectChanges();
    const tabs = controlledFixture.nativeElement.querySelectorAll('[role="tab"]') as NodeListOf<HTMLButtonElement>;

    expect(tabs[0].getAttribute('aria-selected')).toBe('true');

    controlledFixture.componentInstance.activeId.set('two');
    controlledFixture.detectChanges();

    expect(tabs[1].getAttribute('aria-selected')).toBe('true');
    expect(controlledFixture.nativeElement.textContent).toContain('Panel two');
  });
});
