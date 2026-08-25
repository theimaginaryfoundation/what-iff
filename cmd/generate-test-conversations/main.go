package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Generates a synthetic conversations.json export for stress-testing chat import chunking.
// Defaults target ~150 MB with many OpenAI-shaped threads filled with lorem ipsum.

const lorem = `Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. `

type openAIExport struct {
	ID          string                `json:"id"`
	Title       string                `json:"title"`
	CreateTime  float64               `json:"create_time"`
	CurrentNode string                `json:"current_node"`
	Mapping     map[string]openAINode `json:"mapping"`
}

type openAINode struct {
	ID       string         `json:"id"`
	Message  *openAIMessage `json:"message"`
	Parent   *string        `json:"parent"`
	Children []string       `json:"children"`
}

type openAIMessage struct {
	Author     openAIAuthor  `json:"author"`
	CreateTime float64       `json:"create_time"`
	Content    openAIContent `json:"content"`
}

type openAIAuthor struct {
	Role string `json:"role"`
}

type openAIContent struct {
	ContentType string   `json:"content_type"`
	Parts       []string `json:"parts"`
}

type anthropicExport struct {
	UUID         string             `json:"uuid"`
	Name         string             `json:"name"`
	CreatedAt    time.Time          `json:"created_at"`
	ChatMessages []anthropicMessage `json:"chat_messages"`
}

type anthropicMessage struct {
	UUID      string              `json:"uuid"`
	Sender    string              `json:"sender"`
	Text      string              `json:"text"`
	CreatedAt time.Time           `json:"created_at"`
	Content   []map[string]string `json:"content"`
}

func main() {
	var (
		outPath       string
		targetMB      int
		format        string
		threads       int
		msgsPerThread int
		loremRepeats  int
	)

	flag.StringVar(&outPath, "out", "testdata/chat-import/large-conversations.json", "Output path")
	flag.IntVar(&targetMB, "target-mb", 150, "Approximate output size in megabytes")
	flag.StringVar(&format, "format", "openai", "Export shape: openai or anthropic")
	flag.IntVar(&threads, "threads", 0, "Thread count (0 = auto from target size)")
	flag.IntVar(&msgsPerThread, "msgs-per-thread", 12, "User/assistant pairs per thread")
	flag.IntVar(&loremRepeats, "lorem-repeats", 80, "How many lorem blocks per message body")
	flag.Parse()

	if err := os.MkdirAll(dirOf(outPath), 0o755); err != nil {
		fatal(err)
	}

	body := strings.Repeat(lorem, loremRepeats)
	if threads <= 0 {
		threads = estimateThreadCount(targetMB, format, msgsPerThread, len(body))
	}

	var payload any
	switch format {
	case "openai":
		payload = buildOpenAI(threads, msgsPerThread, body)
	case "anthropic":
		payload = buildAnthropic(threads, msgsPerThread, body)
	default:
		fatal(fmt.Errorf("unsupported format %q (use openai or anthropic)", format))
	}

	f, err := os.Create(outPath)
	if err != nil {
		fatal(err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(payload); err != nil {
		fatal(err)
	}

	info, err := f.Stat()
	if err != nil {
		fatal(err)
	}
	fmt.Printf("Wrote %s (%d bytes, ~%.1f MB, %d threads, format=%s)\n",
		outPath, info.Size(), float64(info.Size())/(1024*1024), threads, format)
}

func estimateThreadCount(targetMB int, format string, msgsPerThread, bodyLen int) int {
	// Rough bytes-per-thread heuristic from a tiny sample conversation.
	sample := 0
	switch format {
	case "openai":
		sample = len(mustJSON(buildOpenAI(1, msgsPerThread, strings.Repeat("x", bodyLen))))
	default:
		sample = len(mustJSON(buildAnthropic(1, msgsPerThread, strings.Repeat("x", bodyLen))))
	}
	if sample <= 0 {
		return 100
	}
	targetBytes := int64(targetMB) * 1024 * 1024
	count := int(targetBytes / int64(sample))
	if count < 10 {
		return 10
	}
	return count
}

func buildOpenAI(threads, msgsPerThread int, body string) []openAIExport {
	out := make([]openAIExport, 0, threads)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < threads; i++ {
		convID := fmt.Sprintf("conv-%05d", i)
		mapping := make(map[string]openAINode, msgsPerThread*2)
		var prev *string
		var lastID string
		for m := 0; m < msgsPerThread; m++ {
			userID := fmt.Sprintf("%s-u-%02d", convID, m)
			asstID := fmt.Sprintf("%s-a-%02d", convID, m)
			ts := base.Add(time.Duration(i*msgsPerThread+m) * time.Minute).Unix()

			userParent := prev
			mapping[userID] = openAINode{
				ID: userID,
				Message: &openAIMessage{
					Author:     openAIAuthor{Role: "user"},
					CreateTime: float64(ts),
					Content:    openAIContent{ContentType: "text", Parts: []string{fmt.Sprintf("Thread %d user %d: %s", i, m, body)}},
				},
				Parent:   userParent,
				Children: []string{asstID},
			}

			mapping[asstID] = openAINode{
				ID: asstID,
				Message: &openAIMessage{
					Author:     openAIAuthor{Role: "assistant"},
					CreateTime: float64(ts + 1),
					Content:    openAIContent{ContentType: "text", Parts: []string{fmt.Sprintf("Thread %d assistant %d: %s", i, m, body)}},
				},
				Parent:   strPtr(userID),
				Children: []string{},
			}
			prev = strPtr(asstID)
			lastID = asstID
		}
		out = append(out, openAIExport{
			ID:          convID,
			Title:       fmt.Sprintf("Lorem thread %d", i),
			CreateTime:  float64(base.Add(time.Duration(i) * time.Hour).Unix()),
			CurrentNode: lastID,
			Mapping:     mapping,
		})
	}
	return out
}

func buildAnthropic(threads, msgsPerThread int, body string) []anthropicExport {
	out := make([]anthropicExport, 0, threads)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < threads; i++ {
		msgs := make([]anthropicMessage, 0, msgsPerThread*2)
		for m := 0; m < msgsPerThread; m++ {
			ts := base.Add(time.Duration(i*msgsPerThread+m) * time.Minute)
			userText := fmt.Sprintf("Thread %d user %d: %s", i, m, body)
			asstText := fmt.Sprintf("Thread %d assistant %d: %s", i, m, body)
			msgs = append(msgs,
				anthropicMessage{
					UUID:      fmt.Sprintf("%05d-%02d-human", i, m),
					Sender:    "human",
					Text:      userText,
					CreatedAt: ts,
					Content:   []map[string]string{{"type": "text", "text": userText}},
				},
				anthropicMessage{
					UUID:      fmt.Sprintf("%05d-%02d-assistant", i, m),
					Sender:    "assistant",
					Text:      asstText,
					CreatedAt: ts.Add(time.Second),
					Content:   []map[string]string{{"type": "text", "text": asstText}},
				},
			)
		}
		out = append(out, anthropicExport{
			UUID:         fmt.Sprintf("11111111-1111-4111-8111-%012d", i),
			Name:         fmt.Sprintf("Lorem thread %d", i),
			CreatedAt:    base.Add(time.Duration(i) * time.Hour),
			ChatMessages: msgs,
		})
	}
	return out
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func strPtr(s string) *string { return &s }

func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return "."
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
