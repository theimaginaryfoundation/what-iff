import { uniqueId } from './unique';

/** One `<input type="file">`-ready payload, matching `ChatImportModal.chooseFile`. */
export interface ChatgptExportFile {
  name: string;
  mimeType: string;
  buffer: Buffer;
}

export interface ChatgptExport {
  /** Titles assigned to each conversation, in file order — becomes each imported thread's name. */
  titles: string[];
  file: ChatgptExportFile;
}

/**
 * Builds a minimal-but-valid ChatGPT `conversations.json` export (the format
 * `migration.SplitConversationArchiveFromReader` on the backend expects: a JSON array of
 * conversations, each with a `mapping` of message nodes reachable from `current_node` via
 * `parent` links) with one user + one assistant message per conversation.
 *
 * Every conversation gets a fresh UUID for `conversation_id` so the backend's
 * (owner_id, import_hash) dedup index never collides across runs/workers — each test seeds its
 * own account, but reusing a fixed id here would still risk a same-run dedup skip if a spec
 * imports the same export twice.
 */
export function buildChatgptExport(count: number, label = 'e2e-import'): ChatgptExport {
  const titles = Array.from({ length: count }, (_, i) => `${label}-${uniqueId()}-${i}`);

  const conversations = titles.map((title, i) => {
    const baseTime = 1700000000 + i;
    return {
      conversation_id: uniqueId(),
      id: uniqueId(),
      title,
      create_time: baseTime,
      current_node: 'n2',
      mapping: {
        n1: {
          id: 'n1',
          message: {
            author: { role: 'user' },
            create_time: baseTime,
            content: { content_type: 'text', parts: [`Hello from ${title}`] },
          },
          parent: null,
          children: ['n2'],
        },
        n2: {
          id: 'n2',
          message: {
            author: { role: 'assistant' },
            create_time: baseTime + 1,
            content: { content_type: 'text', parts: [`Hi! This is a reply for ${title}.`] },
          },
          parent: 'n1',
          children: [],
        },
      },
    };
  });

  return {
    titles,
    file: {
      name: 'conversations.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify(conversations)),
    },
  };
}
