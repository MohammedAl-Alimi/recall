package scan

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// flexTime accepts an RFC3339 string or an epoch value (seconds or
// milliseconds) and decodes to a time.Time. Unparseable values decode to
// the zero time rather than failing the whole record.
type flexTime struct{ time.Time }

func (t *flexTime) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		t.Time = time.Time{}
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return nil
		}
		t.Time = parseTimestamp(s)
		return nil
	}
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return nil
	}
	t.Time = epochToTime(f)
	return nil
}

// parseTimestamp parses the RFC3339 timestamps Claude Code writes, tolerating
// a plain epoch string.
func parseTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return epochToTime(f)
	}
	return time.Time{}
}

// epochToTime treats values above 1e12 as milliseconds, else seconds.
func epochToTime(f float64) time.Time {
	if f <= 0 {
		return time.Time{}
	}
	if f > 1e12 {
		return time.UnixMilli(int64(f)).UTC()
	}
	return time.Unix(int64(f), 0).UTC()
}

// flexInt accepts a JSON number or a numeric string.
type flexInt int

func (n *flexInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*n = 0
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return nil
		}
		b = []byte(strings.TrimSpace(s))
	}
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		*n = 0
		return nil
	}
	*n = flexInt(int(f))
	return nil
}

// flexString accepts a JSON string, number or bool and keeps its text form.
type flexString string

func (s *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*s = ""
		return nil
	}
	if b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			*s = ""
			return nil
		}
		*s = flexString(v)
		return nil
	}
	if b[0] == '{' || b[0] == '[' {
		*s = ""
		return nil
	}
	*s = flexString(string(b))
	return nil
}

// rawRecord is the union of every transcript line shape recall cares about.
// Fields of uncertain type use the flex* helpers so a surprising value never
// turns into a parse error for the whole line.
type rawRecord struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`

	// Envelope (conversational records).
	ParentUUID       flexString `json:"parentUuid"`
	IsSidechain      bool       `json:"isSidechain"`
	UUID             flexString `json:"uuid"`
	Timestamp        flexTime   `json:"timestamp"`
	Cwd              flexString `json:"cwd"`
	SessionID        flexString `json:"sessionId"`
	Version          flexString `json:"version"`
	GitBranch        flexString `json:"gitBranch"`
	Entrypoint       flexString `json:"entrypoint"`
	Slug             flexString `json:"slug"`
	PromptID         flexString `json:"promptId"`
	IsMeta           bool       `json:"isMeta"`
	IsCompactSummary bool       `json:"isCompactSummary"`
	SessionKind      flexString `json:"sessionKind"`

	Message    *rawMessage    `json:"message"`
	Attachment *rawAttachment `json:"attachment"`

	// Metadata records (no envelope).
	AITitle        flexString `json:"aiTitle"`
	CustomTitle    flexString `json:"customTitle"`
	AgentName      flexString `json:"agentName"`
	LastPrompt     flexString `json:"lastPrompt"`
	Mode           flexString `json:"mode"`
	PermissionMode flexString `json:"permissionMode"`
	TotalCostUSD   float64    `json:"totalCostUSD"`
	Summary        flexString `json:"summary"`

	// pr-link: observed keys prUrl/prNumber/prRepository; the contract's
	// url/number/repo spelling is accepted too.
	URL          flexString `json:"url"`
	PrURL        flexString `json:"prUrl"`
	Number       flexInt    `json:"number"`
	PrNumber     flexInt    `json:"prNumber"`
	Repo         flexString `json:"repo"`
	PrRepository flexString `json:"prRepository"`

	// frame-link: observed key frameUrl plus title.
	FrameURL flexString `json:"frameUrl"`
	Title    flexString `json:"title"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   flexString      `json:"model"`
	Usage   *rawUsage       `json:"usage"`
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

type rawAttachment struct {
	Type flexString `json:"type"`
	URL  flexString `json:"url"`
}

// rawBlock is one entry of a message.content array.
type rawBlock struct {
	Type      string          `json:"type"`
	Text      flexString      `json:"text"`
	ID        flexString      `json:"id"`
	Name      flexString      `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID flexString      `json:"tool_use_id"`
}

// rawToolInput holds the only tool_use input fields recall inspects.
type rawToolInput struct {
	FilePath        flexString `json:"file_path"`
	NotebookPath    flexString `json:"notebook_path"`
	RunInBackground bool       `json:"run_in_background"`
}

// contentBlocks decodes message.content, which is either a plain string or
// an array of blocks. A plain string is returned as a single text block.
func contentBlocks(raw json.RawMessage) ([]rawBlock, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, true
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, false
		}
		return []rawBlock{{Type: "text", Text: flexString(s)}}, true
	}
	if raw[0] == '[' {
		var blocks []rawBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return nil, false
		}
		return blocks, true
	}
	return nil, false
}

// textOf joins the text blocks of a content array.
func textOf(blocks []rawBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(string(b.Text))
		}
	}
	return sb.String()
}

// hasBlock reports whether any block has the given type.
func hasBlock(blocks []rawBlock, typ string) bool {
	for _, b := range blocks {
		if b.Type == typ {
			return true
		}
	}
	return false
}
