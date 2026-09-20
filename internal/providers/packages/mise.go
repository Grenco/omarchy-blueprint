package packages

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/pelletier/go-toml/v2"
)

type rawMiseFile struct {
	Tools map[string]any `toml:"tools"`
}

type miseFile struct {
	Tools profile.MiseTools `toml:"tools"`
}

// MiseConfigSnapshot captures the exact regular file that a generated write
// must still match when restore executes.
type MiseConfigSnapshot struct {
	Exists bool
	Bytes  []byte
	Hash   string
	Mode   os.FileMode
}

func ResolveMiseGlobalConfigPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("MISE_GLOBAL_CONFIG_FILE")); path != "" {
		return filepath.Clean(path), nil
	}
	if dir := strings.TrimSpace(os.Getenv("MISE_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "config.toml"), nil
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "mise", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mise", "config.toml"), nil
}

func ReadMiseTools(path string) (profile.MiseTools, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return profile.MiseTools{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ReadMiseToolsFromBytes(b)
}

func ReadMiseToolsFromBytes(b []byte) (profile.MiseTools, error) {
	var file rawMiseFile
	if err := toml.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("parse mise config: %w", err)
	}
	return NormalizeMiseTools(file.Tools)
}

func NormalizeMiseTools(raw map[string]any) (profile.MiseTools, error) {
	tools := make(profile.MiseTools, len(raw))
	for id, value := range raw {
		tool, err := NormalizeMiseTool(id, value)
		if err != nil {
			return nil, err
		}
		tools[id] = tool
	}
	return tools, nil
}

func NormalizeMiseTool(id string, raw any) (profile.MiseTool, error) {
	if err := validateMiseID(id); err != nil {
		return nil, err
	}
	switch value := raw.(type) {
	case string:
		return profile.MiseTool{"version": value}, nil
	case []any:
		copied, err := normalizeMiseValue(value)
		if err != nil {
			return nil, fmt.Errorf("mise tool %q: %w", id, err)
		}
		return profile.MiseTool{"version": copied}, nil
	case map[string]any:
		copied, err := normalizeMiseMap(value)
		if err != nil {
			return nil, fmt.Errorf("mise tool %q: %w", id, err)
		}
		if _, ok := copied["version"]; !ok {
			copied["version"] = "latest"
		}
		return profile.MiseTool(copied), nil
	default:
		return nil, fmt.Errorf("mise tool %q has unsupported declaration type %T", id, raw)
	}
}

func validateMiseID(id string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id {
		return fmt.Errorf("mise tool ID %q is empty or has surrounding whitespace", id)
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("mise tool ID %q contains whitespace or a control character", id)
		}
	}
	return nil
}

func normalizeMiseMap(value map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(value))
	for key, child := range value {
		if key == "" {
			return nil, errors.New("contains an empty key")
		}
		copied, err := normalizeMiseValue(child)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", key, err)
		}
		result[key] = copied
	}
	return result, nil
}

func normalizeMiseValue(value any) (any, error) {
	switch value := value.(type) {
	case string, bool, int64, float64:
		return value, nil
	case []any:
		result := make([]any, len(value))
		for i, child := range value {
			copied, err := normalizeMiseValue(child)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			result[i] = copied
		}
		return result, nil
	case map[string]any:
		return normalizeMiseMap(value)
	default:
		return nil, fmt.Errorf("unsupported TOML value type %T", value)
	}
}

func EqualMiseTool(left, right profile.MiseTool) bool { return reflect.DeepEqual(left, right) }

func EncodeMiseTools(tools profile.MiseTools) ([]byte, error) {
	if tools == nil {
		tools = profile.MiseTools{}
	}
	return toml.Marshal(miseFile{Tools: tools})
}

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

func summarizeMiseValue(value any) string {
	b, err := toml.Marshal(struct {
		Version any `toml:"version"`
	}{value})
	if err != nil {
		return fmt.Sprint(value)
	}
	return strings.TrimPrefix(strings.TrimSpace(string(b)), "version = ")
}

func ValidateMiseSecrets(tools profile.MiseTools) error {
	for _, id := range sortedMiseIDs(tools) {
		if err := validateMiseSecretsValue(tools[id], nil); err != nil {
			return fmt.Errorf("mise tool %s contains a literal sensitive value at %s; reference an environment variable or external secret source instead", id, err)
		}
	}
	return nil
}

func validateMiseSecretsValue(value any, path []string) error {
	switch value := value.(type) {
	case profile.MiseTool:
		return validateMiseSecretsValue(map[string]any(value), path)
	case map[string]any:
		for _, key := range sortedMapKeys(value) {
			child := value[key]
			childPath := append(append([]string{}, path...), key)
			if sensitiveMiseKey(key) && literalMiseScalar(child) {
				return errors.New(strings.Join(childPath, "."))
			}
			if err := validateMiseSecretsValue(child, childPath); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range value {
			if err := validateMiseSecretsValue(child, append(path, fmt.Sprintf("[%d]", i))); err != nil {
				return err
			}
		}
	}
	return nil
}

func sensitiveMiseKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	switch key {
	case "token", "secret", "password", "passwd", "credential", "credentials", "private_key", "api_key", "apikey":
		return true
	}
	return strings.HasSuffix(key, "_token") || strings.HasSuffix(key, "_secret") || strings.HasSuffix(key, "_password") || strings.HasSuffix(key, "_api_key")
}

