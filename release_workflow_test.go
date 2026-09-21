package typesafe

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const releaseWorkflowPath = ".github/workflows/release.yml"

func releaseWorkflowJob(t *testing.T, workflow, name string) string {
	t.Helper()
	start := "  " + name + ":\n"
	i := strings.Index(workflow, start)
	if i < 0 {
		t.Fatalf("release workflow missing %s job", name)
	}
	rest := workflow[i+len(start):]
	lines := strings.Split(rest, "\n")
	end := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(line, "  ") && len(line) > 2 && line[2] != ' ' {
			end = i
			break
		}
	}
	return strings.Join(lines[:end], "\n")
}

func releaseStepScript(t *testing.T, job, name string) string {
	t.Helper()
	marker := "      - name: " + name + "\n"
	if count := strings.Count(job, marker); count != 1 {
		t.Fatalf("release job has %d steps named %q; expected exactly one", count, name)
	}
	i := strings.Index(job, marker)
	lines := strings.Split(job[i+len(marker):], "\n")
	run := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "      - ") {
			break
		}
		if line == "        run: |" {
			run = i + 1
			break
		}
	}
	if run < 0 {
		t.Fatalf("release step %q has no literal run block", name)
	}
	var script []string
	for _, line := range lines[run:] {
		if line != "" && !strings.HasPrefix(line, "          ") {
			break
		}
		script = append(script, strings.TrimPrefix(line, "          "))
	}
	return strings.Join(script, "\n")
}

