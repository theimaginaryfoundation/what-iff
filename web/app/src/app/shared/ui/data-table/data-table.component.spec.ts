import { Component, provideZonelessChangeDetection, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';

import { SelectionState } from '../helpers/selection-model';
import { CheckboxComponent } from '../checkbox/checkbox.component';
import { DataTableComponent } from './data-table.component';

@Component({
  standalone: true,
  imports: [DataTableComponent],
  template: `
    <ui-data-table
      [message]="message()"
      [messageTone]="tone()"
      [selectAllState]="state()"
      selectAllLabel="Select all rows"
      selectAllTooltip="Select every row shown"
      bodyLabel="Rows"
      (selectAllChange)="toggles = toggles + 1"
    >
      <ng-container ngProjectAs="[uiTableHead]">
        <th scope="col">NAME</th>
        <th scope="col">WHEN</th>
      </ng-container>
      @for (row of rows(); track row) {
        <tr><td></td><td>{{ row }}</td><td></td></tr>
      }
    </ui-data-table>
  `,
})
class HostComponent {
  readonly message = signal<string | null>(null);
  readonly tone = signal<'muted' | 'error'>('muted');
  readonly state = signal<SelectionState>('none');
  readonly rows = signal(['one', 'two']);
  toggles = 0;
}

describe('DataTableComponent', () => {
  const setup = () => {
    TestBed.configureTestingModule({ imports: [HostComponent], providers: [provideZonelessChangeDetection()] });
    const fixture = TestBed.createComponent(HostComponent);
    fixture.detectChanges();
    return { fixture, host: fixture.componentInstance, el: fixture.nativeElement as HTMLElement };
  };

  it('puts the select-all column first, then the projected header cells and rows', () => {
    const { el } = setup();
    const heads = Array.from(el.querySelectorAll('thead th')).map(th => th.textContent?.trim());
    expect(heads).toEqual(['', 'NAME', 'WHEN']);
    expect(el.querySelector('tbody')?.getAttribute('aria-label')).toBe('Rows');
    expect(el.querySelectorAll('tbody tr')).toHaveLength(2);
  });

  it('reflects none, some and all on the select-all checkbox and reports a toggle', () => {
    const { fixture, host, el } = setup();
    const box = el.querySelector('.ui-data-table__select-all') as HTMLInputElement;
    expect(box.getAttribute('aria-label')).toBe('Select all rows');
    expect(box.checked).toBe(false);

    host.state.set('some');
    fixture.detectChanges();
    expect(box.indeterminate).toBe(true);

    host.state.set('all');
    fixture.detectChanges();
    expect(box.checked).toBe(true);
    expect(box.indeterminate).toBe(false);

    box.click();
    expect(host.toggles).toBe(1);
  });

  it('passes the select-all tooltip to the shared checkbox', () => {
    const { fixture } = setup();
    const checkbox = fixture.debugElement.query(By.directive(CheckboxComponent)).componentInstance as CheckboxComponent;
    expect(checkbox.tooltip()).toBe('Select every row shown');
  });

  it('shows a message instead of the table, as an alert when it is an error', () => {
    const { fixture, host, el } = setup();
    host.message.set('Nothing here');
    fixture.detectChanges();
    expect(el.querySelector('table')?.hidden).toBe(true);
    expect(el.querySelector('.ui-data-table__message')?.textContent?.trim()).toBe('Nothing here');
    expect(el.querySelector('.ui-data-table__message')?.getAttribute('role')).toBeNull();

    host.tone.set('error');
    fixture.detectChanges();
    expect(el.querySelector('.ui-data-table__message')?.getAttribute('role')).toBe('alert');

    host.message.set(null);
    fixture.detectChanges();
    expect(el.querySelector('table')?.hidden).toBe(false);
    expect(el.querySelectorAll('tbody tr')).toHaveLength(2);
  });
});
