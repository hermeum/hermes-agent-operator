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

// TestEgressDotEnvScripts runs the scripts that keep the proxy env out of the
// .env files while the init containers run and add it back for the agent.
func TestEgressDotEnvScripts(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	home := t.TempDir()
	defaultEnv := filepath.Join(home, ".env")
	workEnv := filepath.Join(home, "profiles", "work", ".env")
	writeFiles(t, home, map[string]string{".env": "OTHER_VAR=kept\n"})
	// A .env without a trailing newline must not merge into the proxy env.
	writeFiles(t, filepath.Dir(workEnv), map[string]string{".env": "WORK_VAR=kept"})

	run := func(t *testing.T, script string) {
		t.Helper()
		cmd := exec.Command(sh, "-c", script)
		cmd.Env = []string{"HERMES_HOME=" + home, "PATH=" + os.Getenv("PATH")}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
	}
	read := func(t *testing.T, path string) string {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(got)
	}
	appendScript := buildEgressDotEnvAppendScript(egressHA(nil).GetEgress().GetAgentProxyEnv())

	t.Run("init-egress adds the proxy env to every .env once", func(t *testing.T) {
		run(t, appendScript)
		run(t, appendScript)
		for path, kept := range map[string]string{defaultEnv: "OTHER_VAR=kept\n", workEnv: "WORK_VAR=kept\n"} {
			got := read(t, path)
			for _, line := range []string{
				kept,
				"HTTPS_PROXY=http://127.0.0.1:9090\n",
				"NO_PROXY=localhost,127.0.0.1,.svc,.cluster.local,${KUBERNETES_SERVICE_HOST}\n",
				"SSL_CERT_FILE=/etc/hermes-egress/ca.crt\n",
			} {
				if !strings.Contains(got, line) {
					t.Errorf("expected %q in %s:\n%s", line, path, got)
				}
			}
			if n := strings.Count(got, "HTTPS_PROXY="); n != 1 {
				t.Errorf("expected the proxy env once in %s, got it %d times:\n%s", path, n, got)
			}
		}
	})

	t.Run("the first init step drops it again", func(t *testing.T) {
		run(t, buildEgressDotEnvStripScript())
		for path, want := range map[string]string{defaultEnv: "OTHER_VAR=kept\n", workEnv: "WORK_VAR=kept\n"} {
			if got := read(t, path); got != want {
				t.Errorf("expected %s to hold only %q, got:\n%s", path, want, got)
			}
		}
	})
}
