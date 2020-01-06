package fswatcher

import (
	"os"

	"github.com/sequix/grok_exporter/tailer/glob"
)

// Gets the directory paths from the glob expressions,
// and makes sure these directories exist.
func expandGlobs(globs []glob.Glob) (map[string]struct{}, Error) {
	result := make(map[string]struct{})
	for _, g := range globs {
		if _, existing := result[g.Dir()]; existing {
			continue
		}
		dirInfo, err := os.Stat(g.Dir())
		if err != nil {
			if os.IsNotExist(err) {
				return nil, NewErrorf(DirectoryNotFound, nil, "%q: no such directory", g.Dir())
			}
			return nil, NewErrorf(NotSpecified, err, "%q: stat() failed", g.Dir())
		}
		if !dirInfo.IsDir() {
			return nil, NewErrorf(NotSpecified, nil, "%q is not a directory", g.Dir())
		}
		result[g.Dir()] = struct{}{}
	}
	return result, nil
}
