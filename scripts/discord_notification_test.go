package scripts_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type discordNotificationPayload struct {
	Embeds []struct {
		Description string `json:"description"`
	} `json:"embeds"`
}

func TestDiscordReleaseNotificationTruncatesMultilineNotes(t *testing.T) {
	body := strings.Repeat("A release note with several short lines.\n", 140)
	payload := runDiscordNotification(t, body)
	description := payload.Embeds[0].Description
	require.True(t, strings.HasPrefix(description, string([]rune(body)[:500])))
	require.Contains(t, description, "[read full release notes](https://github.com/javinizer/javinizer-go/releases/tag/v1.6.2)")
	require.LessOrEqual(t, utf8.RuneCountInString(description), 4096)
}

func TestDiscordReleaseNotificationNoteBoundaries(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "empty"},
		{name: "short multiline", body: "First line\n\nSecond line"},
		{name: "below limit", body: strings.Repeat("a", 499)},
		{name: "at limit", body: strings.Repeat("a", 500)},
		{name: "above limit", body: strings.Repeat("a", 501)},
		{name: "multiline at limit", body: strings.Repeat("a", 249) + "\n" + strings.Repeat("b", 250)},
		{name: "Unicode below limit", body: strings.Repeat("界😀é", 166)},
		{name: "Unicode at limit", body: strings.Repeat("界😀", 250)},
		{name: "Unicode above limit", body: strings.Repeat("界😀é", 170)},
		{name: "multiline Unicode", body: strings.Repeat("更新😀 café\n", 100)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := runDiscordNotification(t, tc.body)
			description := payload.Embeds[0].Description
			runes := []rune(tc.body)
			if len(runes) > 500 {
				require.True(t, strings.HasPrefix(description, string(runes[:500])))
				require.Contains(t, description, "[read full release notes](https://github.com/javinizer/javinizer-go/releases/tag/v1.6.2)")
				require.Less(t, utf8.RuneCountInString(description), 700)
				return
			}
			if tc.body == "" {
				require.Equal(t, "*(no release notes)*", description)
				return
			}
			require.Equal(t, tc.body, description)
		})
	}
}

func runDiscordNotification(t *testing.T, body string) discordNotificationPayload {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Discord notification workflow runs in Bash on Ubuntu")
	}
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required to exercise the release notification workflow", tool)
		}
	}

	workflowData, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "cli-release.yml"))
	require.NoError(t, err)
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(workflowData, &workflow))
	job, ok := workflow.Jobs["notify-discord"]
	require.True(t, ok)
	var script string
	for _, step := range job.Steps {
		if step.Name == "Post release to Discord webhook" {
			script = step.Run
			break
		}
	}
	require.NotEmpty(t, script)

	fixture, err := json.Marshal(map[string]any{
		"body": body,
		"author": map[string]string{
			"login":      "release-bot",
			"avatar_url": "https://example.invalid/avatar.png",
		},
		"published_at": "2026-10-16T13:48:06Z",
	})
	require.NoError(t, err)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(`#!/usr/bin/env bash
set -euo pipefail
test "$#" -eq 2
test "$1" = api
test "$2" = repos/javinizer/javinizer-go/releases/tags/v1.6.2
printf '%s' "$DISCORD_TEST_RELEASE_JSON"
`), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "curl"), []byte(`#!/usr/bin/env bash
set -euo pipefail
payload=
webhook_seen=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    -d)
      payload="$2"
      shift 2
      ;;
    https://discord.invalid/test-webhook)
      webhook_seen=true
      shift
      ;;
    *)
      shift
      ;;
  esac
done
test "$webhook_seen" = true
test -n "$payload"
printf '%s' "$payload" > "$DISCORD_TEST_PAYLOAD_PATH"
if ! printf '%s' "$payload" | jq -e '(.embeds[0].description | length) <= 4096' >/dev/null; then
  printf 'simulated Discord: embed description exceeds 4096 characters\n' >&2
  exit 22
fi
`), 0700))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", script)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"BASH_ENV=/dev/null",
		"ENV=/dev/null",
		"DISCORD_WEBHOOK_URL=https://discord.invalid/test-webhook",
		"DISCORD_TEST_RELEASE_JSON="+string(fixture),
		"DISCORD_TEST_PAYLOAD_PATH="+payloadPath,
		"TAG=v1.6.2",
		"NAME=Test release",
		"PRERELEASE=false",
		"REPO=javinizer/javinizer-go",
		"GH_TOKEN=unused-fixture-token",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "notification workflow failed: %s", output)
	require.NotContains(t, string(output), "integer expression expected")

	payloadData, err := os.ReadFile(payloadPath)
	require.NoError(t, err)
	require.True(t, utf8.Valid(payloadData), "notification payload must be valid UTF-8")
	var payload discordNotificationPayload
	require.NoError(t, json.Unmarshal(payloadData, &payload))
	require.Len(t, payload.Embeds, 1)
	return payload
}
