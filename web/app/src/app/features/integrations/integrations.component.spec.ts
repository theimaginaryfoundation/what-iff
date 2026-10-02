import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { BehaviorSubject, of } from 'rxjs';

import { AccessGate } from '../../core/services/access-gate';
import { IntegrationTab, IntegrationTabSource } from '../../core/services/integration-tab-source';
import { IntegrationsComponent } from './integrations.component';
import { IntegrationsConnectorsTabComponent } from './integrations-connectors-tab.component';
import { IntegrationsWebhooksTabComponent } from './integrations-webhooks-tab.component';

@Component({ selector: 'app-test-extra-tab', standalone: true, template: '<p class="extra-content">extra tab body</p>' })
class ExtraTabComponent {}

@Component({ selector: 'app-integrations-connectors-tab', standalone: true, template: '' })
class ConnectorsStubComponent {}

@Component({ selector: 'app-integrations-webhooks-tab', standalone: true, template: '' })
class WebhooksStubComponent {}

describe('IntegrationsComponent contributed tabs', () => {
  let tabs$: BehaviorSubject<IntegrationTab[]>;

  function create(access = true) {
    tabs$ = new BehaviorSubject<IntegrationTab[]>([
      { id: 'extra', label: 'Extra', tooltip: 'An extra tab', component: ExtraTabComponent },
    ]);
    TestBed.configureTestingModule({
      imports: [IntegrationsComponent],
      providers: [
        provideRouter([]),
        { provide: AccessGate, useValue: { hasAccess: () => of(access) } },
        { provide: IntegrationTabSource, useValue: { tabs: () => tabs$ } },
      ],
    }).overrideComponent(IntegrationsComponent, {
      remove: { imports: [IntegrationsConnectorsTabComponent, IntegrationsWebhooksTabComponent] },
      add: { imports: [ConnectorsStubComponent, WebhooksStubComponent] },
    });
    const fixture = TestBed.createComponent(IntegrationsComponent);
    fixture.detectChanges();
    return fixture;
  }

  function tabButton(el: HTMLElement, label: string): HTMLButtonElement | undefined {
    return Array.from(el.querySelectorAll('nav button')).find((b) => b.textContent?.trim() === label) as
      | HTMLButtonElement
      | undefined;
  }

  it('shows a contributed tab and renders its component when selected', () => {
    const fixture = create();
    const el = fixture.nativeElement as HTMLElement;

    expect(tabButton(el, 'Extra')).toBeTruthy();
    expect(el.querySelector('.extra-content')).toBeNull();

    tabButton(el, 'Extra')!.click();
    fixture.detectChanges();

    expect(el.querySelector('.extra-content')?.textContent).toContain('extra tab body');
  });

  it('falls back to connectors when the active contributed tab goes away', () => {
    const fixture = create();
    fixture.componentInstance.setActiveTab('extra');

    tabs$.next([]);
    fixture.detectChanges();

    expect(fixture.componentInstance.activeTab()).toBe('connectors');
    expect(tabButton(fixture.nativeElement, 'Extra')).toBeUndefined();
  });

  it('hides contributed tabs without access', () => {
    const fixture = create(false);
    expect(tabButton(fixture.nativeElement, 'Extra')).toBeUndefined();
  });
});
