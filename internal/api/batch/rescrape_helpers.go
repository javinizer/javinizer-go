package batch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	fsCaseCacheMu sync.RWMutex
	fsCaseCache   = make(map[string]bool)
)

func isCaseInsensitiveFS(path string) bool {
	testFile1 := filepath.Join(path, ".javinizer_case_test_1")
	testFile2 := filepath.Join(path, ".JAVINIZER_CASE_TEST_1")

	defer func() { _ = os.Remove(testFile1) }()
	defer func() { _ = os.Remove(testFile2) }()

	if err := os.WriteFile(testFile1, []byte("test"), 0644); err != nil {
		return false
	}

	if err := os.WriteFile(testFile2, []byte("test2"), 0644); err != nil {
		return false
	}

	content, err := os.ReadFile(testFile1)
	if err != nil {
		return false
	}

	return string(content) == "test2"
}

func isCaseInsensitiveFSCached(path string) bool {
	fsCaseCacheMu.RLock()
	if result, ok := fsCaseCache[path]; ok {
		fsCaseCacheMu.RUnlock()
		return result
	}
	fsCaseCacheMu.RUnlock()

	result := isCaseInsensitiveFS(path)

	fsCaseCacheMu.Lock()
	fsCaseCache[path] = result
	fsCaseCacheMu.Unlock()

	return result
}

func suffixOrder(s string) int {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return 100
	}
	if len(s) == 1 && s[0] >= 'A' && s[0] <= 'Z' {
		return int(s[0] - 'A')
	}
	if strings.HasPrefix(s, "pt") {
		if n, err := strconv.Atoi(s[2:]); err == nil {
			return 10 + n
		}
	}
	if n, err := strconv.Atoi(s); err == nil {
		return 10 + n
	}
	return 50
}
