import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { AgentJobParameter } from '../../../../core/models/agent-job.model';
import { ConfirmationService } from '../../../../core/services/confirmation.service';
import { JobParametersComponent } from './job-parameters.component';

describe('JobParametersComponent', () => {
  let fixture: ComponentFixture<JobParametersComponent>;
  let component: JobParametersComponent;
  let confirm: ReturnType<typeof vi.fn>;

  const defs: AgentJobParameter[] = [
    { name: 'topics', label: 'Topics', type: 'list', format: 'bullets' },
    { name: 'region', label: 'Region', type: 'single_select', options: ['US', 'EU'] },
    { name: 'tags', label: 'Tags', type: 'multi_select', options: ['a', 'b'], format: 'comma' },
    { name: 'tone', type: 'text', description: 'How it should sound' },
  ];

  const host = () => fixture.nativeElement as HTMLElement;
  const button = (text: string) =>
    Array.from(host().querySelectorAll('button')).find(b => b.textContent?.trim() === text) as HTMLButtonElement | undefined;

  function open(parameters: AgentJobParameter[], prompt: string, values = {}): void {
    fixture = TestBed.createComponent(JobParametersComponent);
    component = fixture.componentInstance;
    fixture.componentRef.setInput('parameters', parameters);
    fixture.componentRef.setInput('prompt', prompt);
    fixture.componentRef.setInput('values', values);
    fixture.detectChanges();
  }

  beforeEach(async () => {
    confirm = vi.fn().mockResolvedValue(true);
    await TestBed.configureTestingModule({
      imports: [JobParametersComponent],
      providers: [provideZonelessChangeDetection(), { provide: ConfirmationService, useValue: { confirm } }],
    }).compileComponents();
  });

  it('shows the form by default, one field per type, with missing values marked', () => {
    open(defs, '{{topics}} {{region}} {{tags}} {{tone}}', { region: 'EU' });
    expect(host().querySelector('app-job-parameters-form')).not.toBeNull();
    expect(host().querySelector('app-job-parameters-builder')).toBeNull();
    expect(host().querySelector('app-chip-list-input')).not.toBeNull();
    expect(host().querySelector('select')).not.toBeNull();
    expect(host().querySelectorAll('ui-checkbox').length).toBe(2);
    expect(host().textContent).toContain('How it should sound');
    expect(host().textContent?.match(/Needs a value/g)?.length).toBe(3);
    expect(button('Edit parameters')).toBeDefined();
  });

  it('offers Add parameters when there are none', () => {
    open([], 'Plain prompt');
    expect(button('Add parameters')).toBeDefined();
    expect(host().textContent).toContain('No parameters');
  });

  it('emits values from the form fields, keeping option order for multi selects', () => {
    open(defs, '', { tags: ['b'] });
    const emitted: unknown[] = [];
    component.valuesChange.subscribe(v => emitted.push(v));

    const select = host().querySelector('select') as HTMLSelectElement;
    select.value = 'US';
    select.dispatchEvent(new Event('change'));
    expect(emitted.at(-1)).toEqual({ tags: ['b'], region: 'US' });

    const boxes = host().querySelectorAll('ui-checkbox input') as NodeListOf<HTMLInputElement>;
    // Before change detection catches up, the second edit must keep the first.
    boxes[0].click();
    expect(emitted.at(-1)).toEqual({ tags: ['a', 'b'], region: 'US' });
  });

  it('switches to the builder and back, with Done blocked while there are errors', () => {
    open(defs.slice(0, 1), 'News on {{topics}} and {{extra}}');
    expect(host().textContent).toContain('placeholders with no definition');

    button('Edit parameters')!.click();
    fixture.detectChanges();
    expect(host().querySelector('app-job-parameters-builder')).not.toBeNull();
    expect(button('Done')!.disabled).toBe(true);

    const emitted: AgentJobParameter[][] = [];
    component.parametersChange.subscribe(v => emitted.push(v));
    button('Add definition')!.click();
    expect(emitted.at(-1)).toEqual([...defs.slice(0, 1), { name: 'extra', label: '', type: 'text' }]);

    fixture.componentRef.setInput('parameters', emitted.at(-1));
    fixture.detectChanges();
    expect(button('Done')!.disabled).toBe(false);
    button('Done')!.click();
    fixture.detectChanges();
    expect(host().querySelector('app-job-parameters-form')).not.toBeNull();
  });

  it('notes definitions the prompt does not use without blocking Done', () => {
    open(defs.slice(0, 1), 'No placeholders');
    component.edit();
    fixture.detectChanges();
    expect(host().textContent).toContain('Not used in the prompt yet');
    expect(button('Done')!.disabled).toBe(false);
  });

  it('defaults the format when the type changes to a multi-value type', () => {
    open([{ name: 'tone', type: 'text' }], '{{tone}}');
    component.edit();
    fixture.detectChanges();
    const emitted: AgentJobParameter[][] = [];
    component.parametersChange.subscribe(v => emitted.push(v));
    const typeSelect = host().querySelector('fieldset select') as HTMLSelectElement;
    typeSelect.value = 'multi_select';
    typeSelect.dispatchEvent(new Event('change'));
    expect(emitted.at(-1)![0]).toEqual(expect.objectContaining({ type: 'multi_select', options: [], format: 'comma' }));
  });

  it('moves the value and offers to rename the placeholder when a parameter is renamed', async () => {
    open(defs.slice(0, 1), 'News on {{ topics }}', { topics: ['AI'] });
    const values: unknown[] = [];
    const prompts: string[] = [];
    component.valuesChange.subscribe(v => values.push(v));
    component.promptChange.subscribe(p => prompts.push(p));

    await component.onRenamed({ from: 'topics', to: 'subjects' });

    expect(values.at(-1)).toEqual({ subjects: ['AI'] });
    expect(confirm).toHaveBeenCalledWith(expect.objectContaining({ confirmText: 'Rename in prompt' }));
    expect(prompts.at(-1)).toBe('News on {{subjects}}');
  });
});
