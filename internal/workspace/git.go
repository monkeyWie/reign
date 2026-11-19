package workspace

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/monkeyWie/reignhub/internal/provider"
)

// Metadata reads are bounded and cached only for the current request. Never run
// Git or inspect a controller's filesystem to describe a remote checkout.
func readGitFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("invalid Git metadata file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("Git metadata too large")
	}
	return raw, err
}

func (r *Resolver) gitInfo(checkout checkout) *provider.GitInfo {
	if checkout.headDir == "" {
		return nil
	}
	if info, ok := r.heads[checkout.headDir]; ok {
		return &info
	}
	info := provider.GitInfo{}
	r.repositoryKey(checkout.gitDir)
	info.Repository = r.origins[checkout.gitDir]
	if raw, err := readGitFile(filepath.Join(checkout.headDir, "HEAD"), 4096); err == nil {
		head := strings.TrimSpace(string(raw))
		if branch, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
			if branch != "" && !strings.ContainsFunc(branch, unicode.IsSpace) && !strings.ContainsFunc(branch, unicode.IsControl) {
				info.Branch = branch
			}
		} else if len(head) == 40 || len(head) == 64 {
			if _, err := hex.DecodeString(head); err == nil {
				info.Commit = head
			}
		}
	}
	if r.heads == nil {
		r.heads = map[string]provider.GitInfo{}
	}
	r.heads[checkout.headDir] = info
	return &info
}
