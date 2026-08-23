// Package deepseek is a Go port of packages/llm/llm-deepseek from
// DeepSeek Harness: a direct HTTP+SSE adapter implementing agentloop.LLM
// against the DeepSeek (OpenAI-compatible) chat-completions endpoint.
package deepseek

// wireRequest is the POST {baseURL}/chat/completions request body.
type wireRequest struct {
	Model           string        `json:"model"`
	Messages        []wireMessage `json:"messages"`
	Stream          bool          `json:"stream"`
	StreamOptions   streamOptions `json:"stream_options"`
	Thinking        *wireThinking `json:"thinking,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	Tools           []wireTool    `json:"tools,omitempty"`
	Temperature     *float64      `json:"temperature,omitempty"`
	TopP            *float64      `json:"top_p,omitempty"`
	MaxTokens       int           `json:"max_tokens,omitempty"`
	Stop            []string      `json:"stop,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireThinking struct {
	Type string `json:"type"` // "enabled" | "disabled"
}

// wireMessage is one entry of the request `messages` array. Only the fields
// relevant to its role are populated; unused fields are omitted or ignored
// by role, per the shared struct convention this package uses throughout.
type wireMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content"` // never omitted — DeepSeek rejects assistant messages with neither content nor tool_calls
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireToolFunction `json:"function"`
}

type wireToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// wireTool is one entry of the request `tools` array.
type wireTool struct {
	Type     string           `json:"type"`
	Function wireToolFuncSpec `json:"function"`
}

type wireToolFuncSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// wireChunk is one parsed SSE `data:` payload (a chat.completion.chunk).
type wireChunk struct {
	Choices []wireChoice `json:"choices"`
	// Arrives attached to the finish chunk and/or as a trailing usage-only chunk.
	Usage *wireUsage `json:"usage"`
}

type wireChoice struct {
	Delta        wireDelta `json:"delta"`
	FinishReason string    `json:"finish_reason"`
}

// wireDelta is the incremental content of one streamed choice; any subset
// of fields may be present per chunk.
type wireDelta struct {
	Content          string              `json:"content"`
	ReasoningContent string              `json:"reasoning_content"`
	ToolCalls        []wireToolCallDelta `json:"tool_calls"`
}

// wireToolCallDelta is a streamed fragment of one tool call; fragments
// sharing an Index concatenate into one call.
type wireToolCallDelta struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id,omitempty"`
	Function *wireToolFunctionDelta `json:"function,omitempty"`
}

type wireToolFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// wireUsage is wire token accounting. PromptTokens INCLUDES cache hits;
// mapUsage subtracts them to keep disjoint input/cache-read counts.
type wireUsage struct {
	PromptTokens         int `json:"prompt_tokens"`
	CompletionTokens     int `json:"completion_tokens"`
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails  struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// wireError is a non-2xx error body.
type wireError struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}
