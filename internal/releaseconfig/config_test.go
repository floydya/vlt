package releaseconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(repositoryRoot(t), path))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(contents)
}

func TestReleasePleaseConfiguration(t *testing.T) {
	var manifest map[string]string
	if err := json.Unmarshal([]byte(readRepositoryFile(t, ".release-please-manifest.json")), &manifest); err != nil {
		t.Fatalf("manifest JSON error = %v", err)
	}
	if got := manifest["."]; got != "0.0.0" {
		t.Errorf("initial version = %q, want 0.0.0", got)
	}

	var config struct {
		ReleaseType               string         `json:"release-type"`
		IncludeComponentInTag     bool           `json:"include-component-in-tag"`
		IncludeVInTag             bool           `json:"include-v-in-tag"`
		BumpMinorPreMajor         bool           `json:"bump-minor-pre-major"`
		BumpPatchForMinorPreMajor bool           `json:"bump-patch-for-minor-pre-major"`
		AlwaysUpdate              bool           `json:"always-update"`
		PullRequestTitlePattern   string         `json:"pull-request-title-pattern"`
		Packages                  map[string]any `json:"packages"`
		ChangelogSections         []struct {
			Type   string `json:"type"`
			Hidden bool   `json:"hidden"`
		} `json:"changelog-sections"`
	}
	if err := json.Unmarshal([]byte(readRepositoryFile(t, "release-please-config.json")), &config); err != nil {
		t.Fatalf("config JSON error = %v", err)
	}
	if config.ReleaseType != "go" {
		t.Errorf("release type = %q, want go", config.ReleaseType)
	}
	if config.IncludeComponentInTag || !config.IncludeVInTag {
		t.Errorf("tag settings produce component-prefixed or unprefixed tags")
	}
	if config.BumpMinorPreMajor || config.BumpPatchForMinorPreMajor {
		t.Errorf("pre-major settings weaken strict semantic versioning")
	}
	if !config.AlwaysUpdate {
		t.Errorf("release pull request is not kept current")
	}
	if config.PullRequestTitlePattern != "chore(VLT-REL): release ${version}" {
		t.Errorf("release PR title pattern = %q", config.PullRequestTitlePattern)
	}
	if _, ok := config.Packages["."]; !ok {
		t.Errorf("root package is not configured")
	}

	sections := make(map[string]bool, len(config.ChangelogSections))
	for _, section := range config.ChangelogSections {
		sections[section.Type] = section.Hidden
	}
	for _, visible := range []string{"feat", "fix", "perf", "revert"} {
		if hidden, ok := sections[visible]; !ok || hidden {
			t.Errorf("release type %q is missing or hidden", visible)
		}
	}
	for _, hidden := range []string{"build", "chore", "ci", "docs", "refactor", "style", "test"} {
		if isHidden, ok := sections[hidden]; !ok || !isHidden {
			t.Errorf("maintenance type %q is missing or visible", hidden)
		}
	}
}

func TestWorkflowsUseExternalActionVersionTags(t *testing.T) {
	workflowPaths := []string{
		".github/workflows/ci.yml",
		".github/workflows/publish-homebrew.yml",
		".github/workflows/release-please.yml",
	}
	versionedAction := regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+@(?:v[1-9][0-9]*|[0-9]{4}\.[0-9]{2}\.[0-9]{2}\.[0-9]+)$`)
	for _, path := range workflowPaths {
		for lineNumber, line := range strings.Split(readRepositoryFile(t, path), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "uses:") {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "uses:"))
			if strings.HasPrefix(value, "./") {
				continue
			}
			if !versionedAction.MatchString(value) {
				t.Errorf("%s:%d action does not use a version tag: %q", path, lineNumber+1, value)
			}
		}
	}
}

func TestReleaseWorkflowRequiresDedicatedToken(t *testing.T) {
	workflow := readRepositoryFile(t, ".github/workflows/release-please.yml")
	if !strings.Contains(workflow, "secrets.RELEASE_PLEASE_TOKEN") {
		t.Errorf("release workflow does not use the dedicated Release Please token")
	}
	if !strings.Contains(workflow, "uses: ./.github/workflows/ci.yml") {
		t.Errorf("release workflow does not run the repository quality gate")
	}
}

func TestHomebrewWorkflowHasSafeReleaseAndRetryTriggers(t *testing.T) {
	workflow := readRepositoryFile(t, ".github/workflows/publish-homebrew.yml")
	for _, want := range []string{
		"types: [published]",
		"workflow_dispatch:",
		"secrets.HOMEBREW_TAP_TOKEN",
		"RELEASE_TAG:",
		"go run ./cmd/render-homebrew-formula",
		"git status --porcelain -- Formula/vlt.rb",
		"gh pr merge --auto --squash",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("Homebrew workflow does not contain %q", want)
		}
	}
	if strings.Contains(workflow, "run: ${{") {
		t.Errorf("Homebrew workflow interpolates event data directly as a command")
	}
}

func TestBuildAllIncludesDarwinArchitectures(t *testing.T) {
	justfile := readRepositoryFile(t, "justfile")
	for _, target := range []string{
		"GOOS=darwin GOARCH=amd64 go build ./cmd/vlt",
		"GOOS=darwin GOARCH=arm64 go build ./cmd/vlt",
	} {
		if !strings.Contains(justfile, target) {
			t.Errorf("build-all does not contain %q", target)
		}
	}
}
