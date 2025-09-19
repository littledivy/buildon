package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml"
)

type Remote struct {
	Host  string
	User  string
	Shell string
	Path  string
}

type Config struct {
	Remote map[string]Remote
}

func loadConfig() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home dir: %v", err)
	}

	configPath := filepath.Join(home, ".config", "buildon", "config.toml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("failed to read config at %s: %v", configPath, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}
	return cfg
}
func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func gitOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

func splitNullBytes(b []byte) []string {
	parts := strings.Split(string(b), "\x00")
	var out []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func filesToSync() ([]string, error) {
	if _, err := gitOutput("rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, errors.New("not a git repository (run inside your repo)")
	}

	trackedRaw, err := gitOutput("ls-files", "-z")
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	untrackedRaw, err := gitOutput("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files --others failed: %w", err)
	}

	seen := map[string]struct{}{}
	var all []string
	for _, f := range splitNullBytes(trackedRaw) {
		if _, ok := seen[f]; !ok {
			seen[f] = struct{}{}
			all = append(all, f)
		}
	}
	for _, f := range splitNullBytes(untrackedRaw) {
		if _, ok := seen[f]; !ok {
			seen[f] = struct{}{}
			all = append(all, f)
		}
	}

	var existing []string
	for _, p := range all {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}
	return existing, nil
}

func rsyncToRemote(remote Remote) error {
	files, err := filesToSync()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Println("==> Nothing to sync (file list is empty).")
		return nil
	}

	fmt.Println("==> Files to sync:")
	for _, f := range files {
		fmt.Println(f)
	}

	tmp, err := os.CreateTemp("", "buildon-files-*.txt")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	for _, f := range files {
		if _, err := tmp.WriteString(f + "\n"); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return fmt.Errorf("write temp list: %w", err)
		}
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	dest := fmt.Sprintf("%s@%s:%s", remote.User, remote.Host, remote.Path)

	if !hasCmd("rsync") {
		return fmt.Errorf("rsync not found on PATH (install rsync or run via WSL/Git Bash/MSYS2)")
	}

	args := []string{
		"-avz",
		"--files-from=" + tmp.Name(),
		"./",
		dest,
	}
	fmt.Println("==> Syncing via rsync...")
	cmd := exec.Command("rsync", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func quotePS(s string) string {
	s = strings.ReplaceAll(s, `'`, `''`)
	return `'` + s + `'`
}

func openInteractiveShell(remote Remote) error {
	target := fmt.Sprintf("%s@%s", remote.User, remote.Host)

	if remote.Shell == "powershell" {
		ps := fmt.Sprintf(
			`$p=%s; New-Item -ItemType Directory -Force -Path $p *> $null; Set-Location -Path $p;`,
			quotePS(remote.Path),
		)
		sshArgs := []string{"-t", target, "powershell", "-NoProfile", "-NoLogo", "-NoExit", "-Command", ps}
		c := exec.Command("ssh", sshArgs...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}

	cmdStr := fmt.Sprintf("mkdir -p %s && cd %s && exec ${SHELL:-bash} -l",
		shellQuotePOSIX(remote.Path), shellQuotePOSIX(remote.Path))
	sshArgs := []string{"-t", target, cmdStr}
	c := exec.Command("ssh", sshArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func runRemoteCommand(remote Remote, command []string) error {
	if len(command) == 0 {
		return openInteractiveShell(remote)
	}
	target := fmt.Sprintf("%s@%s", remote.User, remote.Host)

	if remote.Shell == "powershell" {
		ps := fmt.Sprintf(
			`$p=%s; Set-Location -Path $p; %s`,
			quotePS(remote.Path),
			strings.Join(command, " "),
		)
		sshArgs := []string{target, "powershell", "-NoProfile", "-NoLogo", "-Command", ps}
		fmt.Printf("==> Running on %s: %s\n", target, strings.Join(command, " "))
		c := exec.Command("ssh", sshArgs...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}

	cmdStr := fmt.Sprintf("mkdir -p %s && cd %s && %s",
		shellQuotePOSIX(remote.Path), shellQuotePOSIX(remote.Path), strings.Join(command, " "))
	sshArgs := []string{target, cmdStr}
	fmt.Printf("==> Running on %s: %s\n", target, strings.Join(command, " "))
	c := exec.Command("ssh", sshArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func shellQuotePOSIX(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: buildon <remote-name> [command...]")
		os.Exit(1)
	}

	remoteName := os.Args[1]
	command := os.Args[2:]

	cfg := loadConfig()
	remote, ok := cfg.Remote[remoteName]
	if !ok {
		log.Fatalf("no remote named %s", remoteName)
	}

	if err := rsyncToRemote(remote); err != nil {
		log.Fatal(err)
	}

	if err := runRemoteCommand(remote, command); err != nil {
		log.Fatal(err)
	}
}