func literalMiseScalar(value any) bool {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value) != "" && !(strings.Contains(value, "{{") && strings.Contains(value, "}}"))
	case int64, float64:
		return true
	}
	return false
}

func MiseToolHasPostinstall(tool profile.MiseTool) bool { return miseValueHasKey(tool, "postinstall") }

func MiseToolsHavePostinstall(tools profile.MiseTools) bool {
	for _, tool := range tools {
		if MiseToolHasPostinstall(tool) {
			return true
		}
	}
	return false
}

func miseValueHasKey(value any, target string) bool {
	switch value := value.(type) {
	case profile.MiseTool:
		return miseValueHasKey(map[string]any(value), target)
	case map[string]any:
		for key, child := range value {
			if strings.EqualFold(key, target) || miseValueHasKey(child, target) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if miseValueHasKey(child, target) {
				return true
			}
		}
	}
	return false
}

func ReadMiseConfigSnapshot(path string) (MiseConfigSnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return MiseConfigSnapshot{}, nil
	}
	if err != nil {
		return MiseConfigSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return MiseConfigSnapshot{}, fmt.Errorf("mise global config is a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return MiseConfigSnapshot{}, fmt.Errorf("mise global config is not a regular file: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return MiseConfigSnapshot{}, err
	}
	return MiseConfigSnapshot{Exists: true, Bytes: b, Hash: hashBytes(b), Mode: info.Mode()}, nil
}

func BuildMiseAppendCandidate(existing []byte, current, additions profile.MiseTools) ([]byte, error) {
	parsed, err := ReadMiseToolsFromBytes(existing)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(parsed, nonNilMiseTools(current)) {
		return nil, errors.New("existing Mise config tools do not match the supplied current declarations")
	}
	candidate := append([]byte{}, existing...)
	if len(additions) > 0 && len(candidate) > 0 {
		if candidate[len(candidate)-1] != '\n' {
			candidate = append(candidate, '\n')
		}
		candidate = append(candidate, '\n')
	}
	for _, id := range sortedMiseIDs(additions) {
		if _, exists := parsed[id]; exists {
			return nil, fmt.Errorf("mise tool %q already exists in the target config", id)
		}
		block, err := encodeMiseAppendTool(id, additions[id])
		if err != nil {
			return nil, err
		}
		candidate = append(candidate, block...)
	}
	validated, err := ReadMiseToolsFromBytes(candidate)
	if err != nil {
		return nil, fmt.Errorf("existing Mise config cannot be safely extended without rewriting it: %w", err)
	}
	for id, tool := range parsed {
		if !EqualMiseTool(tool, validated[id]) {
			return nil, fmt.Errorf("existing Mise config cannot be safely extended without rewriting it: tool %q would change", id)
		}
	}
	for id, tool := range additions {
		if !EqualMiseTool(tool, validated[id]) {
			return nil, fmt.Errorf("existing Mise config cannot be safely extended without rewriting it: tool %q was not added", id)
		}
	}
	return candidate, nil
}

// BuildMiseRemovalCandidate removes only the requested tool declarations
// from the existing TOML bytes. It fails closed unless both the supplied
// current declarations and the resulting declarations match a full TOML
// parse, so unsupported formatting can never turn into a broad rewrite.
func BuildMiseRemovalCandidate(existing []byte, current profile.MiseTools, removals []string) ([]byte, error) {
	parsed, err := ReadMiseToolsFromBytes(existing)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(parsed, nonNilMiseTools(current)) {
		return nil, errors.New("existing Mise config tools do not match the supplied current declarations")
	}
	remove := make(map[string]bool, len(removals))
	for _, id := range removals {
		if _, present := parsed[id]; !present {
			return nil, fmt.Errorf("mise tool %q is not present in the target config", id)
		}
		remove[id] = true
	}

	lines := strings.SplitAfter(string(existing), "\n")
	section := ""
	removeSection := false
	var candidate strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
			id, isTool := miseToolTableID(trimmed)
			removeSection = isTool && remove[id]
			if removeSection {
				continue
			}
		}
		if removeSection {
			continue
		}
		if section == "[tools]" {
			if id, ok := miseAssignmentID(trimmed); ok && remove[id] {
				continue
			}
		}
		candidate.WriteString(line)
	}
	result := []byte(candidate.String())
	want := make(profile.MiseTools, len(parsed)-len(remove))
	for id, tool := range parsed {
		if !remove[id] {
			want[id] = tool
		}
	}
	got, err := ReadMiseToolsFromBytes(result)
	if err != nil || !reflect.DeepEqual(got, want) {
		if err != nil {
			return nil, fmt.Errorf("existing Mise config cannot be safely reduced without rewriting it: %w", err)
		}
		return nil, errors.New("existing Mise config cannot be safely reduced without changing unrelated declarations")
	}
	return result, nil
}

