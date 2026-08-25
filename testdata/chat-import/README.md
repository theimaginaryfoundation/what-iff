# Chat import test fixtures

Generate a large synthetic `conversations.json` for manual chunking tests:

```bash
go run ./cmd/generate-test-conversations -out testdata/chat-import/large-conversations.json -target-mb 150
```

Options:

- `-format openai` (default) or `anthropic`
- `-threads N` — override auto thread count
- `-msgs-per-thread 12` — messages per conversation
- `-lorem-repeats 80` — padding per message body

The generated file is gitignored; re-run the command when you need a fresh fixture.
