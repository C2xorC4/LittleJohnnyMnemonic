package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"filippo.io/age"
)

// BackupManifest is the ordered list of files (relative to vault root, forward
// slashes) to include in a backup. It is the input to TarGzManifest and is
// what the manifest-hash is computed over.
type BackupManifest struct {
	VaultRoot string
	Paths     []string // sorted, forward-slash-relative
}

// defaultExclusions are the path prefixes/globs that are never included in a
// backup. Most are rebuildable artifacts (jm.exe), UI state (.obsidian/
// workspace.json), or transient operational logs. The .git directory is
// always excluded — backups are content snapshots, not git mirrors.
//
// Each entry is matched against the forward-slash relative path of every
// file under the vault root. Match modes:
//   - exact match (full relative path)
//   - prefix match (entry ends with "/")
//   - basename match (entry has no slash) — matches if the file's base name equals the entry
func defaultExclusions() []string {
	return []string{
		"agent/jm.exe",
		"agent/jm",
		"agent/jm.test",
		"agent/jm.test.exe",
		"agent/coverage.out",
		".obsidian/workspace.json",
		".obsidian/workspace-mobile.json",
		".obsidian/graph.json",
		"Metrics/rule_firings.jsonl",
		".git/",
		"Backup/",                 // never recurse into a vault-local backup tree
		"backups/",                // alternate naming
		".scratch-backup-test/",   // test-suite scratch
		".cache/",
		".idea/",
		".vscode/",
		"node_modules/",           // never expected, but cheap insurance
		".DS_Store",
		"Thumbs.db",
		// extension-style filters handled in pathExcluded
	}
}

// excludedExtensions are file suffixes that never get backed up (editor swap
// files, OS junk). Matched case-insensitively against the file's lower-case
// base name.
func excludedExtensions() []string {
	return []string{".tmp", ".swp", ".swo", "~"}
}

// pathExcluded reports whether a forward-slash relative path matches any of
// the default exclusions or the user-supplied extras. Comparison is
// case-sensitive on POSIX semantics; Windows users who care about
// case-folding should add explicit entries.
func pathExcluded(rel string, extras []string) bool {
	rel = filepath.ToSlash(rel)
	all := append(defaultExclusions(), extras...)
	for _, ex := range all {
		if ex == "" {
			continue
		}
		ex = filepath.ToSlash(ex)
		// directory prefix
		if strings.HasSuffix(ex, "/") {
			if strings.HasPrefix(rel, ex) {
				return true
			}
			continue
		}
		// exact path
		if ex == rel {
			return true
		}
		// basename match (no slash in the rule)
		if !strings.Contains(ex, "/") {
			if filepath.Base(rel) == ex {
				return true
			}
		}
	}
	base := strings.ToLower(filepath.Base(rel))
	for _, ext := range excludedExtensions() {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	return false
}

// BuildManifest walks the vault root and returns every regular file's
// forward-slash relative path that survives the exclusion filter. The output
// is sorted so manifest hashing is stable across runs.
func BuildManifest(vaultRoot string, extraExclusions []string) (*BackupManifest, error) {
	var paths []string
	err := filepath.WalkDir(vaultRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(vaultRoot, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			// Allow exclusion of an entire directory subtree.
			if pathExcluded(relSlash+"/", extraExclusions) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if pathExcluded(relSlash, extraExclusions) {
			return nil
		}
		paths = append(paths, relSlash)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk vault: %w", err)
	}
	sort.Strings(paths)
	return &BackupManifest{VaultRoot: vaultRoot, Paths: paths}, nil
}

// ManifestSHA256 produces a stable hash over the manifest's
// (path, size, sha256-of-content) triples. Content is hashed once per file;
// for a 1-2 MB vault this is microseconds. The hash is what gets recorded
// in the cleartext .meta.json so a restore can verify integrity post-decrypt.
func ManifestSHA256(m *BackupManifest) (string, int64, error) {
	h := sha256.New()
	var total int64
	for _, rel := range m.Paths {
		full := filepath.Join(m.VaultRoot, filepath.FromSlash(rel))
		info, err := os.Stat(full)
		if err != nil {
			return "", 0, fmt.Errorf("stat %s: %w", rel, err)
		}
		fileHash, err := sha256File(full)
		if err != nil {
			return "", 0, err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%s\n", rel, info.Size(), fileHash)
		total += info.Size()
	}
	return hex.EncodeToString(h.Sum(nil)), total, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// TarGzManifest writes a tar.gz stream of the manifest's files to w. Inside
// the archive, paths are forward-slash and relative to the vault root.
// Symlinks, devices, and pipes are skipped — only regular files survive.
func TarGzManifest(m *BackupManifest, w io.Writer) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, rel := range m.Paths {
		full := filepath.Join(m.VaultRoot, filepath.FromSlash(rel))
		info, err := os.Stat(full)
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("tar header %s: %w", rel, err)
		}
		hdr.Name = rel // forward-slash relative
		// Drop OS-specific UID/GID/UName/GName so the archive is portable.
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = ""
		hdr.Gname = ""
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("tar write header %s: %w", rel, err)
		}
		f, err := os.Open(full)
		if err != nil {
			return fmt.Errorf("open %s: %w", rel, err)
		}
		// Copy EXACTLY hdr.Size bytes. The size was fixed from the earlier
		// os.Stat, but volatile files (e.g. Metrics/retrieval_sessions.jsonl,
		// live-appended by every retrieval/hook) can grow between stat and copy.
		// A plain io.Copy would stream the grown tail and trip archive/tar's
		// "write too long". If the file shrank instead (rotation/truncation),
		// pad the entry so its body still matches the declared header size.
		if err := writeTarEntryBounded(tw, f, hdr.Size); err != nil {
			f.Close()
			return fmt.Errorf("tar copy %s: %w", rel, err)
		}
		f.Close()
	}
	return nil
}

// writeTarEntryBounded writes exactly size bytes from r into tw: it never writes
// more than size (so a concurrently-growing file can't overrun the tar header),
// and pads with zeros if r yields fewer than size bytes (so a shrunk file can't
// leave the entry short of its declared size).
func writeTarEntryBounded(tw io.Writer, r io.Reader, size int64) error {
	n, err := io.CopyN(tw, r, size)
	if err != nil && err != io.EOF {
		return err
	}
	if n < size {
		if _, err := io.CopyN(tw, zeroReader{}, size-n); err != nil {
			return err
		}
	}
	return nil
}

// zeroReader is an unbounded source of zero bytes, used to pad a tar entry when
// its backing file shrinks between stat and copy.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// UntarGzInto reads a tar.gz stream from r and extracts every regular-file
// entry under target. Path traversal (..) and absolute paths are rejected.
// Existing files are overwritten. Returns the list of extracted relative
// paths in the order they appeared in the archive.
func UntarGzInto(r io.Reader, target string) ([]string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var extracted []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		clean := filepath.ToSlash(hdr.Name)
		if !isSafeTarPath(clean) {
			return nil, fmt.Errorf("unsafe tar path: %q", hdr.Name)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA { //nolint:staticcheck
			continue
		}
		dest := filepath.Join(target, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", dest, err)
		}
		f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", dest, err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return nil, fmt.Errorf("write %s: %w", dest, err)
		}
		f.Close()
		extracted = append(extracted, clean)
	}
	return extracted, nil
}

