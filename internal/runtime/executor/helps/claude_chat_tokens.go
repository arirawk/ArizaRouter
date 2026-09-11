package helps

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tiktoken-go/tokenizer"
)

// CountClaudeChatTokens approximates prompt tokens for Claude Messages payloads
// using the supplied tokenizer. Image blocks are estimated from their declared
// dimensions. It mirrors the Plus edition helper used by the Kiro executor's
// CountTokens path.
func CountClaudeChatTokens(enc tokenizer.Codec, payload []byte) (int64, error) {
	if enc == nil {
		return 0, fmt.Errorf("encoder is nil")
	}
	if len(payload) == 0 {
		return 0, nil
	}

	root := gjson.ParseBytes(payload)
	segments := make([]string, 0, 32)
	imageTokens := 0

	collectClaudeChatContent(root.Get("system"), &segments, &imageTokens)
	collectClaudeChatMessages(root.Get("messages"), &segments, &imageTokens)
	collectClaudeChatTools(root.Get("tools"), &segments)

	joined := strings.TrimSpace(strings.Join(segments, "\n"))
	if joined == "" {
		return int64(imageTokens), nil
	}
	count, err := enc.Count(joined)
	if err != nil {
		return 0, err
	}
	return int64(count + imageTokens), nil
}

func collectClaudeChatMessages(messages gjson.Result, segments *[]string, imageTokens *int) {
	if !messages.Exists() || !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, message gjson.Result) bool {
		addIfNotEmpty(segments, message.Get("role").String())
		collectClaudeChatContent(message.Get("content"), segments, imageTokens)
		return true
	})
}

func collectClaudeChatContent(content gjson.Result, segments *[]string, imageTokens *int) {
	if !content.Exists() {
		return
	}
	if content.Type == gjson.String {
		addIfNotEmpty(segments, content.String())
		return
	}
	if content.IsArray() {
		content.ForEach(func(_, part gjson.Result) bool {
			switch part.Get("type").String() {
			case "text":
				addIfNotEmpty(segments, part.Get("text").String())
			case "image":
				source := part.Get("source")
				if imageTokens != nil {
					*imageTokens += estimateClaudeImageTokens(source.Get("width").Float(), source.Get("height").Float())
				}
			case "tool_use":
				addIfNotEmpty(segments, part.Get("id").String())
				addIfNotEmpty(segments, part.Get("name").String())
				if input := part.Get("input"); input.Exists() {
					addIfNotEmpty(segments, input.Raw)
				}
			case "tool_result":
				addIfNotEmpty(segments, part.Get("tool_use_id").String())
				collectClaudeChatContent(part.Get("content"), segments, imageTokens)
			case "thinking":
				addIfNotEmpty(segments, part.Get("thinking").String())
			default:
				if part.Type == gjson.String {
					addIfNotEmpty(segments, part.String())
				} else if part.Type == gjson.JSON {
					addIfNotEmpty(segments, part.Raw)
				}
			}
			return true
		})
		return
	}
	if content.Type == gjson.JSON {
		addIfNotEmpty(segments, content.Raw)
	}
}

func collectClaudeChatTools(tools gjson.Result, segments *[]string) {
	if !tools.Exists() || !tools.IsArray() {
		return
	}
	tools.ForEach(func(_, tool gjson.Result) bool {
		addIfNotEmpty(segments, tool.Get("name").String())
		addIfNotEmpty(segments, tool.Get("description").String())
		if inputSchema := tool.Get("input_schema"); inputSchema.Exists() {
			addIfNotEmpty(segments, inputSchema.Raw)
		}
		return true
	})
}

// estimateClaudeImageTokens estimates tokens for an image from its dimensions:
// tokens ≈ (width * height) / 750, clamped to [85, 1590]. Unknown dimensions
// fall back to a medium-sized image estimate.
func estimateClaudeImageTokens(width, height float64) int {
	if width <= 0 || height <= 0 {
		return 1000
	}
	tokens := int(width * height / 750)
	if tokens < 85 {
		return 85
	}
	if tokens > 1590 {
		return 1590
	}
	return tokens
}
