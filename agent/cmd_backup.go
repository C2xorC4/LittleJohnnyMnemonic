package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
)

const backupVersion = 1

// BackupMeta is the cleartext sidecar written next to every encrypted blob.
// It records ONLY operational metadata — no filenames, no content fragments
// — so that a restore can sanity-check the encrypted payload without
// leaking anything useful to a remote that can read the meta file.
type BackupMeta struct {
	Version       int       `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	ToolVersion   string    `json:"tool_version"`
	FileCount     int       `json:"file_count"`
	UncompressedB int64     `json:"uncompressed_bytes"`
	ManifestSHA  string    `json:"manifest_sha256"`
	BlobName     string    `json:"blob_name"`
}

// BackupResult describes what a backup run produced.
type BackupResult struct {
	BlobPath  string
	MetaPath  string
	Pushed    bool
	PushError string // empty on success or when push was skipped
	Meta      BackupMeta
}

func cmdBackup(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	var (
		localOverride  = fs.String("local", "", "Override local target directory (overrides config and env)")
		remoteOverride = fs.String("remote", "", "Override git remote URL (overrides config)")
		noPush         = fs.Bool("no-push", false, "Write local copy only — skip git push even if remote is configured")
		dryRun         = fs.Bool("dry-run", false, "Build manifest and report what would be backed up; don't write or encrypt")
		initKey        = fs.Bool("init-key", false, "Generate a new age keypair, write the private key to the configured path, and print the public recipient (for Config.md)")
		showRecipient  = fs.Bool("show-recipient", false, "Print the public recipient derived from the configured age identity file")
	)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: jm backup [--init-key | --show-recipient | --dry-run] [--local PATH] [--remote URL] [--no-push]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	cfg := LoadConfig(vaultRoot)

	if *initKey {
		runInitKey(cfg)
		return
	}
	if *showRecipient {
		runShowRecipient(cfg)
		return
	}

	opts := BackupOpts{
		LocalOverride:  *localOverride,
		RemoteOverride: *remoteOverride,
		NoPush:         *noPush,
		DryRun:         *dryRun,
	}
	res, err := RunBackup(cfg, vaultRoot, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] backup failed: %v\n", err)
		os.Exit(1)
	}
	if *dryRun {
		fmt.Printf("[dry-run] would back up %d files (%s) to %s\n",
			res.Meta.FileCount, humanBytes(res.Meta.UncompressedB), res.BlobPath)
		return
	}
	fmt.Printf("[ok] wrote %s (manifest sha256: %s, %d files, %s uncompressed)\n",
		res.BlobPath, shortHash(res.Meta.ManifestSHA), res.Meta.FileCount,
		humanBytes(res.Meta.UncompressedB))
	if res.Pushed {
		fmt.Printf("[ok] pushed to git remote\n")
	} else if res.PushError != "" {
		fmt.Fprintf(os.Stderr, "[warn] git push skipped or failed: %s\n", res.PushError)
		fmt.Fprintln(os.Stderr, "      local copy is intact — backup is durable on this machine.")
	}
}

// BackupOpts mirrors the flags but is reusable for in-process invocation
// (Phase 2 will call RunBackup from the consolidation and stop hooks).
type BackupOpts struct {
	LocalOverride  string
	RemoteOverride string
	NoPush         bool
	DryRun         bool
}

// RunBackup is the in-process entry point. Returns the result on success.
// On failure, returns the partial result if the local-dir copy made it.
func RunBackup(cfg Config, vaultRoot string, opts BackupOpts) (BackupResult, error) {
	var res BackupResult

	if cfg.BackupAgeRecipient == "" && !opts.DryRun {
		return res, fmt.Errorf("no backup_age_recipient configured — run `jm backup --init-key` first")
	}

	manifest, err := BuildManifest(vaultRoot, nil)
	if err != nil {
		return res, fmt.Errorf("build manifest: %w", err)
	}
	manifestSHA, totalSize, err := ManifestSHA256(manifest)
	if err != nil {
		return res, fmt.Errorf("hash manifest: %w", err)
	}
	res.Meta = BackupMeta{
		Version:       backupVersion,
		CreatedAt:     time.Now().UTC(),
		ToolVersion:   fmt.Sprintf("jm/%s", runtime.Version()),
		FileCount:     len(manifest.Paths),
		UncompressedB: totalSize,
		ManifestSHA:   manifestSHA,
	}

	if opts.DryRun {
		// Resolve target for reporting only.
		target := opts.LocalOverride
		if target == "" {
			target = resolveBackupLocalTargetDir(cfg, vaultRoot)
		}
		res.BlobPath = filepath.Join(target, blobName(res.Meta.CreatedAt, manifestSHA))
		return res, nil
	}

	target := opts.LocalOverride
	if target == "" {
		target = resolveBackupLocalTargetDir(cfg, vaultRoot)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return res, fmt.Errorf("create target dir: %w", err)
	}

	blob := blobName(res.Meta.CreatedAt, manifestSHA)
	res.Meta.BlobName = blob
	blobPath := filepath.Join(target, blob)
	metaPath := filepath.Join(target, strings.TrimSuffix(blob, ".age")+".meta.json")

	if err := writeEncryptedBlob(blobPath, cfg.BackupAgeRecipient, manifest); err != nil {
		return res, fmt.Errorf("encrypt+write blob: %w", err)
	}
	if err := writeMeta(metaPath, res.Meta); err != nil {
		return res, fmt.Errorf("write meta: %w", err)
	}
	res.BlobPath = blobPath
	res.MetaPath = metaPath

	applyLocalRetention(target, cfg.BackupRetentionKeepLast)

	remoteURL := opts.RemoteOverride
	if remoteURL == "" {
		remoteURL = cfg.BackupRemoteURL
	}
	if !opts.NoPush && cfg.BackupPushOnBackup && remoteURL != "" {
		err := pushBlobToRemote(cfg, remoteURL, blobPath, metaPath)
		if err != nil {
			res.PushError = err.Error()
		} else {
			res.Pushed = true
		}
	}

	recordLastBackup(vaultRoot, res.Meta.CreatedAt)
	return res, nil
}

func writeEncryptedBlob(path string, recipientStr string, m *BackupManifest) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	enc, err := EncryptToAge(out, recipientStr)
	if err != nil {
		return err
	}
	if err := TarGzManifest(m, enc); err != nil {
		_ = enc.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("close age writer: %w", err)
	}
	return nil
}

func writeMeta(path string, m BackupMeta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func blobName(t time.Time, manifestSHA string) string {
	return fmt.Sprintf("vault-%s-%s.age",
		t.Format("20060102T150405Z"),
		manifestSHA[:12])
}

// applyLocalRetention deletes all but the N newest vault-*.age (and matching
// .meta.json) pairs in dir. Errors are non-fatal — retention is best-effort.
func applyLocalRetention(dir string, keep int) {
	if keep <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type pair struct {
		name string
		mod  time.Time
	}
	var blobs []pair
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
		blobs = append(blobs, pair{e.Name(), info.ModTime()})
	}
	if len(blobs) <= keep {
		return
	}
	sort.Slice(blobs, func(i, j int) bool { return blobs[i].mod.After(blobs[j].mod) })
	for _, p := range blobs[keep:] {
		_ = os.Remove(filepath.Join(dir, p.name))
		meta := strings.TrimSuffix(p.name, ".age") + ".meta.json"
		_ = os.Remove(filepath.Join(dir, meta))
	}
}

// pushBlobToRemote shells out to git in a working clone of the configured
// remote. Workflow is **always pull-then-push, never auto-merge**:
//
//   - if the local clone has commits, run `git pull --ff-only` first.
//     A non-fast-forward result is a hard failure — the user must
//     resolve the divergence by hand before the next backup will push
//     (this is intentional; encrypted blobs can't be auto-merged and
//     conflicting memory states deserve deliberate review).
//   - on first push to an empty remote (unborn HEAD locally), skip the
//     pull entirely — there's nothing to pull yet.
//   - the push uses `-u origin HEAD` so the first run sets upstream
//     and subsequent runs fast-forward.
//
// The clone is created on first use at cfg.BackupRemoteClonePath if it
// doesn't already exist. Auth comes from the user's git config
// (credential helpers, SSH agent, etc.) — no creds handled here.
func pushBlobToRemote(cfg Config, remoteURL, blobPath, metaPath string) error {
	clonePath := strings.TrimSpace(cfg.BackupRemoteClonePath)
	if clonePath == "" {
		clonePath = filepath.Join(filepath.Dir(blobPath), ".remote-clone")
	}

	if _, err := os.Stat(filepath.Join(clonePath, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(clonePath), 0o755); err != nil {
			return fmt.Errorf("make parent for clone: %w", err)
		}
		out, err := exec.Command("git", "clone", remoteURL, clonePath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("git clone: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}

	// Pull only if we already have local commits. An unborn HEAD means
	// this is the first push to an empty remote and there's nothing to
	// pull. `git rev-parse --verify HEAD` returns non-zero on unborn.
	if _, err := runGitIn(clonePath, "rev-parse", "--verify", "HEAD"); err == nil {
		if out, err := runGitIn(clonePath, "pull", "--ff-only"); err != nil {
			return fmt.Errorf("git pull --ff-only failed — remote has changes that don't fast-forward. "+
				"Resolve the divergence in %s manually (merge or rebase) before the next backup. "+
				"Local copy of this backup is intact at %s. (%v: %s)",
				clonePath, blobPath, err, strings.TrimSpace(out))
		}
	}

	dstBlob := filepath.Join(clonePath, filepath.Base(blobPath))
	dstMeta := filepath.Join(clonePath, filepath.Base(metaPath))
	if err := copyFile(blobPath, dstBlob); err != nil {
		return fmt.Errorf("copy blob into clone: %w", err)
	}
	if err := copyFile(metaPath, dstMeta); err != nil {
		return fmt.Errorf("copy meta into clone: %w", err)
	}

	if out, err := runGitIn(clonePath, "add", filepath.Base(blobPath), filepath.Base(metaPath)); err != nil {
		return fmt.Errorf("git add: %v: %s", err, strings.TrimSpace(out))
	}
	commitMsg := fmt.Sprintf("backup %s", strings.TrimSuffix(filepath.Base(blobPath), ".age"))
	if out, err := runGitIn(clonePath, "commit", "-m", commitMsg); err != nil {
		// "nothing to commit" can happen if the same blob (same manifest hash)
		// was already added in a prior run. Treat as success, not failure.
		if strings.Contains(out, "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit: %v: %s", err, strings.TrimSpace(out))
	}
	// `-u origin HEAD` works for both the first push (sets upstream tracking)
	// and subsequent pushes (fast-forward).
	if out, err := runGitIn(clonePath, "push", "-u", "origin", "HEAD"); err != nil {
		return fmt.Errorf("git push: %v: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func runGitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// recordLastBackup writes Metrics/last_backup.json with the most recent
// successful backup timestamp. Used by the cooldown gate in Phase 2 hooks.
func recordLastBackup(vaultRoot string, t time.Time) {
	path := filepath.Join(vaultRoot, "Metrics", "last_backup.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	payload := map[string]any{"last_backup_utc": t.UTC().Format(time.RFC3339)}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// readLastBackup returns the timestamp of the most recent successful backup,
// or the zero time if no record exists yet (forces the first auto-backup
// through immediately).
func readLastBackup(vaultRoot string) time.Time {
	path := filepath.Join(vaultRoot, "Metrics", "last_backup.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	var payload struct {
		LastBackupUTC string `json:"last_backup_utc"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, payload.LastBackupUTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

// MaybeRunBackup is the cooldown-gated, fail-soft entry point used by the
// post-consolidation and stop hooks. Returns:
//   - skipped=true when cfg.BackupEnabled is false, the recipient isn't
//     configured, or the cooldown window hasn't elapsed (no error logged
//     in those cases — they're normal operation)
//   - skipped=false on attempt; err non-nil only on actual failure to
//     produce the local copy. Push failures are absorbed into res and
//     never surface as errors here, so a bad network never blocks
//     consolidation.
//
// The "trigger" string is included in stderr-logged warnings so multi-hook
// invocations can be told apart at a glance ("consolidate" vs "stop").
func MaybeRunBackup(cfg Config, vaultRoot, trigger string) (skipped bool, err error) {
	if !cfg.BackupEnabled {
		return true, nil
	}
	if strings.TrimSpace(cfg.BackupAgeRecipient) == "" {
		return true, nil
	}
	cooldown := time.Duration(cfg.BackupCooldownMinutes) * time.Minute
	if cooldown > 0 {
		last := readLastBackup(vaultRoot)
		if !last.IsZero() && time.Since(last) < cooldown {
			return true, nil
		}
	}

	res, err := RunBackup(cfg, vaultRoot, BackupOpts{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[backup:%s] failed: %v\n", trigger, err)
		return false, err
	}
	fmt.Fprintf(os.Stderr, "[backup:%s] wrote %s (%d files, %s)\n",
		trigger, filepath.Base(res.BlobPath), res.Meta.FileCount,
		humanBytes(res.Meta.UncompressedB))
	if res.PushError != "" {
		fmt.Fprintf(os.Stderr, "[backup:%s] push warning: %s\n", trigger, res.PushError)
	}
	return false, nil
}

// runInitKey generates a fresh X25519 keypair, writes the private key to
// the configured identity path (creating the directory), and prints the
// public recipient so the user can paste it into Config.md.
func runInitKey(cfg Config) {
	identityPath, err := resolveBackupIdentityPath(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] resolve identity path: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stat(identityPath); err == nil {
		fmt.Fprintf(os.Stderr, "[!] identity file already exists at %s — refusing to overwrite\n", identityPath)
		fmt.Fprintln(os.Stderr, "    Move/rename the existing key if you really want to rotate.")
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(identityPath), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "[!] create identity dir: %v\n", err)
		os.Exit(1)
	}

	id, err := age.GenerateX25519Identity()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] generate keypair: %v\n", err)
		os.Exit(1)
	}
	body := fmt.Sprintf("# created: %s\n# public key: %s\n%s\n",
		time.Now().UTC().Format(time.RFC3339),
		id.Recipient().String(),
		id.String())
	if err := os.WriteFile(identityPath, []byte(body), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "[!] write identity file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[ok] wrote private key to %s (mode 0600)\n", identityPath)
	fmt.Println()
	fmt.Println("Public recipient (paste into System/Config.md as backup_age_recipient):")
	fmt.Printf("  %s\n", id.Recipient().String())
	fmt.Println()
	fmt.Println("ESCROW THIS KEYFILE.")
	fmt.Println("Without it, every encrypted backup is unrecoverable. Suggested escrow:")
	fmt.Println("  - copy the file to a hardware device (USB, Yubikey-attached storage)")
	fmt.Println("  - paste the contents into your password manager")
	fmt.Println("  - print and store offline")
}

func runShowRecipient(cfg Config) {
	identityPath, err := resolveBackupIdentityPath(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] resolve identity path: %v\n", err)
		os.Exit(1)
	}
	ids, err := loadAgeIdentities(identityPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] load identity: %v\n", err)
		os.Exit(1)
	}
	for _, raw := range ids {
		id, ok := raw.(*age.X25519Identity)
		if !ok {
			continue
		}
		fmt.Println(id.Recipient().String())
	}
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
