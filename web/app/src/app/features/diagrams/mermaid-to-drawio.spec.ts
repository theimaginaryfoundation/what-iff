import { describe, expect, it } from 'vitest';

import { mermaidFileName } from './mermaid-to-drawio';

describe('mermaidFileName', () => {
  it('names the file after the diagram’s title, or its kind', () => {
    expect(mermaidFileName('---\ntitle: Sign-up flow\n---\nflowchart LR\n A --> B')).toBe('Sign-up flow.drawio');
    expect(mermaidFileName('%% a note\nsequenceDiagram\n A->>B: hi')).toBe('sequenceDiagram.drawio');
    expect(mermaidFileName('')).toBe('diagram.drawio');
  });
});
