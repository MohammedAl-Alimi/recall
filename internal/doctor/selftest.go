package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MohammedAl-Alimi/recall/internal/scan"
)

// selfTestTranscript is a tiny hand-written transcript in the observed
// Claude Code 2.1.x schema. It is written to a temp file and parsed so the
// parser is exercised on every install without touching the Claude dir.
const selfTestTranscript = `{"parentUuid":null,"isSidechain":false,"type":"user","uuid":"u1","timestamp":"2026-01-02T10:00:00.000Z","cwd":"/Users/alice/dev/app","sessionId":"0d0c0b0a-0000-4000-8000-000000000001","version":"2.1.223","gitBranch":"main","entrypoint":"cli","promptId":"p1","message":{"role":"user","content":"Add a retry helper to the http client"}}
{"parentUuid":"u1","isSidechain":false,"type":"assistant","uuid":"a1","timestamp":"2026-01-02T10:00:05.000Z","cwd":"/Users/alice/dev/app","sessionId":"0d0c0b0a-0000-4000-8000-000000000001","version":"2.1.223","gitBranch":"main","entrypoint":"cli","promptId":"p1","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"Adding a retry helper."},{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/Users/alice/dev/app/client.go"}}],"usage":{"input_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":5}}}
{"parentUuid":"a1","isSidechain":false,"type":"user","uuid":"u2","timestamp":"2026-01-02T10:00:06.000Z","cwd":"/Users/alice/dev/app","sessionId":"0d0c0b0a-0000-4000-8000-000000000001","version":"2.1.223","gitBranch":"main","entrypoint":"cli","promptId":"p1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"parentUuid":"u2","isSidechain":false,"type":"assistant","uuid":"a2","timestamp":"2026-01-02T10:00:08.000Z","cwd":"/Users/alice/dev/app","sessionId":"0d0c0b0a-0000-4000-8000-000000000001","version":"2.1.223","gitBranch":"main","entrypoint":"cli","promptId":"p1","message":{"role":"assistant","model":"claude-test","content":[{"type":"text","text":"Done."}],"usage":{"input_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":3}}}
{"type":"ai-title","aiTitle":"Retry helper for http client","sessionId":"0d0c0b0a-0000-4000-8000-000000000001"}
{"type":"permission-mode","permissionMode":"default"}
{"type":"mode","mode":"default"}
{"type":"last-prompt","lastPrompt":"Add a retry helper to the http client"}
`

// checkParser writes the embedded transcript to a temp dir and parses it.
func checkParser() Check {
	dir, err := os.MkdirTemp("", "recall-selftest-")
	if err != nil {
		return Check{Name: "parser", Status: "warn", Detail: "self-test skipped: " + err.Error()}
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "0d0c0b0a-0000-4000-8000-000000000001.jsonl")
	if err := os.WriteFile(path, []byte(selfTestTranscript), 0o600); err != nil {
		return Check{Name: "parser", Status: "warn", Detail: "self-test skipped: " + err.Error()}
	}
	sess, err := scan.ParseTranscript(path)
	if err != nil {
		return Check{Name: "parser", Status: "fail", Detail: "self-test transcript failed to parse: " + err.Error()}
	}
	var problems []string
	if sess.Title != "Retry helper for http client" {
		problems = append(problems, fmt.Sprintf("title %q", sess.Title))
	}
	if sess.Branch != "main" {
		problems = append(problems, fmt.Sprintf("branch %q", sess.Branch))
	}
	if sess.Cwd != "/Users/alice/dev/app" {
		problems = append(problems, fmt.Sprintf("cwd %q", sess.Cwd))
	}
	if sess.Turns < 1 {
		problems = append(problems, fmt.Sprintf("turns %d", sess.Turns))
	}
	if len(sess.FilesChanged) != 1 {
		problems = append(problems, fmt.Sprintf("files changed %d", len(sess.FilesChanged)))
	}
	if sess.Headless {
		problems = append(problems, "flagged headless")
	}
	if len(problems) > 0 {
		return Check{Name: "parser", Status: "fail", Detail: "self-test mismatch: " + joinComma(problems)}
	}
	return Check{Name: "parser", Status: "ok", Detail: fmt.Sprintf("self-test transcript parsed: %d lines, title, branch, cwd, %d file changed", sess.Lines, len(sess.FilesChanged))}
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
