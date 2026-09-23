import {
  formatToolPayload,
  friendlyToolName,
  parseToolPayload,
  summarizeToolCall,
  summarizeToolInput,
  summarizeToolOutput,
} from './tool-call-format.helpers';

describe('tool-call format helpers', () => {
  describe('parseToolPayload', () => {
    it('parses JSON objects and arrays', () => {
      expect(parseToolPayload('{"a":1}')).toEqual({ a: 1 });
      expect(parseToolPayload(' [1,2] ')).toEqual([1, 2]);
    });

    it('unwraps JSON that was encoded twice', () => {
      expect(parseToolPayload(JSON.stringify('{"a":1}'))).toEqual({ a: 1 });
    });

    it('returns undefined for plain text and malformed JSON', () => {
      expect(parseToolPayload('Retrieved 3 memories')).toBeUndefined();
      expect(parseToolPayload('{not json')).toBeUndefined();
      expect(parseToolPayload('')).toBeUndefined();
      expect(parseToolPayload(undefined)).toBeUndefined();
    });
  });

  describe('formatToolPayload', () => {
    it('pretty-prints JSON', () => {
      expect(formatToolPayload('{"a":1,"b":[true]}')).toBe('{\n  "a": 1,\n  "b": [\n    true\n  ]\n}');
    });

    it('passes plain text through trimmed', () => {
      expect(formatToolPayload('  hello\nworld  ')).toBe('hello\nworld');
    });

    it('shows a JSON string value as its text', () => {
      expect(formatToolPayload('"just text"')).toBe('just text');
    });
  });

  describe('summarizeToolInput', () => {
    it('prefers a descriptive argument', () => {
      expect(summarizeToolInput('{"limit":5,"query":"red fox"}')).toBe('"red fox"');
      expect(summarizeToolInput('{"url":"https://example.com"}')).toBe('"https://example.com"');
    });

    it('falls back to the first scalar arguments', () => {
      expect(summarizeToolInput('{"limit":5,"tag":"work","nested":{"x":1},"extra":true}')).toBe('limit: 5, tag: work');
    });

    it('is empty for an argument-less call', () => {
      expect(summarizeToolInput('{}')).toBe('');
    });

    it('clips long values', () => {
      const summary = summarizeToolInput(JSON.stringify({ query: 'x'.repeat(300) }));
      expect(summary.length).toBeLessThanOrEqual(96);
      expect(summary.endsWith('…')).toBe(true);
    });

    it('uses plain-text input as-is', () => {
      expect(summarizeToolInput('look this up')).toBe('look this up');
    });
  });

  describe('summarizeToolOutput', () => {
    it('counts array results', () => {
      expect(summarizeToolOutput('[{},{},{}]')).toBe('3 results');
      expect(summarizeToolOutput('[{}]')).toBe('1 result');
    });

    it('counts the first list field of an object', () => {
      expect(summarizeToolOutput('{"memories":[{},{}],"total":2}')).toBe('2 memories');
      expect(summarizeToolOutput('{"memories":[{}]}')).toBe('1 memory');
      expect(summarizeToolOutput('{"search_results":[]}')).toBe('0 search results');
    });

    it('uses a message-like field when there is no list', () => {
      expect(summarizeToolOutput('{"status":"saved","id":"m-1"}')).toBe('saved');
    });

    it('says nothing for a truncated JSON preview instead of echoing a fragment', () => {
      expect(summarizeToolOutput('{"results":[{"id":"a","text":"long…')).toBe('');
      expect(summarizeToolOutput('[{"id":1},{"id":…')).toBe('');
    });

    it('uses the first non-empty line of a text result', () => {
      expect(summarizeToolOutput('\nRetrieved memories: foxes\nmore')).toBe('Retrieved memories: foxes');
    });
  });

  describe('summarizeToolCall', () => {
    const call = { tool_input: '{"query":"red fox"}', tool_output: '[1,2,3]', tool_error: 'timeout' };

    it('joins what the call did with what came back', () => {
      expect(summarizeToolCall(call, 'complete')).toBe('"red fox" · 3 results');
    });

    it('shows only the arguments while running', () => {
      expect(summarizeToolCall(call, 'running')).toBe('"red fox"');
    });

    it('uses the error text for a failed call', () => {
      expect(summarizeToolCall(call, 'error')).toBe('"red fox" · timeout');
    });

    it('falls back when there is nothing to summarize', () => {
      expect(summarizeToolCall({ tool_input: '{}', tool_output: '' }, 'complete')).toBe('No summary');
    });
  });

  it('friendlyToolName title-cases snake and kebab names', () => {
    expect(friendlyToolName('recall_memories')).toBe('Recall Memories');
    expect(friendlyToolName('web-search')).toBe('Web Search');
  });
});
