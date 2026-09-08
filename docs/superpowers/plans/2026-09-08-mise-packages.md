# Mise Packages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add user-global Mise `[tools]` declarations as a third package source under the existing Packages provider, with semantic capture, conservative merge/restore, native Mise installation, include/exclude support, and schema-6 persistence.

**Architecture:** Packages remains one provider with official, AUR, and Mise sources. Mise declarations are normalized into structured per-tool maps stored in `packages/mise.toml`; restore appends only missing declarations to the user's primary global Mise config, validates the complete candidate TOML, writes it atomically with existing `FileWrite` preconditions, then runs `mise -C / install` for the newly added tools. Existing differing declarations and target-only tools are preserved.

**Tech Stack:** Go 1.25+, Cobra, `github.com/pelletier/go-toml/v2` v2.2.4, existing `command.Runner`, `model.RestorePlan`, generated `FileWrite`, and restore executor/journal.

**Spec:** `docs/superpowers/specs/2026-09-08-mise-packages-design.md`

## Global Constraints

- Profile schema becomes exactly `6`; existing introduction thresholds remain config `2`, defaults `3`, shell `4`, hooks `5`, and Mise packages are introduced at schema `6`.
- Mise is part of provider/category `packages`; do not introduce a top-level `tools` provider or CLI category.
- Capture only the `[tools]` subtree of the primary user-global Mise config selected by `MISE_GLOBAL_CONFIG_FILE`, then `MISE_CONFIG_DIR`, then `XDG_CONFIG_HOME`, then `$HOME/.config/mise/config.toml`.
- Do not capture project-local Mise config, global `[env]`, `[settings]`, tasks, `[tool_alias]`, install caches, lockfiles, or resolved versions.
- Preserve configured requests such as `"24"`, `"latest"`, exact pins, arrays, and valid structured tool options; do not resolve them to concrete installed versions.
- Missing saved Mise tools may be added; differing target declarations are conflicts and must not be overwritten; target-only tools are never removed.
- `--force` remains Shell-only for this milestone.
- Restore must not mutate through a symlinked global config or any symlinked existing parent directory.
- Do not reserialize the whole target Mise config and do not create a persistent Blueprint-owned Mise overlay.
- `postinstall` is restored, not stripped or skipped; the install operation is `RiskHigh` if any newly added declaration contains `postinstall`, otherwise `RiskLow`.
- The generated Mise config write is `RiskMedium`.
- Literal obvious secrets inside a Mise tool declaration must not be written into the profile; fail capture/check with a path-specific message rather than redacting and changing semantics.
- Run the existing full verification commands before declaring completion: `go test ./...`, `go vet ./...`, `go build ./cmd/omarchy-blueprint`.

---

## File Structure

Create or modify these units. Keep Mise-specific logic out of the already-large generic package file where possible.

- Modify `internal/profile/profile.go`
  - schema 6;
  - `MiseTool` / `MiseTools`;
  - `Packages.Mise`;
  - load/save `packages/mise.toml`.
- Modify `internal/profile/profile_test.go`
  - schema migration and deterministic Mise profile round-trips.
- Create `internal/providers/packages/mise.go`
  - global path resolution;
  - TOML read/normalize/equality/summary;
  - secret validation;
  - append-only candidate generation;
  - symlink-parent safety helpers.
- Create `internal/providers/packages/mise_test.go`
  - focused Mise unit tests.
- Modify `internal/providers/packages/provider.go`
  - detect/diff/plan/verify/check integration;
  - include/exclude/reference semantics;
  - convert `Plan` to a provider method returning an error.
- Modify `internal/providers/packages/provider_test.go`
  - mixed official/AUR/Mise behavior and exclusions.
- Modify `internal/app/app.go`
  - injectable `MiseGlobalConfig` dependency and default.
- Modify `internal/app/providers.go`
  - central Packages provider construction using the resolved Mise config path.
- Modify `internal/app/app_test.go`
  - app-level three-source capture/restore/status fixture.
- Modify `README.md`
  - document Mise as third package source.
- Modify `ROADMAP.md`
  - mark semantic Mise Packages milestone and remove any separate Tools-provider implication.
- Create `docs/adr/0011-mise-packages.md`
  - record the provider-boundary and restore decisions.

No restore-executor change is expected: the existing generated `FileWrite`, hash preconditions, backups, atomic rename, and dependency graph are sufficient.

---

### Task 1: Schema 6 and Mise Profile Persistence

**Files:**
- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/profile_test.go`

**Interfaces:**
- Produces:
  ```go
  type MiseTool map[string]any
  type MiseTools map[string]MiseTool
  ```
- Produces: `profile.Packages.Mise profile.MiseTools`
- Produces: `const misePackagesSchema = 6`
- Persists: `packages/mise.toml`

- [ ] **Step 1: Write failing schema-migration and round-trip tests**

Add tests equivalent to:

```go
func TestSchema5LoadsAsSchema6WithMiseEmpty(t *testing.T) {
    dir := t.TempDir()
    profileTOML := `schema = 5

[profile]
name = "schema5"
created_at = 2026-09-08T00:00:00Z
updated_at = 2026-09-08T00:00:00Z
`
    if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
        t.Fatal(err)
    }
    if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
        t.Fatal(err)
    }

    got, err := Load(dir)
    if err != nil {
        t.Fatal(err)
    }
    if got.Manifest.Schema != 6 {
        t.Fatalf("schema=%d want=6", got.Manifest.Schema)
    }
    if len(got.Packages.Mise) != 0 {
        t.Fatalf("mise=%#v want empty", got.Packages.Mise)
    }
}

