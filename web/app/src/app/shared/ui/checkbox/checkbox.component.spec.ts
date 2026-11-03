import { ChangeDetectionStrategy, Component, provideZonelessChangeDetection, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { FormControl, FormsModule, ReactiveFormsModule } from '@angular/forms';
import { By } from '@angular/platform-browser';

import { TooltipDirective } from '../tooltip/tooltip.directive';
import { CheckboxComponent } from './checkbox.component';

@Component({
  standalone: true,
  imports: [CheckboxComponent],
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <label>
      <ui-checkbox
        [checked]="checked()"
        [indeterminate]="indeterminate()"
        [disabled]="disabled()"
        ariaLabel="Select row"
        inputClass="row__select"
        testId="row-select"
        tooltip="Pick this row"
        (change)="changes.push($any($event.target).checked)"
        (checkedChange)="values.push($event)"
      />
      Row
    </label>
  `,
})
class BoundHostComponent {
  readonly checked = signal(false);
  readonly indeterminate = signal(false);
  readonly disabled = signal(false);
  readonly changes: boolean[] = [];
  readonly values: boolean[] = [];
}

@Component({
  standalone: true,
  imports: [CheckboxComponent, FormsModule, ReactiveFormsModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <ui-checkbox class="model" [(ngModel)]="agreed" ariaLabel="Agree" />
    <ui-checkbox class="control" [formControl]="control" ariaLabel="Remember" />
  `,
})
class FormHostComponent {
  agreed = false;
  readonly control = new FormControl(true, { nonNullable: true });
}

describe('CheckboxComponent', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection()] });
  });

  const bound = () => {
    const fixture = TestBed.createComponent(BoundHostComponent);
    fixture.detectChanges();
    const box = fixture.nativeElement.querySelector('input[type="checkbox"]') as HTMLInputElement;
    return { fixture, host: fixture.componentInstance, box };
  };

  it('renders one native checkbox carrying the label, class hook and test id', () => {
    const { fixture, box } = bound();
    expect(fixture.nativeElement.querySelectorAll('input[type="checkbox"]')).toHaveLength(1);
    expect(box.getAttribute('aria-label')).toBe('Select row');
    expect(box.getAttribute('data-testid')).toBe('row-select');
    expect(box.classList).toContain('ui-checkbox');
    expect(box.classList).toContain('row__select');
    // The name lives on the input only, so getByLabel / getByRole find exactly one element.
    expect(fixture.nativeElement.querySelector('ui-checkbox').getAttribute('aria-label')).toBeNull();
  });

  it('puts the tooltip on the input, so it shows on keyboard focus and describes the box', () => {
    const { fixture, box } = bound();
    const tooltip = fixture.debugElement.query(By.directive(TooltipDirective));
    expect(tooltip.nativeElement).toBe(box);
    expect(tooltip.injector.get(TooltipDirective).uiTooltip()).toBe('Pick this row');
  });

  it('reflects checked, indeterminate and disabled', () => {
    const { fixture, host, box } = bound();
    expect(box.checked).toBe(false);

    host.checked.set(true);
    fixture.detectChanges();
    expect(box.checked).toBe(true);

    host.indeterminate.set(true);
    fixture.detectChanges();
    expect(box.indeterminate).toBe(true);

    host.disabled.set(true);
    fixture.detectChanges();
    expect(box.disabled).toBe(true);
  });

  it('emits one change (target = the input) and checkedChange per click, also from the wrapping label', () => {
    const { fixture, host, box } = bound();
    box.click();
    expect(host.changes).toEqual([true]);
    expect(host.values).toEqual([true]);

    (fixture.nativeElement.querySelector('label') as HTMLLabelElement).click();
    expect(host.changes).toEqual([true, false]);
    expect(host.values).toEqual([true, false]);
  });

  it('reports a change dispatched directly on the input (a non-bubbling event)', () => {
    const { host, box } = bound();
    box.checked = true;
    box.dispatchEvent(new Event('change'));
    expect(host.changes).toEqual([true]);
  });

  it('works with ngModel and reactive forms', async () => {
    const fixture = TestBed.createComponent(FormHostComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    const model = fixture.nativeElement.querySelector('.model input') as HTMLInputElement;
    const control = fixture.nativeElement.querySelector('.control input') as HTMLInputElement;
    expect(model.checked).toBe(false);
    expect(control.checked).toBe(true);

    model.click();
    control.click();
    expect(fixture.componentInstance.agreed).toBe(true);
    expect(fixture.componentInstance.control.value).toBe(false);

    fixture.componentInstance.control.setValue(true);
    fixture.componentInstance.control.disable();
    fixture.detectChanges();
    expect(control.checked).toBe(true);
    expect(control.disabled).toBe(true);
  });
});