func readReleaseWorkflow(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(releaseWorkflowPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func linkAwk(t *testing.T, bin string) {
	t.Helper()
	awk, err := exec.LookPath("awk")
	if err != nil {
		t.Skip("awk is unavailable")
	}
	awk, err = filepath.Abs(awk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(awk, filepath.Join(bin, "awk")); err != nil {
		t.Fatal(err)
	}
}

func commandEnv(overrides map[string]string) []string {
	env := map[string]string{
		"HOME": overrides["HOME"],
		"LANG": "C",
		"PATH": overrides["PATH"],
	}
	for key, value := range overrides {
		if key != "HOME" && key != "LANG" && key != "PATH" {
			env[key] = value
		}
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func runReleaseScript(t *testing.T, script string, env map[string]string) (string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	bash, err = filepath.Abs(bash)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "-c", script)
	cmd.Dir = env["TEST_CWD"]
	cmd.Env = commandEnv(env)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestVerifyReleaseIdentityScript(t *testing.T) {
	workflow := readReleaseWorkflow(t)
	script := releaseStepScript(t, releaseWorkflowJob(t, workflow, "verify"), "Verify release identity")
	sha := strings.Repeat("a", 40)
	cases := []struct {
		name, tag, ref, eventSHA, tagSHA, headSHA, module, ancestor string
		wantOK                                                      bool
		wantGit                                                     int
	}{
		{name: "v0", tag: "v0.3.4", wantOK: true, wantGit: 3},
		{name: "v1", tag: "v1.2.3", wantOK: true, wantGit: 3},
		{name: "malformed", tag: "1.2.3"},
		{name: "prerelease", tag: "v0.2.0-rc.1"},
		{name: "leading zero", tag: "v0.02.1"},
		{name: "unsupported major", tag: "v2.0.0"},
		{name: "event SHA mismatch", tag: "v0.2.1", eventSHA: strings.Repeat("b", 40), wantGit: 2},
		{name: "event ref mismatch", tag: "v0.2.1", ref: "refs/tags/v0.2.2"},
		{name: "tag mismatch", tag: "v0.2.1", tagSHA: strings.Repeat("b", 40), wantGit: 2},
		{name: "HEAD mismatch", tag: "v0.2.1", headSHA: strings.Repeat("b", 40), wantGit: 2},
		{name: "not on main", tag: "v0.2.1", ancestor: "1", wantGit: 3},
		{name: "module mismatch", tag: "v0.2.1", module: "example.com/wrong"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			linkAwk(t, bin)
			gitLog := filepath.Join(dir, "git.log")
			writeExecutable(t, filepath.Join(bin, "git"), `
	printf '%s\n' "$*" >> "$GIT_LOG"
	if [ "$#" -eq 3 ] && [ "$1" = rev-parse ] && [ "$2" = --verify ] && [ "$3" = "refs/tags/$RELEASE_TAG^{commit}" ]; then printf '%s\n' "$TAG_SHA"; exit 0; fi
	if [ "$#" -eq 2 ] && [ "$1" = rev-parse ] && [ "$2" = HEAD ]; then printf '%s\n' "$HEAD_SHA"; exit 0; fi
	if [ "$#" -eq 4 ] && [ "$1" = merge-base ] && [ "$2" = --is-ancestor ] && [ "$3" = "$HEAD_SHA" ] && [ "$4" = refs/remotes/origin/main ]; then exit "${ANCESTOR_STATUS:-0}"; fi
	printf 'unexpected git arguments: %s\n' "$*" >&2
	exit 2
`)
			module := tc.module
			if module == "" {
				module = "github.com/stacklok/typesafe-go"
			}
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			value := func(got string) string {
				if got == "" {
					return sha
				}
				return got
			}
			ref := tc.ref
			if ref == "" {
				ref = "refs/tags/" + tc.tag
			}
			outputFile := filepath.Join(dir, "output")
			_, err := runReleaseScript(t, script, map[string]string{
				"TEST_CWD": dir, "PATH": bin, "HOME": filepath.Join(dir, "home"),
				"EVENT_REF": ref, "EVENT_SHA": value(tc.eventSHA), "RELEASE_TAG": tc.tag,
				"TAG_SHA": value(tc.tagSHA), "HEAD_SHA": value(tc.headSHA),
				"ANCESTOR_STATUS": tc.ancestor, "GITHUB_OUTPUT": outputFile, "GIT_LOG": gitLog,
			})
			if (err == nil) != tc.wantOK {
				t.Fatalf("success=%t, want %t (err=%v)", err == nil, tc.wantOK, err)
			}
			wantGit := []string{}
			if tc.wantGit >= 1 {
				wantGit = append(wantGit, "rev-parse --verify refs/tags/"+tc.tag+"^{commit}")
			}
			if tc.wantGit >= 2 {
				wantGit = append(wantGit, "rev-parse HEAD")
			}
			if tc.wantGit >= 3 {
				wantGit = append(wantGit, "merge-base --is-ancestor "+value(tc.headSHA)+" refs/remotes/origin/main")
			}
			logged, _ := os.ReadFile(gitLog)
			wantLog := strings.Join(wantGit, "\n")
			if wantLog != "" {
				wantLog += "\n"
			}
			if string(logged) != wantLog {
				t.Fatalf("git calls = %q, want %q", logged, wantLog)
			}
			output, readErr := os.ReadFile(outputFile)
			if tc.wantOK {
				want := fmt.Sprintf("release_tag=%s\nrelease_sha=%s\n", tc.tag, sha)
				if readErr != nil || string(output) != want {
					t.Fatalf("output = %q, %v; want %q", output, readErr, want)
				}
			} else if readErr == nil && len(output) != 0 {
				t.Fatalf("failed verification wrote output %q", output)
			}
		})
	}
}

func TestPublishGitHubReleaseScript(t *testing.T) {
	workflow := readReleaseWorkflow(t)
	script := releaseStepScript(t, releaseWorkflowJob(t, workflow, "publish"), "Publish GitHub release")
	sha := strings.Repeat("a", 40)
	cases := []struct {
		name, tag, currentSHA, missing, existing string
		wantOK, wantAPI, wantCreate              bool
	}{
		{name: "matching tag", tag: "v0.4.0", currentSHA: sha, wantOK: true, wantAPI: true, wantCreate: true},
		{name: "matching v1 tag", tag: "v1.0.0", currentSHA: sha, wantOK: true, wantAPI: true, wantCreate: true},
		{name: "moved tag", tag: "v0.4.0", currentSHA: strings.Repeat("b", 40), wantAPI: true},
		{name: "missing tag", tag: "v0.4.0", currentSHA: sha, missing: "1", wantAPI: true},
		{name: "existing release", tag: "v0.4.0", currentSHA: sha, existing: "1", wantAPI: true, wantCreate: true},
		{name: "malicious tag", tag: "v0.4.0$(printf pwned > PWNED)", currentSHA: sha},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(dir, "gh.log")
			writeExecutable(t, filepath.Join(bin, "gh"), `
	printf '%s\n' "$*" >> "$GH_LOG"
	if [ "$#" -eq 4 ] && [ "$1" = api ] && [ "$2" = "repos/$GH_REPO/commits/refs/tags/$RELEASE_TAG" ] && [ "$3" = --jq ] && [ "$4" = .sha ]; then [ -z "$MISSING" ] || exit 1; printf '%s\n' "$CURRENT_SHA"; exit 0; fi
	if [ "$#" -eq 7 ] && [ "$1" = release ] && [ "$2" = create ] && [ "$3" = --repo ] && [ "$4" = "$GH_REPO" ] && [ "$5" = "$RELEASE_TAG" ] && [ "$6" = --verify-tag ] && [ "$7" = --generate-notes ]; then [ -z "$EXISTING" ] || exit 1; exit 0; fi
	printf 'unexpected gh arguments: %s\n' "$*" >&2
	exit 2
`)
			_, err := runReleaseScript(t, script, map[string]string{
				"TEST_CWD": dir, "PATH": bin, "HOME": filepath.Join(dir, "home"),
				"GH_LOG": log, "GH_REPO": "stacklok/typesafe-go", "RELEASE_TAG": tc.tag,
				"VERIFIED_SHA": sha, "CURRENT_SHA": tc.currentSHA, "MISSING": tc.missing, "EXISTING": tc.existing,
			})
			if (err == nil) != tc.wantOK {
				t.Fatalf("success=%t, want %t (err=%v)", err == nil, tc.wantOK, err)
			}
			wantCalls := []string{}
			if tc.wantAPI {
				wantCalls = append(wantCalls, "api repos/stacklok/typesafe-go/commits/refs/tags/"+tc.tag+" --jq .sha")
			}
			if tc.wantCreate {
				wantCalls = append(wantCalls, "release create --repo stacklok/typesafe-go "+tc.tag+" --verify-tag --generate-notes")
			}
			logged, _ := os.ReadFile(log)
			wantLog := strings.Join(wantCalls, "\n")
			if wantLog != "" {
				wantLog += "\n"
			}
			if string(logged) != wantLog {
				t.Fatalf("gh calls = %q, want %q", logged, wantLog)
			}
			if _, statErr := os.Stat(filepath.Join(dir, "PWNED")); !os.IsNotExist(statErr) {
				t.Fatalf("malicious tag was executed: %v", statErr)
			}
		})
	}
}