// isSafeTarPath reports whether a tar entry name is safe to extract: non-empty,
// relative, and with no ".." path SEGMENT. A literal filename that merely
// CONTAINS ".." (e.g. "agent/..jm.exe") is safe — only a ".." directory
// component is a traversal risk. The previous strings.Contains(name, "..")
// check false-rejected such names and made every backup containing the stray
// "agent/..jm.exe" artifact (May 22 – Jun 11) un-restorable.
func isSafeTarPath(name string) bool {
	name = filepath.ToSlash(name)
	if name == "" || strings.HasPrefix(name, "/") {
		return false
	}
	if filepath.IsAbs(filepath.FromSlash(name)) { // e.g. C:\... on Windows
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// EncryptToAge wraps w in an age envelope using the provided X25519 recipient
// (parsed from an "age1..." string). Caller must close the returned writer
// to flush the envelope before closing the underlying writer.
func EncryptToAge(w io.Writer, recipientStr string) (io.WriteCloser, error) {
	recipient, err := age.ParseX25519Recipient(recipientStr)
	if err != nil {
		return nil, fmt.Errorf("parse age recipient: %w", err)
	}
	return age.Encrypt(w, recipient)
}

// DecryptFromAge unwraps r using the X25519 identity in identityPath
// (a file containing an "AGE-SECRET-KEY-1..." line, possibly with comments).
func DecryptFromAge(r io.Reader, identityPath string) (io.Reader, error) {
	identities, err := loadAgeIdentities(identityPath)
	if err != nil {
		return nil, err
	}
	return age.Decrypt(r, identities...)
}

// loadAgeIdentities parses an age keyfile. Format follows age-keygen:
// comment lines starting with '#' are ignored; the first AGE-SECRET-KEY line
// is used. Multiple identities can be present (they are all returned).
func loadAgeIdentities(path string) ([]age.Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read age identity file: %w", err)
	}
	var ids []age.Identity
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, err := age.ParseX25519Identity(line)
		if err != nil {
			return nil, fmt.Errorf("parse age identity: %w", err)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no AGE-SECRET-KEY found in %s", path)
	}
	return ids, nil
}

// resolveBackupIdentityPath returns the configured private key path with
// home-directory expansion. Defaults to %USERPROFILE%/.config/ljm/age.key
// (Windows) or $HOME/.config/ljm/age.key (Unix) when unset.
func resolveBackupIdentityPath(cfg Config) (string, error) {
	p := strings.TrimSpace(cfg.BackupAgeIdentityPath)
	if p == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home dir: %w", err)
		}
		return filepath.Join(home, ".config", "ljm", "age.key"), nil
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home dir: %w", err)
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// resolveBackupLocalTargetDir returns the configured local target dir with
// fallbacks. Order: env (JM_BACKUP_LOCAL_TARGET_DIR) > Config.md value >
// vault-sibling default ("<vaultRoot>/../LittleJohnnyMnemonic-Backup").
func resolveBackupLocalTargetDir(cfg Config, vaultRoot string) string {
	if v := strings.TrimSpace(os.Getenv("JM_BACKUP_LOCAL_TARGET_DIR")); v != "" {
		return v
	}
	if v := strings.TrimSpace(cfg.BackupLocalTargetDir); v != "" {
		return v
	}
	parent := filepath.Dir(vaultRoot)
	return filepath.Join(parent, "LittleJohnnyMnemonic-Backup")
}
