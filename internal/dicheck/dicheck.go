package dicheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FindRuntimeDIUsage scans the directory rooted at 'root' and returns a
// list of files that use runtime container patterns (Resolve, MustResolve, Bind).
// Files under any 'allowedDirs' (relative to root) are ignored.
func FindRuntimeDIUsage(root string, allowedDirs []string) ([]string, error) {
	var matches []string

	// Normalize allowed dirs
	normAllowed := make([]string, 0, len(allowedDirs))
	for _, p := range allowedDirs {
		p = strings.TrimPrefix(p, "/")
		p = filepath.Clean(p)
		normAllowed = append(normAllowed, p)
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil {
				relSlash := filepath.ToSlash(rel)
				for _, ad := range normAllowed {
					adSlash := filepath.ToSlash(ad)
					if relSlash == adSlash || strings.HasPrefix(relSlash, adSlash+"/") {
						// allowed directory - skip its contents
						return filepath.SkipDir
					}
				}
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// ignore files in testdata
		if strings.Contains(path, string(os.PathSeparator)+"testdata"+string(os.PathSeparator)) {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := string(b)
		// Patterns indicative of runtime DI usage
		if strings.Contains(s, "Resolve[") || strings.Contains(s, "MustResolve[") || strings.Contains(s, "Resolve(") || strings.Contains(s, "MustResolve(") || strings.Contains(s, "Bind(") {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}
