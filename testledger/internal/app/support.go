// Small shared helpers: identifiers, artifact hashing, subprocess
// environment, and pagination.
package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

func newID(prefix string) string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UTC().UnixMilli(), hex.EncodeToString(bytes))
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), count, err
}

func environment(allowlist []string, additions map[string]string) []string {
	allowed := make(map[string]bool, len(allowlist))
	for _, key := range allowlist {
		allowed[key] = true
	}
	values := map[string]string{}
	for _, pair := range os.Environ() {
		key, value, ok := strings.Cut(pair, "=")
		if ok && allowed[key] {
			values[key] = value
		}
	}
	for key, value := range additions {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func truncate(value string, limit int64) string {
	if limit <= 0 || int64(len(value)) <= limit {
		return value
	}
	return value[:limit] + "\n<testledger: output truncated>"
}

func writeJSON(path string, value any) error {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(contents, '\n'), 0o600)
}

// coverable reports whether a symbol has no executable body -- an overload
// stub, or a body that is only a docstring. No run can ever produce coverage
// for one, so reporting it as a gap would create a backlog entry nobody can
// ever clear.

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }

func normalizePage(limit, cursor int) (int, int) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if cursor < 0 {
		cursor = 0
	}
	return limit, cursor
}

// unsupportedLanguageError names a configured language with no adapter.
func unsupportedLanguageError(name string) error {
	return fmt.Errorf("unsupported language adapter %q", name)
}
