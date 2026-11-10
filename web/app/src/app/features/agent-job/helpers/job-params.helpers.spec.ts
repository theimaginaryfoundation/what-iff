import { AgentJobParameter } from '../../../core/models/agent-job.model';
import {
  analyzeParameters,
  cleanParameterValues,
  hasParameterErrors,
  joinNames,
  missingParameters,
  normalizeDefinition,
  promptPlaceholders,
  renamePlaceholder,
  sameDefinitions,
  sameValues,
} from './job-params.helpers';

const defs: AgentJobParameter[] = [
  { name: 'topics', type: 'list' },
  { name: 'region', type: 'single_select', options: ['US', 'EU'] },
  { name: 'tags', type: 'multi_select', options: ['a', 'b'] },
  { name: 'tone', type: 'text' },
];

describe('job params helpers', () => {
  it('finds distinct placeholders in order, allowing spaces inside the braces', () => {
    expect(promptPlaceholders('{{topics}} in {{ region }}; {{topics}} {{1bad}} {single}')).toEqual(['topics', 'region']);
  });

  it('reports undefined placeholders as errors and unused definitions as notes', () => {
    const analysis = analyzeParameters('News on {{topics}} and {{extra}}', defs);
    expect(analysis.undefinedPlaceholders).toEqual(['extra']);
    expect(analysis.unusedDefinitions).toEqual(['region', 'tags', 'tone']);
    expect(hasParameterErrors(analysis)).toBe(true);
    expect(hasParameterErrors(analyzeParameters('{{topics}}', defs))).toBe(false);
  });

  it('flags bad names, duplicates and selects without options', () => {
    const analysis = analyzeParameters('', [
      { name: '1x', type: 'text' },
      { name: 'a', type: 'text' },
      { name: 'a', type: 'list' },
      { name: 'b', type: 'single_select', options: [' '] },
    ]);
    expect(analysis.problems.map(p => p.index)).toEqual([0, 2, 3]);
  });

  it('lists missing values in definition order', () => {
    expect(missingParameters(defs, { region: 'EU', tags: ['a', 'gone'], tone: '  ' })).toEqual(['topics', 'tags', 'tone']);
    expect(missingParameters(defs, { topics: ['AI'], region: 'EU', tags: ['b'], tone: 'dry' })).toEqual([]);
  });

  it('cleans values the way the server stores them', () => {
    expect(cleanParameterValues(defs, {
      topics: [' AI ', '', 'climate'],
      region: 'Mars',
      tags: ['b', 'a'],
      unknown: 'x',
    })).toEqual({ topics: ['AI', 'climate'], tags: ['a', 'b'] });
  });

  it('normalises definitions like the server (label, options, format)', () => {
    expect(normalizeDefinition({ name: ' tags ', type: 'multi_select', options: ['a', ' a', 'b', ''] })).toEqual({
      name: 'tags', label: 'tags', type: 'multi_select', options: ['a', 'b'], format: 'comma',
    });
    expect(normalizeDefinition({ name: 'topics', type: 'list' }).format).toBe('bullets');
    expect(normalizeDefinition({ name: 'tone', type: 'text', format: 'comma' }).format).toBeUndefined();
    expect(sameDefinitions([{ name: 'tone', type: 'text' }], [{ name: 'tone', label: 'tone', type: 'text' }])).toBe(true);
  });

  it('renames placeholders, leaving other names alone', () => {
    expect(renamePlaceholder('{{ topic }} and {{topics}} and {{topic}}', 'topic', 'subject')).toBe('{{subject}} and {{topics}} and {{subject}}');
  });

  it('compares values ignoring key order', () => {
    expect(sameValues({ a: 'x', b: ['y'] }, { b: ['y'], a: 'x' })).toBe(true);
    expect(sameValues({ a: 'x' }, { a: 'y' })).toBe(false);
  });

  it('joins names for messages', () => {
    expect(joinNames(['Topics'])).toBe('Topics');
    expect(joinNames(['Topics', 'Region'])).toBe('Topics and Region');
    expect(joinNames(['A', 'B', 'C'])).toBe('A, B and C');
  });
});
