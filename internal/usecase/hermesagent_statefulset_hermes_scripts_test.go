package usecase

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runDotEnvScript runs the script buildDotEnvScript renders, with a stub
// `hermes` that points the .env at a temp file, and returns that file.
func runDotEnvScript(t *testing.T, egressTokensDir string, mountPaths ...string) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	envFile := filepath.Join(dir, "out.env")
	bin := filepath.Join(dir, "bin")
	writeFiles(t, bin, map[string]string{"hermes": "#!/bin/sh\necho " + envFile + "\n"})
	if err := os.Chmod(filepath.Join(bin, "hermes"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(sh, "-c", buildDotEnvScript("default", egressTokensDir, mountPaths...))
	cmd.Env = []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildDotEnvScript(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user")
	tokens := filepath.Join(dir, "tokens")
	writeFiles(t, user, map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-real",
		"OTHER_VAR":         "kept",
	})
	writeFiles(t, tokens, map[string]string{
		"ANTHROPIC_API_KEY": "anthropic-token",
		"GEMINI_API_KEY":    "gemini-token",
		"proxy.yaml":        "dns:\n  enabled: false\n",
	})

	t.Run("egress swaps real keys for tokens", func(t *testing.T) {
		got := runDotEnvScript(t, tokens, user)
		for _, line := range []string{"ANTHROPIC_API_KEY=anthropic-token", "GEMINI_API_KEY=gemini-token", "OTHER_VAR=kept"} {
			if !strings.Contains(got, line+"\n") {
				t.Errorf("expected %q in the .env:\n%s", line, got)
			}
		}
		for _, leak := range []string{"sk-ant-real", "proxy.yaml", "dns:"} {
			if strings.Contains(got, leak) {
				t.Errorf("the .env must not contain %q:\n%s", leak, got)
			}
		}
	})

	t.Run("without egress the values are written as they are", func(t *testing.T) {
		got := runDotEnvScript(t, "", user)
		if !strings.Contains(got, "ANTHROPIC_API_KEY=sk-ant-real\n") || strings.Contains(got, "token") {
			t.Errorf("unexpected .env:\n%s", got)
		}
	})
}
