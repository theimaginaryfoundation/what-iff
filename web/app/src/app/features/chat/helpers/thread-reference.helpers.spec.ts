import {
  formatThreadReferenceLine,
  formatThreadReferences,
  isUuid,
  parseThreadReferenceBlock,
  threadReferenceCountLabel,
} from './thread-reference.helpers';

const ID_A = '11111111-1111-4111-8111-111111111111';
const ID_B = '22222222-2222-4222-8222-222222222222';

describe('thread reference helpers', () => {
  it('formats one line per thread in the find_context vocabulary', () => {
    expect(formatThreadReferences([{ id: ID_A, name: 'Alpha' }])).toBe(
      `[Referenced thread "Alpha" — read it with find_context mode="conversation" target="${ID_A}"]\n`,
    );
  });

  it('returns an empty block for no threads or only non-UUID ids', () => {
    expect(formatThreadReferences([])).toBe('');
    expect(formatThreadReferences([{ id: 'not-a-uuid', name: 'Bad' }])).toBe('');
    expect(formatThreadReferences([{ id: 'x', name: 'Bad' }, { id: ID_A, name: 'Good' }])).toBe(
      `${formatThreadReferenceLine({ id: ID_A, name: 'Good' })}\n`,
    );
  });

  it('round-trips names with quotes, backslashes, newlines and brackets', () => {
    const threads = [
      { id: ID_A, name: 'He said "hi" \\ bye' },
      { id: ID_B, name: 'Line one\nLine two] [Referenced thread "x"' },
    ];
    const block = formatThreadReferences(threads);

    expect(block.split('\n')).toHaveLength(3);
    expect(parseThreadReferenceBlock(`${block}What do these say?`)).toEqual({
      references: threads,
      body: 'What do these say?',
    });
  });

  it('keeps multi-line message bodies after the block intact', () => {
    const block = formatThreadReferences([{ id: ID_A, name: 'Alpha' }]);
    expect(parseThreadReferenceBlock(`${block}first\nsecond`).body).toBe('first\nsecond');
  });

  it('leaves non-matching text untouched', () => {
    const texts = [
      'Just a message',
      '',
      `[Thread context: "Alpha"; thread_id="${ID_A}"]\nhello`,
      `[Referenced thread "Alpha" — read it with find_context mode="conversation" target="nope"]\nhello`,
      `[Referenced thread Alpha — read it with find_context mode="conversation" target="${ID_A}"]\nhello`,
      // No trailing newline: a lone line is the user's own text.
      formatThreadReferenceLine({ id: ID_A, name: 'Alpha' }),
      `hello\n${formatThreadReferenceLine({ id: ID_A, name: 'Alpha' })}\n`,
    ];
    for (const text of texts) {
      expect(parseThreadReferenceBlock(text)).toEqual({ references: [], body: text });
    }
  });

  it('stops the block at the first non-matching line', () => {
    const line = formatThreadReferenceLine({ id: ID_A, name: 'Alpha' });
    const text = `${line}\nnot a ref\n${formatThreadReferenceLine({ id: ID_B, name: 'Beta' })}\n`;
    expect(parseThreadReferenceBlock(text)).toEqual({
      references: [{ id: ID_A, name: 'Alpha' }],
      body: `not a ref\n${formatThreadReferenceLine({ id: ID_B, name: 'Beta' })}\n`,
    });
  });

  it('validates UUIDs', () => {
    expect(isUuid(ID_A)).toBe(true);
    expect(isUuid('chat-1')).toBe(false);
  });

  it('labels the attached count', () => {
    expect(threadReferenceCountLabel(1)).toBe('1 thread added');
    expect(threadReferenceCountLabel(3)).toBe('3 threads added');
  });
});
