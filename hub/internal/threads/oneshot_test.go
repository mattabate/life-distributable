package threads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The router, titles and preflight load no MCP server from any settings
// layer, and every one-shot in the package takes its args from oneShotArgs.
func TestOneShotLoadsNoMCP(t *testing.T) {
	got := strings.Join(oneShotArgs("m"), " ")
	for _, want := range []string{"--tools  ", "--max-turns 1", `--strict-mcp-config --mcp-config {"mcpServers":{}}`, "--model m"} {
		if !strings.Contains(got+" ", want) {
			t.Errorf("oneShotArgs = %q, want %q", got, want)
		}
	}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "preflight.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"--max-turns", "1"`) {
			t.Errorf("%s builds a one-shot's args itself: use oneShotArgs", f)
		}
	}
}
