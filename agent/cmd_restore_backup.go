package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func cmdRestoreBackup(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("restore-backup", flag.ExitOnError)
	var (
		target       = fs.String("target", "", "Where to extract (default: a temp directory). Use --force to overwrite the live vault root.")
		force        = fs.Bool("force", false, "Allow extraction onto an existing non-empty target (required for live-vault restore)")
		fromRemote   = fs.String("from-remote", "", "Pull the latest blob from this git remote URL before restoring (overrides config)")
		listLocal    = fs.Bool("list", false, "List backups available in the configured local target dir and exit")
		latest       = fs.Bool("latest", false, "Restore the newest blob in the configured local target dir (instead of taking a path argument)")
		skipVerify   = fs.Bool("skip-verify", false, "Skip the post-decrypt manifest-hash verification (not recommended)")
	)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  jm restore-backup <path-to-blob> [--target DIR] [--force] [--skip-verify]")
		fmt.Fprintln(os.Stderr, "  jm restore-backup --latest [--target DIR] [--force]")
		fmt.Fprintln(os.Stderr, "  jm restore-backup --list")
		fmt.Fprintln(os.Stderr, "  jm restore-backup --from-remote URL [--target DIR] [--force]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	cfg := LoadConfig(vaultRoot)

	if *listLocal {
		listLocalBackups(cfg, vaultRoot)
		return
	}

	var blobPath string
	switch {
	case *fromRemote != "":
		path, err := pullLatestFromRemote(cfg, *fromRemote)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] pull from remote failed: %v\n", err)
			os.Exit(1)
		}
		blobPath = path
	case *latest:
		path, err := newestLocalBackup(cfg, vaultRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] %v\n", err)
			os.Exit(1)
		}
		blobPath = path
	default:
		if fs.NArg() < 1 {
			fs.Usage()
			os.Exit(2)
		}
		blobPath = fs.Arg(0)
	}

	dest := strings.TrimSpace(*target)
	if dest == "" {
		tmp, err := os.MkdirTemp("", "ljm-restore-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] create temp dir: %v\n", err)
			os.Exit(1)
		}
		dest = tmp
	} else {
		// If destination exists and is non-empty, require --force.
		if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 && !*force {
			fmt.Fprintf(os.Stderr, "[!] target %s is not empty — pass --force to overwrite\n", dest)
			os.Exit(1)
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "[!] create target: %v\n", err)
			os.Exit(1)
		}
	}

	identityPath, err := resolveBackupIdentityPath(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] %v\n", err)
		os.Exit(1)
	}

	extracted, err := decryptAndExtract(blobPath, identityPath, dest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] decrypt/extract: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[ok] decrypted %s\n", filepath.Base(blobPath))
	fmt.Printf("[ok] extracted %d files to %s\n", len(extracted), dest)

	if !*skipVerify {
		if err := verifyAgainstMeta(blobPath, dest, extracted); err != nil {
			fmt.Fprintf(os.Stderr, "[warn] verification: %v\n", err)
		} else {
			fmt.Println("[ok] manifest hash verified against .meta.json sidecar")
		}
	}

	// If the user pointed --target at an existing vault (CLAUDE.md present
	// pre-restore), warn that this overwrites live state.
	if *target != "" && filepath.Clean(*target) == filepath.Clean(vaultRoot) {
		fmt.Println()
		fmt.Println("[!] You restored ON TOP OF your live vault. The Memory/Buffer/Archive")
		fmt.Println("    state from the backup is now authoritative. Newer in-conversation")
		fmt.Println("    buffer entries from after the backup timestamp may have been lost.")
	}
}

func decryptAndExtract(blobPath, identityPath, dest string) ([]string, error) {
	in, err := os.Open(blobPath)
	if err != nil {
		return nil, fmt.Errorf("open blob: %w", err)
	}
	defer in.Close()

	r, err := DecryptFromAge(in, identityPath)
	if err != nil {
		return nil, fmt.Errorf("age decrypt: %w", err)
	}
	return UntarGzInto(r, dest)
}

// verifyAgainstMeta loads the .meta.json sidecar (alongside the blob) and
// recomputes the manifest hash over the extracted tree. Mismatch is a
// warning, not a hard error — a user might intentionally restore an old
// blob whose paths were excluded by a newer rules set.
func verifyAgainstMeta(blobPath, extractDir string, extracted []string) error {
	metaPath := strings.TrimSuffix(blobPath, ".age") + ".meta.json"
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("no meta sidecar at %s — skipping verification", filepath.Base(metaPath))
	}
	var meta BackupMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("parse meta: %w", err)
	}
	if meta.FileCount != len(extracted) {
		return fmt.Errorf("file count mismatch: meta=%d extracted=%d", meta.FileCount, len(extracted))
	}
	// Recompute the manifest hash over the extracted tree.
	sort.Strings(extracted)
	manifest := &BackupManifest{VaultRoot: extractDir, Paths: extracted}
	got, _, err := ManifestSHA256(manifest)
	if err != nil {
		return fmt.Errorf("hash extracted tree: %w", err)
	}
	if got != meta.ManifestSHA {
		return fmt.Errorf("manifest hash mismatch: want %s got %s",
			shortHash(meta.ManifestSHA), shortHash(got))
	}
	return nil
}

func listLocalBackups(cfg Config, vaultRoot string) {
	dir := resolveBackupLocalTargetDir(cfg, vaultRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] read %s: %v\n", dir, err)
		os.Exit(1)
	}
	type row struct {
		name string
		size int64
		mod  time.Time
	}
	var rows []row
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), "vault-") || !strings.HasSuffix(e.Name(), ".age") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		rows = append(rows, row{e.Name(), info.Size(), info.ModTime()})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].mod.After(rows[j].mod) })
	if len(rows) == 0 {
		fmt.Printf("(no backups found in %s)\n", dir)
		return
	}
	fmt.Printf("Backups in %s:\n", dir)
	for _, r := range rows {
		fmt.Printf("  %s  %s  %s\n",
			r.mod.Format("2006-01-02 15:04:05"),
			rightPad(humanBytes(r.size), 9),
			r.name)
	}
}

func newestLocalBackup(cfg Config, vaultRoot string) (string, error) {
	dir := resolveBackupLocalTargetDir(cfg, vaultRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	var newest os.FileInfo
	var newestName string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "vault-") || !strings.HasSuffix(e.Name(), ".age") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == nil || info.ModTime().After(newest.ModTime()) {
			newest = info
			newestName = e.Name()
		}
	}
	if newestName == "" {
		return "", fmt.Errorf("no backups found in %s", dir)
	}
	return filepath.Join(dir, newestName), nil
}

// pullLatestFromRemote clones (or pulls) the configured remote into a
// temporary directory, picks the newest vault-*.age, copies it locally,
// and returns the path to that local copy. The caller decrypts it.
func pullLatestFromRemote(cfg Config, remoteURL string) (string, error) {
	tmp, err := os.MkdirTemp("", "ljm-remote-*")
	if err != nil {
		return "", err
	}
	out, err := exec.Command("git", "clone", "--depth", "1", remoteURL, tmp).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git clone: %v: %s", err, strings.TrimSpace(string(out)))
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", err
	}
	var newest os.FileInfo
	var newestName string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "vault-") || !strings.HasSuffix(e.Name(), ".age") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == nil || info.Name() > newestName {
			// Filenames are timestamp-prefixed and sortable lexically.
			newest = info
			newestName = e.Name()
		}
	}
	if newestName == "" {
		return "", fmt.Errorf("no vault-*.age files found at remote %s", remoteURL)
	}
	return filepath.Join(tmp, newestName), nil
}

func rightPad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