func miseAssignmentID(line string) (string, bool) {
	if line == "" || strings.HasPrefix(line, "#") {
		return "", false
	}
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return "", false
	}
	id, err := parseTOMLKey(strings.TrimSpace(key))
	return id, err == nil
}

func miseToolTableID(header string) (string, bool) {
	if len(header) < 3 || header[0] != '[' || header[len(header)-1] != ']' {
		return "", false
	}
	inner := strings.TrimSpace(header[1 : len(header)-1])
	if !strings.HasPrefix(inner, "tools.") {
		return "", false
	}
	key := strings.TrimPrefix(inner, "tools.")
	if key == "" {
		return "", false
	}
	if key[0] == '\'' || key[0] == '"' {
		quote := key[0]
		for i := 1; i < len(key); i++ {
			if key[i] == quote && (quote == '\'' || key[i-1] != '\\') {
				id, err := parseTOMLKey(key[:i+1])
				return id, err == nil
			}
		}
		return "", false
	}
	if dot := strings.IndexByte(key, '.'); dot >= 0 {
		key = key[:dot]
	}
	id, err := parseTOMLKey(key)
	return id, err == nil
}

func parseTOMLKey(key string) (string, error) {
	if len(key) >= 2 && key[0] == '\'' && key[len(key)-1] == '\'' {
		return key[1 : len(key)-1], nil
	}
	if len(key) >= 2 && key[0] == '"' && key[len(key)-1] == '"' {
		return strconv.Unquote(key)
	}
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		return "", errors.New("invalid TOML key")
	}
	return key, nil
}

func encodeMiseAppendTool(id string, tool profile.MiseTool) ([]byte, error) {
	if _, err := NormalizeMiseTool(id, map[string]any(tool)); err != nil {
		return nil, err
	}
	b, err := EncodeMiseTools(profile.MiseTools{id: tool})
	if err != nil {
		return nil, err
	}
	// The profile encoder emits [tools] before its child table. Reopening that
	// table would be illegal when appending to a user config that already has it.
	b = bytes.TrimPrefix(b, []byte("[tools]\n"))
	if len(b) == 0 || !bytes.HasPrefix(b, []byte("[tools.")) {
		return nil, fmt.Errorf("unexpected Mise append encoding for tool %q", id)
	}
	return b, nil
}

func ValidateMiseMutationPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(abs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("mise global config is a symlink: %s", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("mise global config is not a regular file: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	root := filepath.VolumeName(abs) + string(os.PathSeparator)
	relative, err := filepath.Rel(root, abs)
	if err != nil {
		return err
	}
	parts := strings.Split(relative, string(os.PathSeparator))
	parent := root
	for _, part := range parts[:len(parts)-1] {
		parent = filepath.Join(parent, part)
		info, err := os.Lstat(parent)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("mise global config parent is a symlink: %s", parent)
			}
			if !info.IsDir() {
				return fmt.Errorf("mise global config parent is not a directory: %s", parent)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sortedMiseIDs(tools profile.MiseTools) []string {
	ids := make([]string, 0, len(tools))
	for id := range tools {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func nonNilMiseTools(tools profile.MiseTools) profile.MiseTools {
	if tools == nil {
		return profile.MiseTools{}
	}
	return tools
}

// mergeMise applies the Capture merge transition table to Mise tools,
// keeping the prior validated declaration for the "preserve" outcome
// (disabled + previously present, or disabled + previously desired-absent)
// rather than any live re-detected value, so restoring later reproduces the
// exact configuration that was actually desired.
func mergeMise(previous, current profile.MiseTools, prevAbsent map[string]bool, prevAbsentMise profile.MiseTools, enabled func(ref string) bool, absences []profile.PackageAbsence) (profile.MiseTools, []profile.PackageAbsence) {
	ids := map[string]bool{}
	for id := range previous {
		ids[id] = true
	}
	for id := range current {
		ids[id] = true
	}
	for ref := range prevAbsent {
		if kind, id, ok := splitRef(ref); ok && kind == "mise" {
			ids[id] = true
		}
	}
	result := profile.MiseTools{}
	for id := range ids {
		ref := "mise:" + id
		prevTool, wasPresent := previous[id]
		curTool, isPresent := current[id]
		isEnabled := enabled(ref)
		switch transition(wasPresent, prevAbsent[ref], isPresent, isEnabled) {
		case transitionPresent:
			if isEnabled && isPresent {
				result[id] = curTool
			} else {
				result[id] = prevTool
			}
		case transitionAbsent:
			mise := prevAbsentMise[id]
			if wasPresent {
				mise = prevTool
			}
			absences = append(absences, profile.PackageAbsence{Ref: ref, Mise: mise})
		}
	}
	return result, absences
}
