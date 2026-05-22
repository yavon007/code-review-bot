package worker

import (
	"path/filepath"
	"strings"

	"code-review-bot/internal/gitea"
)

func filterChangedFiles(files []gitea.ChangedFile, excludePatterns []string) []gitea.ChangedFile {
	if len(excludePatterns) == 0 {
		return files
	}
	result := make([]gitea.ChangedFile, 0, len(files))
	for _, file := range files {
		if !isExcludedPath(file.Filename, excludePatterns) {
			result = append(result, file)
		}
	}
	return result
}

func filterUnifiedDiff(diff string, excludePatterns []string) string {
	if len(excludePatterns) == 0 || diff == "" {
		return diff
	}

	lines := strings.Split(diff, "\n")
	var builder strings.Builder
	includeFile := true
	inFile := false

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			path := diffPath(line)
			includeFile = path == "" || !isExcludedPath(path, excludePatterns)
			inFile = true
		}
		if !inFile || includeFile {
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
	}
	return strings.TrimSuffix(builder.String(), "\n")
}

func diffPath(line string) string {
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return ""
	}
	path := strings.TrimPrefix(parts[3], "b/")
	if path == "/dev/null" {
		path = strings.TrimPrefix(parts[2], "a/")
	}
	return path
}

func isExcludedPath(path string, patterns []string) bool {
	path = filepath.ToSlash(strings.TrimPrefix(path, "./"))
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "**")
			if strings.HasPrefix(path, prefix) {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(pattern, path); ok {
			return true
		}
		if !strings.Contains(pattern, "/") {
			if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
				return true
			}
		}
	}
	return false
}
