import { AgentPromptQuestion } from '../../../../core/models/job.model';
import {
  formError,
  formFields,
  formResponse,
  initialFormValues,
  linkHost,
  promptHeading,
  questionsResponse,
  setOwnText,
  toggleOption,
} from './agent-prompt-card.helpers';

const single: AgentPromptQuestion = { id: 'q1', question: 'Which?', options: [{ label: 'A' }, { label: 'B' }] };
const multi: AgentPromptQuestion = { ...single, id: 'q2', multi_select: true };

describe('agent prompt card helpers', () => {
  it('single-select replaces the pick and own text replaces it', () => {
    let d = toggleOption({ selected: [], text: 'x' }, single, 'A');
    expect(d).toEqual({ selected: ['A'], text: '' });
    d = toggleOption(d, single, 'B');
    expect(d.selected).toEqual(['B']);
    d = toggleOption(d, single, 'B');
    expect(d.selected).toEqual([]);
    d = setOwnText({ selected: ['A'], text: '' }, single, 'something else');
    expect(d).toEqual({ selected: [], text: 'something else' });
  });

  it('multi-select adds and removes', () => {
    let d = toggleOption({ selected: [], text: '' }, multi, 'A');
    d = toggleOption(d, multi, 'B');
    expect(d.selected).toEqual(['A', 'B']);
    d = toggleOption(d, multi, 'A');
    expect(d.selected).toEqual(['B']);
    expect(setOwnText(d, multi, 'and C').selected).toEqual(['B']);
  });

  it('builds the answer, leaving unanswered questions out', () => {
    expect(questionsResponse({ q1: { selected: ['A'], text: '' }, q2: { selected: [], text: '  ' }, q3: { selected: [], text: ' hi ' } })).toEqual({
      action: 'accept',
      answers: { q1: { selected: ['A'] }, q3: { text: 'hi' } },
    });
  });

  it('turns an elicitation schema into fields with defaults and validation', () => {
    const fields = formFields({
      type: 'object',
      properties: {
        name: { type: 'string', title: 'Name', minLength: 2 },
        age: { type: 'integer', minimum: 0 },
        color: { type: 'string', oneOf: [{ const: '#f00', title: 'Red' }, { const: '#0f0', title: 'Green' }], default: '#0f0' },
        legacy: { type: 'string', enum: ['s', 'm'], enumNames: ['Small', 'Medium'] },
        tags: { type: 'array', items: { type: 'string', enum: ['x', 'y'] } },
        ok: { type: 'boolean' },
        nested: { type: 'object' as never },
      },
      required: ['name', 'age'],
    });
    expect(fields.map(f => [f.name, f.control])).toEqual([
      ['name', 'text'],
      ['age', 'number'],
      ['color', 'select'],
      ['legacy', 'select'],
      ['tags', 'multi'],
      ['ok', 'checkbox'],
    ]);
    expect(fields[3].choices[1]).toEqual({ value: 'm', label: 'Medium' });
    const values = initialFormValues(fields);
    expect(values).toEqual({ color: '#0f0', tags: [], ok: false });
    expect(formError(fields, values)).toBe('Name is required.');
    expect(formError(fields, { ...values, name: 'A', age: '3' })).toBe('Name must be at least 2 characters.');
    expect(formError(fields, { ...values, name: 'Al', age: '3.5' })).toBe('age must be a whole number.');
    expect(formError(fields, { ...values, name: 'Al', age: '3' })).toBeNull();
    expect(formResponse(fields, { ...values, name: 'Al', age: '3' })).toEqual({
      action: 'accept',
      content: { name: 'Al', age: 3, color: '#0f0', ok: false },
    });
  });

  it('only shows http(s) link hosts', () => {
    expect(linkHost('https://mcp.example.com/connect?x=1')).toBe('mcp.example.com');
    expect(linkHost('javascript:alert(1)')).toBeNull();
    expect(linkHost('not a url')).toBeNull();
  });

  it('names who is asking', () => {
    expect(promptHeading({ kind: 'approval' }, 'agent')).toBe('Approval needed');
    expect(promptHeading({ kind: 'form', server_name: 'GitHub' }, 'mcp_elicitation')).toBe('GitHub is asking');
    expect(promptHeading({ kind: 'questions', questions: [single, multi] }, 'agent')).toBe('A few questions');
  });
});
