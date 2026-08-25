import { ChangeDetectionStrategy, Component, ElementRef, OnDestroy, OnInit, inject, input } from '@angular/core';
import { TabsComponent } from './tabs.component';
import { isActivationKey } from '../helpers/keyboard.helpers';

@Component({
  selector: 'ui-tab',
  standalone: true,
  templateUrl: './tab.component.html',
  styleUrl: './tab.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TabComponent implements OnInit, OnDestroy {
  readonly id = input.required<string>();
  readonly controls = input.required<string>();
  readonly disabled = input(false);
  private readonly tabs = inject(TabsComponent, { host: true });
  private readonly elementRef = inject<ElementRef<HTMLElement>>(ElementRef);

  ngOnInit(): void {
    const button = this.elementRef.nativeElement.querySelector('button');
    if (button) this.tabs.registerTab(this.id(), button);
  }

  ngOnDestroy(): void { this.tabs.unregisterTab(this.id()); }
  isSelected(): boolean { return this.tabs.isSelected(this.id()); }
  activate(): void { if (!this.disabled()) this.tabs.activate(this.id()); }

  onKeydown(event: KeyboardEvent): void {
    if (event.key === 'ArrowRight') { event.preventDefault(); this.tabs.focusRelative(this.id(), 1); }
    else if (event.key === 'ArrowLeft') { event.preventDefault(); this.tabs.focusRelative(this.id(), -1); }
    else if (event.key === 'Home') { event.preventDefault(); this.tabs.focusFirst(); }
    else if (event.key === 'End') { event.preventDefault(); this.tabs.focusLast(); }
    else if (isActivationKey(event)) { event.preventDefault(); this.activate(); }
  }
}
