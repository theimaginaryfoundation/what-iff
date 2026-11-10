import { AgentPromptQuestion } from '../../../../core/models/job.model';
import {
  formError,
  formFields,
  formResponse,
  initialFormValues,
  emptyDrafts,
  linkHost,
  optionLabelParts,
  promptHeading,
  questionComplete,
  questionTabLabel,
  questionsResponse,
  setOtherChosen,
  setOwnText,
  skipQuestion,
  toggleOption,
} from './agent-prompt-card.helpers';

const single: AgentPromptQuestion = { id: 'q1', question: 'Which?', options: [{ label: 'A' }, { label: 'B' }] };
const multi: AgentPromptQuestion = { ...single, id: 'q2', multi_select: true };

describe('agent prompt card helpers', () => {
  it('single-select replaces the pick, and "Other" takes its place without losing typed text', () => {
    let d = toggleOption({ selected: [], text: '', other: false }, single, 'A');
    expect(d).toEqual({ selected: ['A'], text: '', other: false });
    d = toggleOption(d, single, 'B');
    expect(d.selected).toEqual(['B']);
    d = setOwnText(setOtherChosen(d, single, true), 'something else');
    expect(d).toEqual({ selected: [], text: 'something else', other: true });
    d = toggleOption(d, single, 'A');
    expect(d).toEqual({ selected: ['A'], text: 'something else', other: false });
    expect(questionsResponse({ q1: d }).answers).toEqual({ q1: { selected: ['A'] } });
    expect(setOtherChosen(d, single, true).text).toBe('something else');
  });

  it('multi-select adds and removes, with "Other" alongside', () => {
    let d = toggleOption({ selected: [], text: '', other: false }, multi, 'A');
    d = toggleOption(d, multi, 'B');
    expect(d.selected).toEqual(['A', 'B']);
    d = toggleOption(d, multi, 'A');
    expect(d.selected).toEqual(['B']);
    d = setOwnText(setOtherChosen(d, multi, true), 'and C');
    expect(d).toEqual({ selected: ['B'], text: 'and C', other: true });
  });

  it('opens a required open question\'s box from the start, but not an optional one\'s', () => {
    const drafts = emptyDrafts([
      { id: 'q1', question: 'When?' },
      { id: 'q2', question: 'Anything else?', optional: true },
      { ...single, id: 'q3' },
    ]);
    expect(drafts['q1'].other).toBe(true);
    expect(drafts['q2'].other).toBe(false);
    expect(drafts['q3'].other).toBe(false);
  });

  it('holds sending only on required questions', () => {
    const optional: AgentPromptQuestion = { id: 'q9', question: 'Anything else?', optional: true };
    expect(questionComplete(single, { selected: [], text: '', other: false })).toBe(false);
    expect(questionComplete(single, { selected: [], text: '  ', other: true })).toBe(false);
    expect(questionComplete(single, { selected: [], text: 'mine', other: true })).toBe(true);
    expect(questionComplete(optional, { selected: [], text: '', other: true })).toBe(true);
    expect(skipQuestion({ selected: ['A'], text: 'x', other: true })).toEqual({ selected: [], text: 'x', other: false });
  });

  it('builds the answer, leaving out unanswered questions and text from a closed box', () => {
    expect(questionsResponse({
      q1: { selected: ['A'], text: 'stale', other: false },
      q2: { selected: [], text: '  ', other: true },
      q3: { selected: [], text: ' hi ', other: true },
      q4: { selected: ['B'], text: ' also ', other: true },
    })).toEqual({
      action: 'accept',
      answers: { q1: { selected: ['A'] }, q3: { text: 'hi' }, q4: { selected: ['B'], text: 'also' } },
    });
  });

  it('names tabs and reads the agent\'s recommendation off a label', () => {
    expect(questionTabLabel({ id: 'q1', question: 'Which?', header: ' Format ' }, 0)).toBe('Format');
    expect(questionTabLabel({ id: 'q2', question: 'Which?' }, 1)).toBe('Question 2');
    expect(optionLabelParts('PDF (Recommended)')).toEqual({ text: 'PDF', recommended: true });
    expect(optionLabelParts('PDF (recommended) ')).toEqual({ text: 'PDF', recommended: true });
    expect(optionLabelParts('PDF')).toEqual({ text: 'PDF', recommended: false });
    expect(optionLabelParts('(Recommended)')).toEqual({ text: '(Recommended)', recommended: false });
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