func TestMisePackagesRoundTripSchema6(t *testing.T) {
    dir := t.TempDir()
    d := New("main", time.Unix(0, 0))
    d.Manifest.Capture.Packages = true
    d.Packages.Mise = MiseTools{
        "node": {"version": "24"},
        "python": {"version": []any{"3.12", "3.13"}},
        "npm:@anthropic-ai/claude-code": {"version": "latest"},
        "foo": {
            "version": "2",
            "postinstall": "foo setup",
            "install_env": map[string]any{"FOO_MODE": "portable"},
        },
    }

    if err := Save(dir, d); err != nil {
        t.Fatal(err)
    }
    loaded, err := Load(dir)
    if err != nil {
        t.Fatal(err)
    }
    if !reflect.DeepEqual(loaded.Packages.Mise, d.Packages.Mise) {
        t.Fatalf("mise=%#v want=%#v", loaded.Packages.Mise, d.Packages.Mise)
    }
}
```

Also add a deterministic-save assertion:

```go
first, err := os.ReadFile(filepath.Join(dir, "packages", "mise.toml"))
if err != nil { t.Fatal(err) }
if err := Save(dir, d); err != nil { t.Fatal(err) }
second, err := os.ReadFile(filepath.Join(dir, "packages", "mise.toml"))
if err != nil { t.Fatal(err) }
if !bytes.Equal(first, second) {
    t.Fatalf("mise.toml is not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run:

```bash
go test ./internal/profile -run 'TestSchema5LoadsAsSchema6WithMiseEmpty|TestMisePackagesRoundTripSchema6' -count=1
```

Expected: FAIL because schema is still 5 and the Mise profile types/storage do not exist.

- [ ] **Step 3: Add schema and profile types**

In `internal/profile/profile.go`:

```go
const Schema = 6

const (
    configSchema       = 2
    defaultsSchema     = 3
    shellSchema        = 4
    hooksSchema        = 5
    misePackagesSchema = 6
)

type MiseTool map[string]any
type MiseTools map[string]MiseTool

type Packages struct {
    Official        []string  `json:"official"`
    AUR             []string  `json:"aur"`
    Mise            MiseTools `json:"mise,omitempty" toml:"-"`
    MachineSpecific []string  `json:"machine_specific,omitempty"`
    Excluded        []string  `json:"excluded,omitempty"`
    Installed       []string  `json:"-" toml:"-"`
}
```

Add a private profile-file envelope:

```go
type misePackagesFile struct {
    Tools MiseTools `toml:"tools"`
}
```

- [ ] **Step 4: Load `packages/mise.toml` only for schema 6+**

After existing package text-file loads:

```go
if loadedSchema >= misePackagesSchema {
    b, err := os.ReadFile(filepath.Join(dir, "packages", "mise.toml"))
    if err == nil {
        var file misePackagesFile
        if err := toml.Unmarshal(b, &file); err != nil {
            return d, fmt.Errorf("parse packages/mise.toml: %w", err)
        }
        if file.Tools == nil {
            file.Tools = MiseTools{}
        }
        d.Packages.Mise = file.Tools
    } else if !errors.Is(err, os.ErrNotExist) {
        return d, err
    }
}
if d.Packages.Mise == nil {
    d.Packages.Mise = MiseTools{}
}
```

Schema 6 treats a missing `packages/mise.toml` as an empty Mise set for compatibility with partially upgraded working trees; malformed content still fails.

- [ ] **Step 5: Save deterministic `packages/mise.toml`**

Before constructing `writes`:

```go
misePackages, err := toml.Marshal(misePackagesFile{Tools: d.Packages.Mise})
if err != nil {
    return err
}
```

Add:

```go
{filepath.Join(dir, "packages", "mise.toml"), misePackages},
```

The repository already uses `go-toml/v2` v2.2.4, whose map encoder sorts map entries by key. Do not add or upgrade a TOML dependency for this milestone.

- [ ] **Step 6: Update schema-threshold tests**

Where `TestLoaderThresholdsUseIntroductionVersions` asserts provider introduction versions, require:

```go
if configSchema != 2 ||
    defaultsSchema != 3 ||
    shellSchema != 4 ||
    hooksSchema != 5 ||
    misePackagesSchema != 6 {
    t.Fatalf("unexpected schema introduction versions")
}
```

Update all existing `want 5` schema expectations to `want 6` where they mean the current profile schema, without changing historical fixture schema values.

- [ ] **Step 7: Run profile tests**

Run:

```bash
go test ./internal/profile -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/profile/profile.go internal/profile/profile_test.go
git commit -m "feat: add schema 6 mise package state"
```

---

### Task 2: Global Mise Path Resolution and Declaration Normalization

**Files:**
- Create: `internal/providers/packages/mise.go`
- Create: `internal/providers/packages/mise_test.go`

**Interfaces:**
- Consumes: `profile.MiseTool`, `profile.MiseTools`
- Produces:
  ```go
  func ResolveMiseGlobalConfigPath() (string, error)
  func ReadMiseTools(path string) (profile.MiseTools, error)
  func NormalizeMiseTools(raw map[string]any) (profile.MiseTools, error)
  func NormalizeMiseTool(id string, raw any) (profile.MiseTool, error)
  func EqualMiseTool(left, right profile.MiseTool) bool
  func EncodeMiseTools(tools profile.MiseTools) ([]byte, error)
  func SummarizeMiseTool(tool profile.MiseTool) string
  ```

- [ ] **Step 1: Write path-resolution tests**

Use `t.Setenv` so each branch is explicit:

```go
func TestResolveMiseGlobalConfigPathPrecedence(t *testing.T) {
    home := t.TempDir()
    t.Setenv("HOME", home)
    t.Setenv("XDG_CONFIG_HOME", "")
    t.Setenv("MISE_CONFIG_DIR", "")
    t.Setenv("MISE_GLOBAL_CONFIG_FILE", "")

    got, err := ResolveMiseGlobalConfigPath()
    if err != nil { t.Fatal(err) }
    want := filepath.Join(home, ".config", "mise", "config.toml")
    if got != want { t.Fatalf("got=%q want=%q", got, want) }

    xdg := filepath.Join(t.TempDir(), "xdg")
    t.Setenv("XDG_CONFIG_HOME", xdg)
    got, _ = ResolveMiseGlobalConfigPath()
    if want := filepath.Join(xdg, "mise", "config.toml"); got != want {
        t.Fatalf("got=%q want=%q", got, want)
    }

    miseDir := filepath.Join(t.TempDir(), "mise-config")
    t.Setenv("MISE_CONFIG_DIR", miseDir)
    got, _ = ResolveMiseGlobalConfigPath()
    if want := filepath.Join(miseDir, "config.toml"); got != want {
        t.Fatalf("got=%q want=%q", got, want)
    }

    explicit := filepath.Join(t.TempDir(), "global.toml")
    t.Setenv("MISE_GLOBAL_CONFIG_FILE", explicit)
    got, _ = ResolveMiseGlobalConfigPath()
    if got != explicit { t.Fatalf("got=%q want=%q", got, explicit) }
}
```

- [ ] **Step 2: Write normalization tests covering current Mise declaration forms**

Add table-driven cases:

```go
func TestNormalizeMiseTool(t *testing.T) {
    tests := []struct {
        id   string
        raw  any
        want profile.MiseTool
    }{
        {"node", "24", profile.MiseTool{"version": "24"}},
        {"python", []any{"3.12", "3.13"}, profile.MiseTool{"version": []any{"3.12", "3.13"}}},
        {"npm:@anthropic-ai/claude-code", "latest", profile.MiseTool{"version": "latest"}},
        {"foo", map[string]any{
            "version": "2",
            "postinstall": "foo setup",
            "install_env": map[string]any{"FOO_MODE": "portable"},
        }, profile.MiseTool{
            "version": "2",
            "postinstall": "foo setup",
            "install_env": map[string]any{"FOO_MODE": "portable"},
        }},
        {"omitted-version", map[string]any{"postinstall": "echo ok"}, profile.MiseTool{
            "version": "latest",
            "postinstall": "echo ok",
        }},
    }

    for _, tt := range tests {
        t.Run(tt.id, func(t *testing.T) {
            got, err := NormalizeMiseTool(tt.id, tt.raw)
            if err != nil { t.Fatal(err) }
            if !reflect.DeepEqual(got, tt.want) {
                t.Fatalf("got=%#v want=%#v", got, tt.want)
            }
        })
    }
}
```

Add rejection cases for empty IDs, whitespace/control IDs, nil values, and invalid nested values.

- [ ] **Step 3: Write `ReadMiseTools` tests**

Fixture:

```toml
[tools]
node = "24"
python = ["3.12", "3.13"]
"npm:@anthropic-ai/claude-code" = "latest"
foo = { version = "2", postinstall = "foo setup" }

[env]
UNCHANGED = "not package state"

[settings]
jobs = 4
```

Assert exactly four normalized tools and no `[env]` / `[settings]` state in the result.

Also test:

```text
missing file → empty map, nil error
malformed TOML → error
missing [tools] → empty map
```

- [ ] **Step 4: Run tests and verify failure**

Run:

```bash
go test ./internal/providers/packages -run 'TestResolveMise|TestNormalizeMise|TestReadMise' -count=1
```

Expected: FAIL because `mise.go` does not exist.

- [ ] **Step 5: Implement global path resolution**

```go
func ResolveMiseGlobalConfigPath() (string, error) {
    if path := strings.TrimSpace(os.Getenv("MISE_GLOBAL_CONFIG_FILE")); path != "" {
        return filepath.Clean(path), nil
    }

    configDir := strings.TrimSpace(os.Getenv("MISE_CONFIG_DIR"))
    if configDir == "" {
        if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
            configDir = filepath.Join(xdg, "mise")
        } else {
            home, err := os.UserHomeDir()
            if err != nil {
                return "", err
            }
            configDir = filepath.Join(home, ".config", "mise")
        }
    }
    return filepath.Join(configDir, "config.toml"), nil
}
```

- [ ] **Step 6: Implement recursive normalization**

Use an envelope for source parsing:

```go
type rawMiseFile struct {
    Tools map[string]any `toml:"tools"`
}
```

`NormalizeMiseTool` behavior:

```go
func NormalizeMiseTool(id string, raw any) (profile.MiseTool, error) {
    if err := validateMiseID(id); err != nil {
        return nil, err
    }

    switch value := raw.(type) {
    case string:
        return profile.MiseTool{"version": value}, nil
    case []any:
        copied, err := normalizeMiseValue(value)
        if err != nil { return nil, err }
        return profile.MiseTool{"version": copied}, nil
    case map[string]any:
        copied, err := normalizeMiseMap(value)
        if err != nil { return nil, err }
        if _, ok := copied["version"]; !ok {
            copied["version"] = "latest"
        }
        return profile.MiseTool(copied), nil
    default:
        return nil, fmt.Errorf("mise tool %q has unsupported declaration type %T", id, raw)
    }
}
```

`normalizeMiseValue` accepts TOML scalar values, recursively copies `[]any` and `map[string]any`, and errors on nil/unsupported runtime types.

Do **not** resolve a version using Mise.

- [ ] **Step 7: Implement equality and deterministic encoding**

```go
func EqualMiseTool(left, right profile.MiseTool) bool {
    return reflect.DeepEqual(left, right)
}

type miseFile struct {
    Tools profile.MiseTools `toml:"tools"`
}

func EncodeMiseTools(tools profile.MiseTools) ([]byte, error) {
    if tools == nil {
        tools = profile.MiseTools{}
    }
    return toml.Marshal(miseFile{Tools: tools})
}
```

`go-toml/v2` v2.2.4 sorts map entries by key; keep the existing dependency.

- [ ] **Step 8: Implement compact summaries**

Make `version`-only declarations concise:

```go
func SummarizeMiseTool(tool profile.MiseTool) string {
    if len(tool) == 1 {
        if version, ok := tool["version"]; ok {
            return summarizeMiseValue(version)
        }
    }
    b, err := toml.Marshal(map[string]any(tool))
    if err != nil {
        return fmt.Sprintf("%v", map[string]any(tool))
    }
    return strings.TrimSpace(strings.ReplaceAll(string(b), "\n", " "))
}
```

The test should assert deterministic summaries rather than a Go map's `%v` ordering.

- [ ] **Step 9: Run focused and package tests**

Run:

```bash
go test ./internal/providers/packages -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/providers/packages/mise.go internal/providers/packages/mise_test.go
git commit -m "feat: parse global mise package declarations"
```

---

### Task 3: Mise Secret Guard and Executable-Risk Detection

**Files:**
- Modify: `internal/providers/packages/mise.go`
- Modify: `internal/providers/packages/mise_test.go`

**Interfaces:**
- Produces:
  ```go
  func ValidateMiseSecrets(tools profile.MiseTools) error
  func MiseToolHasPostinstall(tool profile.MiseTool) bool
  func MiseToolsHavePostinstall(tools profile.MiseTools) bool
  ```

- [ ] **Step 1: Write failing literal-secret tests**

```go
func TestValidateMiseSecretsRejectsLiteralSensitiveValues(t *testing.T) {
    tools := profile.MiseTools{
        "github:example/private": {
            "version": "latest",
            "install_env": map[string]any{
                "GITHUB_TOKEN": "ghp_literal_secret",
            },
        },
    }

    err := ValidateMiseSecrets(tools)
    if err == nil ||
        !strings.Contains(err.Error(), "github:example/private") ||
        !strings.Contains(err.Error(), "install_env.GITHUB_TOKEN") {
        t.Fatalf("err=%v", err)
    }
}

func TestValidateMiseSecretsAllowsEnvironmentReferences(t *testing.T) {
    tools := profile.MiseTools{
        "github:example/private": {
            "version": "latest",
            "install_env": map[string]any{
                "GITHUB_TOKEN": "{{ env.GITHUB_TOKEN }}",
            },
        },
    }
    if err := ValidateMiseSecrets(tools); err != nil {
        t.Fatal(err)
    }
}
```

Also test case-insensitive keys like `api_key`, `Private-Key`, `password`.

- [ ] **Step 2: Write postinstall detection tests**

```go
func TestMiseToolsHavePostinstall(t *testing.T) {
    clean := profile.MiseTools{"node": {"version": "24"}}
    risky := profile.MiseTools{"foo": {"version": "2", "postinstall": "foo setup"}}

    if MiseToolsHavePostinstall(clean) {
        t.Fatal("clean tools reported postinstall")
    }
    if !MiseToolsHavePostinstall(risky) {
        t.Fatal("postinstall was not detected")
    }
}
```

- [ ] **Step 3: Run tests and verify failure**

Run:

```bash
go test ./internal/providers/packages -run 'TestValidateMiseSecrets|TestMiseToolsHavePostinstall' -count=1
```

Expected: FAIL because helpers do not exist.

- [ ] **Step 4: Implement narrow sensitive-key validation**

Use a normalized key helper:

```go
func sensitiveMiseKey(key string) bool {
    normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
    switch normalized {
    case "token", "secret", "password", "passwd",
        "credential", "credentials", "private_key",
        "api_key", "apikey":
        return true
    }
    return strings.HasSuffix(normalized, "_token") ||
        strings.HasSuffix(normalized, "_secret") ||
        strings.HasSuffix(normalized, "_password") ||
        strings.HasSuffix(normalized, "_api_key")
}
```

Allow template references:

```go
func miseValueIsReference(value string) bool {
    trimmed := strings.TrimSpace(value)
    return strings.Contains(trimmed, "{{") && strings.Contains(trimmed, "}}")
}
```

Walk maps/arrays recursively while carrying a dotted path.

Do not redact a literal and continue.

- [ ] **Step 5: Implement `postinstall` detection**

Recursively inspect map keys case-insensitively:

```go
func MiseToolHasPostinstall(tool profile.MiseTool) bool {
    return miseValueHasKey(map[string]any(tool), "postinstall")
}
```

- [ ] **Step 6: Run package tests**

```bash
go test ./internal/providers/packages -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/packages/mise.go internal/providers/packages/mise_test.go
git commit -m "feat: validate mise package safety metadata"
```

---

### Task 4: Detect, Diff, Verify, and Package-source Identity

**Files:**
- Modify: `internal/providers/packages/provider.go`
- Modify: `internal/providers/packages/provider_test.go`
- Modify: `internal/providers/packages/mise_test.go`

**Interfaces:**
- Change provider shape to:
  ```go
  type Provider struct {
      Runner           command.Runner
      MiseGlobalConfig string
  }
  ```
- `Provider.Detect(ctx)` returns `profile.Packages` including `Mise`.
- Existing free `Diff` and `Verify` remain.
- `packageNames` remains Pacman-only equivalence.

- [ ] **Step 1: Write a detection test combining Pacman and Mise**

Create a runner that can return different Pacman outputs by arg instead of the existing one-output `queryRunner`, for example:

```go
type mapRunner struct {
    outputs map[string]string
}

func (r mapRunner) Run(_ context.Context, name string, args ...string) (string, error) {
    key := name + " " + strings.Join(args, " ")
    return r.outputs[key], nil
}
```

Fixture global config:

```toml
[tools]
node = "24"
"npm:@anthropic-ai/claude-code" = "latest"
```

Assert `Detect` returns official/AUR as before plus normalized `Mise`.

- [ ] **Step 2: Write semantic diff tests**

```go
func TestDiffIncludesMiseMissingExtraAndModified(t *testing.T) {
    saved := profile.Packages{Mise: profile.MiseTools{
        "node": {"version": "24"},
        "python": {"version": "3.12"},
    }}
    current := profile.Packages{Mise: profile.MiseTools{
        "node": {"version": "22"},
        "bun": {"version": "latest"},
    }}

    changes := Diff(saved, current)
    // Assert:
    // + mise package bun
    // ~ mise package node
    // - mise package python
}
```

- [ ] **Step 3: Write source-identity regression**

```go
func TestMiseDoesNotSatisfyPacmanPackageByName(t *testing.T) {
    saved := profile.Packages{Official: []string{"node"}}
    current := profile.Packages{Mise: profile.MiseTools{
        "node": {"version": "24"},
    }}

    if changes := Diff(saved, current); len(changes) == 0 {
        t.Fatal("mise:node must not satisfy official:node")
    }
    if verification := Verify(saved, current); verification.OK {
        t.Fatal("mise:node must not verify official:node")
    }
}
```

Keep the existing official↔AUR equivalence test unchanged.

- [ ] **Step 4: Write Mise verification tests**

```go
func TestVerifyMiseRequiresSavedDeclarationAndIgnoresExtras(t *testing.T) {
    saved := profile.Packages{Mise: profile.MiseTools{
        "node": {"version": "24"},
    }}

    equalPlusExtra := profile.Packages{Mise: profile.MiseTools{
        "node": {"version": "24"},
        "bun": {"version": "latest"},
    }}
    if got := Verify(saved, equalPlusExtra); !got.OK {
        t.Fatalf("verification=%#v", got)
    }

    changed := profile.Packages{Mise: profile.MiseTools{
        "node": {"version": "22"},
    }}
    if got := Verify(saved, changed); got.OK {
        t.Fatalf("verification=%#v", got)
    }
}
```

- [ ] **Step 5: Run tests and verify failure**

```bash
go test ./internal/providers/packages -run 'Test.*Mise' -count=1
```

Expected: FAIL because provider detection/diff/verify do not yet include Mise.

- [ ] **Step 6: Extend `Provider.Detect`**

After Pacman queries:

```go
mise, err := ReadMiseTools(p.MiseGlobalConfig)
if err != nil {
    return profile.Packages{}, fmt.Errorf("detect global mise packages: %w", err)
}
if err := ValidateMiseSecrets(mise); err != nil {
    return profile.Packages{}, err
}

return classify(profile.Packages{
    Official:  official,
    AUR:       aur,
    Installed: installed,
    Mise:      mise,
}), nil
```

If `MiseGlobalConfig` is empty in an old unit test, treat Mise as empty so existing tests do not need filesystem setup:

```go
if p.MiseGlobalConfig != "" {
    // read Mise
}
```

- [ ] **Step 7: Extend `Diff`**

Add a dedicated helper:

```go
func diffMise(saved, current profile.MiseTools) []model.Change
```

Rules:

```text
target-only → ChangeAdd
saved-only → ChangeRemove
both different → ChangeModify
both equal → no change
```

Set:

```go
Provider: "packages"
Kind:     "mise"
Name:     id
```

Append Mise changes after existing official/AUR changes, then retain the existing stable `(Kind, Name)` sort.

- [ ] **Step 8: Extend `Verify`**

After official/AUR checks:

```go
for id, desired := range saved.Mise {
    actual, ok := current.Mise[id]
    if !ok || !EqualMiseTool(desired, actual) {
        missing = append(missing, "mise:"+id)
    }
}
sort.Strings(missing)
```

- [ ] **Step 9: Keep cross-source equivalence scoped**

Do **not** add `packages.Mise` to `packageNames`.

- [ ] **Step 10: Run package tests**

```bash
go test ./internal/providers/packages -count=1
```

Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/providers/packages/provider.go internal/providers/packages/provider_test.go internal/providers/packages/mise_test.go
git commit -m "feat: detect and diff mise packages"
```

---

### Task 5: Extend Package Include/Exclude Policy to Mise

**Files:**
- Modify: `internal/providers/packages/provider.go`
- Modify: `internal/providers/packages/provider_test.go`

**Interfaces:**
- `ApplyExclusions`, `Exclude`, `Include`, `resolveRef`, and `ValidateExclusions` accept `mise:<id>`.
- `package:<id>` can resolve official, AUR, or Mise.
- Excluded Mise declaration remains stored in `Packages.Mise`.

- [ ] **Step 1: Write backend-qualified exclude/include test**

```go
func TestExcludeAndIncludeMiseReferenceRetainsDeclaration(t *testing.T) {
    id := "npm:@anthropic-ai/claude-code"
    original := profile.Packages{
        Mise: profile.MiseTools{
            id: {"version": "latest"},
        },
    }

    excluded, changed, err := Exclude(original, []string{"mise:" + id})
    if err != nil { t.Fatal(err) }
    if !reflect.DeepEqual(changed, []string{"mise:" + id}) {
        t.Fatalf("changed=%#v", changed)
    }
    if _, ok := excluded.Mise[id]; !ok {
        t.Fatal("excluded mise declaration was discarded")
    }
    if !reflect.DeepEqual(excluded.Excluded, changed) {
        t.Fatalf("excluded=%#v", excluded.Excluded)
    }

    view := ApplyExclusions(excluded, excluded.Excluded)
    if _, ok := view.Mise[id]; ok {
        t.Fatal("excluded mise package remained in managed view")
    }

    included, changed, err := Include(excluded, []string{"mise:" + id})
    if err != nil { t.Fatal(err) }
    if _, ok := included.Mise[id]; !ok || len(included.Excluded) != 0 {
        t.Fatalf("included=%#v", included)
    }
}
```

- [ ] **Step 2: Write generic ambiguity test**

```go
func TestGenericPackageReferenceCanBeAmbiguousWithMise(t *testing.T) {
    packages := profile.Packages{
        Official: []string{"node"},
        Mise: profile.MiseTools{
            "node": {"version": "24"},
        },
    }

    _, _, err := Exclude(packages, []string{"package:node"})
    if err == nil || !strings.Contains(err.Error(), "official:node") ||
        !strings.Contains(err.Error(), "mise:node") {
        t.Fatalf("err=%v", err)
    }
}
```

- [ ] **Step 3: Write validation test for stored excluded Mise metadata**

Assert:

```go
ValidateExclusions(profile.Packages{
    Excluded: []string{"mise:missing"},
})
```

fails because the declaration required for later Include is not stored.

Assert a stored declaration plus `mise:<id>` succeeds.

- [ ] **Step 4: Run tests and verify failure**

```bash
go test ./internal/providers/packages -run 'Test.*MiseReference|TestGenericPackageReferenceCanBeAmbiguousWithMise|TestValidateExclusions' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Add source-aware name validation**

Keep current strict Pacman package-name rules for `official` / `aur`.

For Mise:

```go
func validMiseRefName(name string) bool {
    if strings.TrimSpace(name) != name || name == "" {
        return false
    }
    for _, r := range name {
        if unicode.IsSpace(r) || unicode.IsControl(r) {
            return false
        }
    }
    return true
}
```

Because `splitRef` already uses `strings.Cut`, only the first colon is consumed.

- [ ] **Step 6: Extend `ApplyExclusions`**

```go
case "mise":
    packages.Mise = cloneMiseWithout(packages.Mise, name)
```

Do not mutate the original map by reference.

- [ ] **Step 7: Change `Exclude` / `Include` behavior**

For `mise`:

```text
Exclude:
- add mise:<id> to Excluded
- keep result.Mise[id] stored

Include:
- remove mise:<id> from Excluded
- do not synthesize or alter result.Mise[id]
```

For official/AUR retain current list-removal/list-restoration behavior.

- [ ] **Step 8: Extend `resolveRef` candidates**

Candidate sources:

```go
official:<name>
aur:<name>
mise:<name>
```

`package:<name>` is valid when the name is valid for at least one candidate source. Return an ambiguity error listing exact candidates when more than one matches.

- [ ] **Step 9: Extend `ValidateExclusions`**

Allow `mise`.

For each `mise:<id>`:

```go
if _, ok := packages.Mise[id]; !ok {
    return fmt.Errorf("excluded mise package %s has no stored declaration", id)
}
```

Do not reject overlap between stored `Mise[id]` and `Excluded`, because storage retention is intentional. Continue rejecting official/AUR managed+excluded overlap.

- [ ] **Step 10: Preserve excluded Mise declaration during capture**

In the app/package capture path, before replacing `d.Packages`:

```go
current = packagesprovider.ApplyExclusions(current, d.Packages.Excluded)
current = packagesprovider.PreserveExcludedMise(current, d.Packages)
```

Define:

```go
func PreserveExcludedMise(current, previous profile.Packages) profile.Packages
```

which copies previous stored declarations for every `mise:<id>` exclusion into the new captured state.

This prevents excluded live changes from churning the profile.

- [ ] **Step 11: Run package tests**

```bash
go test ./internal/providers/packages -count=1
```

Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add internal/providers/packages/provider.go internal/providers/packages/provider_test.go
git commit -m "feat: support mise package exclusions"
```

---

### Task 6: Append-only Mise Config Candidate and Restore-path Safety

**Files:**
- Modify: `internal/providers/packages/mise.go`
- Modify: `internal/providers/packages/mise_test.go`

**Interfaces:**
- Produces:
  ```go
  type MiseConfigSnapshot struct {
      Exists bool
      Bytes  []byte
      Hash   string
      Mode   os.FileMode
  }

  func ReadMiseConfigSnapshot(path string) (MiseConfigSnapshot, error)
  func BuildMiseAppendCandidate(
      existing []byte,
      current profile.MiseTools,
      additions profile.MiseTools,
  ) ([]byte, error)
  func ValidateMiseMutationPath(path string) error
  ```

- [ ] **Step 1: Write candidate-preservation test**

Existing bytes:

```toml
# user comment
[tools]
node = "24"

[env]
WORK = "1"

[settings]
jobs = 3
```

Add `npm:@anthropic-ai/claude-code`.

Assertions:

```go
candidate, err := BuildMiseAppendCandidate(existing, current, additions)
if err != nil { t.Fatal(err) }

if !bytes.HasPrefix(candidate, existing) {
    t.Fatal("existing config bytes were rewritten")
}

parsed, err := ReadMiseToolsFromBytes(candidate)
if err != nil { t.Fatal(err) }

if !EqualMiseTool(parsed["node"], current["node"]) {
    t.Fatal("existing node declaration changed")
}
if !EqualMiseTool(parsed[id], additions[id]) {
    t.Fatal("addition not present")
}
```

- [ ] **Step 2: Write append-incompatible inline-root test**

Existing:

```toml
tools = { node = "24" }

[settings]
jobs = 3
```

Attempt to append `[tools.python]`.

Expected:

```text
error
candidate not returned
original bytes unchanged
```

The error should mention that the existing Mise config cannot be safely extended without rewriting it.

- [ ] **Step 3: Write malformed-target test**

Malformed TOML must make candidate generation fail before any `FileWrite` exists.

- [ ] **Step 4: Write symlink-destination and symlink-parent tests**

Create:

```text
real/
  config.toml
link.toml -> real/config.toml
```

`ValidateMiseMutationPath(link.toml)` must fail.

Then:

```text
external/
config-link -> external/
config-link/config.toml
```

`ValidateMiseMutationPath(config-link/config.toml)` must fail even if the final file is absent.

A missing normal directory hierarchy must succeed.

- [ ] **Step 5: Run focused tests and verify failure**

```bash
go test ./internal/providers/packages -run 'TestBuildMiseAppendCandidate|TestValidateMiseMutationPath' -count=1
```

Expected: FAIL.

- [ ] **Step 6: Implement a single-tool append encoder**

Build canonical appended child tables without re-marshalling the original document.

For one tool:

```go
func encodeMiseAppendTool(id string, tool profile.MiseTool) ([]byte, error) {
    single := struct {
        Tools profile.MiseTools `toml:"tools"`
    }{
        Tools: profile.MiseTools{id: tool},
    }
    return toml.Marshal(single)
}
```

Because this emits a self-contained `[tools.<id>]` tree and contains only one top-level tool key, concatenate the encoded blocks in sorted ID order. Reject an encoder result that unexpectedly contains anything outside the single tool subtree.

- [ ] **Step 7: Implement `BuildMiseAppendCandidate`**

Algorithm:

```text
parse existing first
assert normalized existing [tools] == supplied current
sort addition IDs
candidate = exact copy(existing)
ensure separator newline(s)
append each encoded single-tool block
parse candidate
normalize candidate [tools]
assert every original tool unchanged
assert every addition exactly matches
return candidate
```

Never return a best-effort candidate after a failed assertion.

- [ ] **Step 8: Implement snapshot hashing**

Use SHA-256:

```go
func hashBytes(data []byte) string {
    sum := sha256.Sum256(data)
    return hex.EncodeToString(sum[:])
}
```

`ReadMiseConfigSnapshot` uses `os.Lstat` and rejects symlink/special destination files. Missing is represented by `Exists=false`.

- [ ] **Step 9: Implement parent symlink walk**

Walk from the nearest existing ancestor down toward the destination parent using `os.Lstat`. Any existing component with `ModeSymlink` fails.

Do not use `filepath.EvalSymlinks`, because evaluating the path is exactly the behavior restore must avoid.

- [ ] **Step 10: Run package tests**

```bash
go test ./internal/providers/packages -count=1
```

Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/providers/packages/mise.go internal/providers/packages/mise_test.go
git commit -m "feat: build safe mise config updates"
```

---

### Task 7: Mise Restore Planning and Native Installation

**Files:**
- Modify: `internal/providers/packages/provider.go`
- Modify: `internal/providers/packages/provider_test.go`
- Modify: `internal/providers/packages/mise_test.go`

**Interfaces:**
- Change:
  ```go
  func Plan(...)
  ```
  to:
  ```go
  func (p Provider) Plan(
      saved, current profile.Packages,
      schema int,
      from, to string,
  ) (model.RestorePlan, error)
  ```
- Produces operations:
  ```text
  packages.mise.configure
  packages.mise.install
  ```

- [ ] **Step 1: Update existing package Plan tests to provider-method form**

Replace:

```go
plan := Plan(saved, current, 1, "4.0.0", "4.1.0")
```

with a helper:

```go
func planPackages(t *testing.T, p Provider, saved, current profile.Packages) model.RestorePlan {
    t.Helper()
    plan, err := p.Plan(saved, current, 6, "4.0.0", "4.1.0")
    if err != nil { t.Fatal(err) }
    return plan
}
```

Use `Provider{}` in old official/AUR-only tests.

- [ ] **Step 2: Write missing-tool plan test**

Set up an existing real global config with unrelated sections and current tools.

Saved adds:

```go
"npm:@anthropic-ai/claude-code": {"version": "latest"}
```

Assert plan has:

```text
packages.mise.configure
packages.mise.install
```

Configure operation:

```go
if !op.File.Generated ||
    op.File.Destination != config ||
    op.File.ExpectedHash == "" ||
    !op.File.Backup ||
    op.Risk != model.RiskMedium {
    t.Fatalf("configure op=%#v", op)
}
```

Install operation:

```go
wantCommand := []string{
    "mise", "-C", "/", "install",
    "npm:@anthropic-ai/claude-code",
}
if !reflect.DeepEqual(op.Command, wantCommand) {
    t.Fatalf("command=%#v want=%#v", op.Command, wantCommand)
}
if !reflect.DeepEqual(op.DependsOn, []string{"packages.mise.configure"}) {
    t.Fatalf("depends=%#v", op.DependsOn)
}
if op.Risk != model.RiskLow {
    t.Fatalf("risk=%s", op.Risk)
}
```

- [ ] **Step 3: Write postinstall risk test**

Saved missing tool:

```go
"foo": {
    "version": "2",
    "postinstall": "foo setup",
}
```

Assert configure remains `RiskMedium`, install becomes `RiskHigh`, and declaration is not skipped.

- [ ] **Step 4: Write conflict + partial-apply test**

Saved:

```text
node = 24          // target has 22 → conflict
python = 3.13      // target missing → safe addition
```

Assert:

```text
one skipped entry for mise:node
configure/install only python
target node bytes untouched in generated candidate
```

- [ ] **Step 5: Write target-only preservation test**

Saved has Node; target has same Node plus Bun. No removal operation exists; skipped plan explains:

```text
mise:bun
additional package left installed; removal disabled
```

- [ ] **Step 6: Write missing-config creation test**

No global file exists.

Assert configure `FileWrite` has:

```go
ExpectedMissing: true
Backup:          false
Generated:       true
Mode:            modePtr(0o644)
```

If the existing executor's generated-file default 0644 is relied on instead of explicit `Mode`, assert `Mode == nil` and document that generated writes default to 0644. Do not invent a second mode mechanism.

- [ ] **Step 7: Run restore-plan tests and verify failure**

```bash
go test ./internal/providers/packages -run 'TestPlan.*Mise|TestMise.*Plan' -count=1
```

Expected: FAIL.

- [ ] **Step 8: Implement Mise classification inside `Provider.Plan`**

Create:

```go
func classifyMiseRestore(
    saved, current profile.MiseTools,
) (
    additions profile.MiseTools,
    conflicts []string,
    extras []string,
)
```

Iterate IDs in sorted order.

- [ ] **Step 9: Preserve existing official/AUR planning**

Start the plan exactly as today:

```go
saved, current = classify(saved), classify(current)
current = ApplyExclusions(current, saved.Excluded)
plan := model.RestorePlan{...}
```

Keep official batch first and AUR per-package operations next.

Then append Mise conflict skips and extra-target skips.

- [ ] **Step 10: Build the generated config operation only when additions exist**

Before mutation:

```go
if err := ValidateMiseMutationPath(p.MiseGlobalConfig); err != nil {
    // convert all safe additions into skipped resources and do not emit write/install
}
snapshot, err := ReadMiseConfigSnapshot(p.MiseGlobalConfig)
```

For an existing file:

```go
candidate, err := BuildMiseAppendCandidate(snapshot.Bytes, current.Mise, additions)
```

For a missing file:

```go
candidate, err := EncodeMiseTools(additions)
```

Emit:

```go
configure := model.Operation{
    ID:       "packages.mise.configure",
    Provider: "packages",
    Action:   "configure",
    Resource: "mise:global-tools",
    Items:    sortedMiseIDs(additions),
    File: &model.FileWrite{
        Generated:   true,
        Content:     candidate,
        Destination: p.MiseGlobalConfig,
        SourceHash:  hashBytes(candidate),
        Backup:      snapshot.Exists,
    },
    Risk:       model.RiskMedium,
    Reversible: snapshot.Exists,
}
if snapshot.Exists {
    configure.File.ExpectedHash = snapshot.Hash
} else {
    configure.File.ExpectedMissing = true
}
```

- [ ] **Step 11: Emit native install operation**

```go
argv := []string{"mise", "-C", "/", "install"}
argv = append(argv, sortedMiseIDs(additions)...)

risk := model.RiskLow
if MiseToolsHavePostinstall(additions) {
    risk = model.RiskHigh
}

install := model.Operation{
    ID:         "packages.mise.install",
    Provider:   "packages",
    Action:     "install",
    Resource:   "mise:" + strings.Join(sortedMiseIDs(additions), ","),
    Items:      sortedMiseIDs(additions),
    Command:    argv,
    DependsOn:  []string{"packages.mise.configure"},
    Risk:       risk,
    Reversible: false,
}
```

- [ ] **Step 12: Convert mutation-construction failures into explicit skips**

A target safety/candidate incompatibility is not a profile parse error. Add one skip per intended addition:

```go
model.Skipped{
    Provider: "packages",
    Resource: "mise:" + id,
    Reason:   reason,
}
```

Malformed saved profile metadata remains an error.

- [ ] **Step 13: Run all package tests**

```bash
go test ./internal/providers/packages -count=1
```

Expected: PASS.

- [ ] **Step 14: Commit**

```bash
git add internal/providers/packages/provider.go internal/providers/packages/provider_test.go internal/providers/packages/mise_test.go
git commit -m "feat: restore mise packages conservatively"
```

---

### Task 8: Package Provider Check and App Dependency Wiring

**Files:**
- Modify: `internal/providers/packages/provider.go`
- Modify: `internal/providers/packages/provider_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Produces:
  ```go
  func (p Provider) Check(ctx context.Context, saved profile.Packages) error
  ```
- Adds:
  ```go
  Dependencies.MiseGlobalConfig func() (string, error)
  ```

- [ ] **Step 1: Write provider Check tests**

```go
func TestCheckRequiresMiseOnlyWhenProfileManagesMise(t *testing.T) {
    failing := runnerThatFailsMiseVersion{}

    p := Provider{Runner: failing, MiseGlobalConfig: filepath.Join(t.TempDir(), "config.toml")}

    if err := p.Check(context.Background(), profile.Packages{}); err != nil {
        t.Fatalf("empty mise profile should not require mise: %v", err)
    }

    saved := profile.Packages{Mise: profile.MiseTools{
        "node": {"version": "24"},
    }}
    if err := p.Check(context.Background(), saved); err == nil {
        t.Fatal("managed mise profile must require a working mise executable")
    }
}
```

The runner should still allow the Pacman checks used by `Detect`.

- [ ] **Step 2: Run focused test and verify failure**

```bash
go test ./internal/providers/packages -run TestCheckRequiresMiseOnlyWhenProfileManagesMise -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement `Provider.Check`**

```go
func (p Provider) Check(ctx context.Context, saved profile.Packages) error {
    if err := ValidateExclusions(saved); err != nil {
        return err
    }
    if err := ValidateMiseSecrets(saved.Mise); err != nil {
        return err
    }
    if p.MiseGlobalConfig != "" {
        if _, err := ReadMiseTools(p.MiseGlobalConfig); err != nil {
            return fmt.Errorf("validate global mise config: %w", err)
        }
    }
    if len(ApplyExclusions(saved, saved.Excluded).Mise) > 0 {
        if _, err := p.Runner.Run(ctx, "mise", "--version"); err != nil {
            return fmt.Errorf("mise is required to restore mise packages: %w", err)
        }
    }
    return nil
}
```

Do not run `mise --version` merely because target-only tools exist.

- [ ] **Step 4: Add the app dependency hook**

In `Dependencies`:

```go
MiseGlobalConfig func() (string, error)
```

In `Execute` defaults:

```go
if deps.MiseGlobalConfig == nil {
    deps.MiseGlobalConfig = packagesprovider.ResolveMiseGlobalConfigPath
}
```

- [ ] **Step 5: Centralize Packages provider creation**

In `internal/app/providers.go`:

```go
func (p packagesStateProvider) provider() (packagesprovider.Provider, error) {
    path, err := p.deps.MiseGlobalConfig()
    if err != nil {
        return packagesprovider.Provider{}, err
    }
    return packagesprovider.Provider{
        Runner:           p.deps.Runner,
        MiseGlobalConfig: path,
    }, nil
}
```

Use this helper in Capture, Diff, Plan, Verify, and Check.

`Plan` now handles the provider error-returning method:

```go
return provider.Plan(
    d.Packages,
    current,
    d.Manifest.Schema,
    d.Manifest.Omarchy.CapturedVersion,
    info.Version,
)
```

- [ ] **Step 6: Preserve excluded Mise state during app capture**

After detecting current and before assigning `d.Packages`:

```go
current = packagesprovider.ApplyExclusions(current, d.Packages.Excluded)
current = packagesprovider.PreserveExcludedMise(current, d.Packages)
```

- [ ] **Step 7: Make app fixtures deterministic**

Any app test using real `Dependencies` should inject a temp Mise config path:

```go
miseConfig := filepath.Join(t.TempDir(), "mise", "config.toml")
deps.MiseGlobalConfig = func() (string, error) {
    return miseConfig, nil
}
```

Do not let tests inspect the developer/CI user's real `~/.config/mise/config.toml`.

- [ ] **Step 8: Run app and provider tests**

```bash
go test ./internal/providers/packages ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/packages/provider.go internal/providers/packages/provider_test.go internal/app/app.go internal/app/providers.go internal/app/app_test.go
git commit -m "feat: wire mise packages into app state"
```

---

### Task 9: End-to-end Three-source Restore Regression

**Files:**
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Exercises the existing CLI surface:
  ```text
  capture packages
  status packages
  restore packages --dry-run
  restore packages --yes
  ```

- [ ] **Step 1: Add a runner that models Pacman/AUR/Mise mutations**

Extend or create a test runner with:

```go
type packageMachineRunner struct {
    official map[string]bool
    aur      map[string]bool
    commands [][]string
}
```

It must support:

```text
pacman -Qqen
pacman -Qqem
pacman -Qq
omarchy pkg add ...
omarchy pkg aur add ...
mise --version
mise -C / install ...
```

The runner records install argv but the Mise declaration itself is represented by the temp global config file written by the real restore executor.

- [ ] **Step 2: Build source state with all three package sources**

Source runner:

```text
official: git
AUR:      visual-studio-code-bin
```

Source Mise config:

```toml
# source comment
[tools]
node = "24"
"npm:@anthropic-ai/claude-code" = "latest"
foo = { version = "2", postinstall = "foo setup" }

[settings]
jobs = 4
```

Capture:

```go
if code, out := run("capture", "packages"); code != 0 {
    t.Fatalf("capture code=%d out=%s", code, out)
}
```

Assert `packages/mise.toml` contains normalized profile state and does not contain `[settings]`.

- [ ] **Step 3: Switch to a fresh target**

Target runner starts without captured official/AUR packages.

Target global Mise config contains:

```toml
# target-only config must survive
[tools]
bun = "latest"

[env]
TARGET_ONLY = "yes"
```

Point `deps.MiseGlobalConfig` at the target file.

- [ ] **Step 4: Assert pre-restore status reports drift**

```go
if code, out := run("status", "packages"); code != 2 {
    t.Fatalf("status code=%d out=%s", code, out)
}
```

Output should mention missing captured official/AUR/Mise packages and target-only `mise:bun`.

- [ ] **Step 5: Assert dry-run contains native operations**

Dry run must contain:

```text
omarchy pkg add git
omarchy pkg aur add visual-studio-code-bin
packages.mise.configure
mise -C / install node npm:@anthropic-ai/claude-code foo
```

The exact Mise ID order is lexical. Because `foo` has `postinstall`, the Mise install operation is high risk.

- [ ] **Step 6: Execute restore**

```go
if code, out := run("restore", "packages", "--yes"); code != 0 {
    t.Fatalf("restore code=%d out=%s", code, out)
}
```

Read target Mise config and assert:

- original prefix bytes remain unchanged;
- `[env] TARGET_ONLY` remains;
- `bun` remains;
- Node / Claude / foo declarations were appended semantically;
- no source `[settings] jobs = 4` was copied.

- [ ] **Step 7: Assert install argv**

Find the recorded command:

```go
[]string{
    "mise", "-C", "/", "install",
    "foo",
    "node",
    "npm:@anthropic-ai/claude-code",
}
```

Use the actual lexical sort produced by `sortedMiseIDs`.

- [ ] **Step 8: Assert post-restore status behavior**

Because target-only Bun remains configured, existing Packages semantics report it as an additional package. Therefore `status packages` remains drifted (`2`) unless Bun is explicitly excluded or captured.

Also assert `Verify` for saved desired state is OK because target-only packages do not fail verification.

This distinction mirrors existing official/AUR behavior.

- [ ] **Step 9: Add conflict regression**

Change target `node` to `"22"` and restore again. Assert:

```text
node remains 22
node is skipped as conflict
other missing safe Mise tools still restore
no command installs node on behalf of the profile
```

- [ ] **Step 10: Run the app test**

```bash
go test ./internal/app -run 'TestPackages.*Mise|TestMise.*Packages' -count=1
```

Expected: PASS.

- [ ] **Step 11: Run all app tests**

```bash
go test ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add internal/app/app_test.go
git commit -m "test: cover three-source package restore"
```

---

### Task 10: Documentation and Architecture Decision Record

**Files:**
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Create: `docs/adr/0011-mise-packages.md`

**Interfaces:**
- Documents the already-implemented behavior; no runtime interface change.

- [ ] **Step 1: Write ADR 0011**

Use this decision summary:

```markdown
# ADR 0011: Treat global Mise tools as Packages

## Status
Accepted

## Context
Omarchy uses Mise for fast-moving CLI tools and agents, and users may add
their own global Mise tools. Blueprint already models installation provenance
under Packages and application selection under Defaults.

## Decision
Mise is a third source under Packages, alongside official Pacman and AUR.
Schema 6 stores normalized primary-global `[tools]` declarations in
`packages/mise.toml`. Restore adds only missing declarations, preserves
conflicts and target-only tools, mutates the existing global config
append-only with atomic preconditions, then invokes `mise -C / install`.

Configured requests are preserved rather than replaced with resolved versions.
Valid structured options, including `postinstall`, are retained; an install
with `postinstall` is high risk.

## Consequences
- no new top-level Tools provider;
- Packages profiles become structurally richer;
- project-local Mise config remains outside this provider;
- effective multi-file global Mise state is future work.
```

- [ ] **Step 2: Update README package documentation**

Describe:

```text
packages/official.txt
packages/aur.txt
packages/mise.toml
```

Add the policy:

```text
Defaults selects applications; Packages restores installation provenance.
```

Mention target-only packages remain and same-ID/different Mise declarations are skipped.

- [ ] **Step 3: Update ROADMAP**

Record the Mise milestone under Packages rather than adding a standalone Tools provider.

Future Portable Resources should explicitly own project-local configs/repos rather than global package declarations.

- [ ] **Step 4: Search docs for contradictory package-source statements**

Run:

```bash
grep -RniE 'official.*AUR|AUR.*official|two package|package source|Mise|Tools provider' README.md ROADMAP.md docs/adr docs/superpowers/specs
```

Review each relevant hit and update only statements contradicted by schema-6 behavior.

- [ ] **Step 5: Run formatting/doc-independent tests**

```bash
gofmt -w internal/profile/profile.go \
  internal/profile/profile_test.go \
  internal/providers/packages/provider.go \
  internal/providers/packages/provider_test.go \
  internal/providers/packages/mise.go \
  internal/providers/packages/mise_test.go \
  internal/app/app.go \
  internal/app/providers.go \
  internal/app/app_test.go
```

Then:

```bash
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
```

Expected: all commands exit 0.

- [ ] **Step 6: Inspect git diff**

Run:

```bash
git status --short
git diff --check
git diff --stat
```

Expected:

- no whitespace errors;
- only intended source/tests/docs changed;
- no generated binaries, temp profiles, real Mise configs, or secrets.

- [ ] **Step 7: Commit docs**

```bash
git add README.md ROADMAP.md docs/adr/0011-mise-packages.md
git commit -m "docs: describe mise package provenance"
```

---

## Final Verification Matrix

Before opening the PR, run these focused regressions in addition to the full suite:

```bash
go test ./internal/profile -count=1
go test ./internal/providers/packages -count=1
go test ./internal/app -count=1

go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
```

Manually inspect one test-generated profile or fixture and confirm the desired shape:

```text
packages/
├── official.txt
├── aur.txt
├── mise.toml
├── machine-specific.txt
└── excluded.txt
```

`mise.toml` must contain only normalized `[tools]` package declarations.

## Real-machine Acceptance Pass

On the source Omarchy machine:

```bash
mise use --global node@24
mise use --global npm:@anthropic-ai/claude-code@latest

omarchy-blueprint --profile ~/omarchy-profile capture packages
cat ~/omarchy-profile/packages/mise.toml
```

Confirm the profile records configured requests, not resolved concrete versions.

On a fresh Omarchy target:

```bash
omarchy-blueprint --profile ~/omarchy-profile diff packages
omarchy-blueprint --profile ~/omarchy-profile restore packages --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore packages
```

Before approving restore, confirm the plan shows:

```text
official/AUR package operations as applicable
packages.mise.configure
mise -C / install ...
```

After restore:

```bash
mise config get --global tools
omarchy-blueprint --profile ~/omarchy-profile check
```

Confirm unrelated target global Mise sections and target-only tools were not removed or rewritten.

Finally, create a deliberate target conflict:

```toml
[tools]
node = "22"
```

against saved `"24"`, rerun restore, and confirm Blueprint preserves `"22"` and reports the conflict instead of overwriting it.

## PR Review Gate

Before merge, review the PR specifically for these blocker classes:

1. Any code that resolves floating Mise requests into concrete installed versions.
2. Any whole-file re-marshalling of the user's global Mise config.
3. Any target Mise declaration overwrite.
4. Any following/writing through symlinked destination parents.
5. Any accidental capture of `[env]`, `[settings]`, tasks, project configs, or install cache.
6. Any `postinstall` declaration being stripped or silently skipped.
7. Any literal secret being persisted to the Git-friendly profile.
8. Any addition of Mise IDs to Pacman name-only equivalence.
9. Any new top-level `tools` CLI/provider category.
10. Any install operation that can run before `packages.mise.configure`.

Only merge after exact-head CI passes the supported Go matrix.
