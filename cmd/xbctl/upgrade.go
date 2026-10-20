package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// releaseRepo is the GitHub repository whose releases `xbctl upgrade` installs.
// Override at build time with -ldflags "-X main.releaseRepo=owner/name".
var releaseRepo = "xiaofujie369/xbord-node-v3"

const (
	checksumAsset     = "SHA256SUMS"
	maxDownloadBytes  = 256 << 20
	maxChecksumBytes  = 64 << 10
	upgradeLockPath   = "/run/xbctl-upgrade.lock"
	healthWaitTimeout = 15 * time.Second
)

var (
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type upgradeOptions struct {
	version   string
	repo      string
	binarySum string
	cliSum    string
}

func parseUpgradeArgs(args []string) (upgradeOptions, error) {
	opts := upgradeOptions{version: "latest", repo: releaseRepo}
	for i := 0; i < len(args); i++ {
		var target *string
		switch args[i] {
		case "--version":
			target = &opts.version
		case "--repo":
			target = &opts.repo
		case "--sha256":
			target = &opts.binarySum
		case "--xbctl-sha256":
			target = &opts.cliSum
		default:
			return opts, fmt.Errorf("unknown upgrade argument %q", args[i])
		}
		if i+1 >= len(args) {
			return opts, fmt.Errorf("%s requires a value", args[i])
		}
		*target = args[i+1]
		i++
	}
	if opts.version != "latest" && !versionPattern.MatchString(opts.version) {
		return opts, fmt.Errorf("invalid version %q", opts.version)
	}
	if !repoPattern.MatchString(opts.repo) {
		return opts, fmt.Errorf("invalid repository %q, expected owner/name", opts.repo)
	}
	opts.binarySum = strings.ToLower(opts.binarySum)
	opts.cliSum = strings.ToLower(opts.cliSum)
	for _, sum := range []string{opts.binarySum, opts.cliSum} {
		if sum != "" && !sha256Pattern.MatchString(sum) {
			return opts, fmt.Errorf("invalid sha256 %q", sum)
		}
	}
	if (opts.binarySum == "") != (opts.cliSum == "") {
		return opts, errors.New("--sha256 and --xbctl-sha256 must be given together")
	}
	return opts, nil
}

func releaseAssetURL(repo, version, asset string) string {
	base := "https://github.com/" + repo + "/releases"
	if version == "latest" {
		return base + "/latest/download/" + asset
	}
	return base + "/download/" + version + "/" + asset
}

// parseChecksums reads `sha256sum` output ("<hex>  <name>" or "<hex> *<name>").
func parseChecksums(r io.Reader) (map[string]string, error) {
	sums := make(map[string]string)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("malformed checksum line %q", line)
		}
		sum := strings.ToLower(fields[0])
		name := strings.TrimPrefix(fields[1], "*")
		if !sha256Pattern.MatchString(sum) {
			return nil, fmt.Errorf("malformed checksum for %s", name)
		}
		sums[filepath.Base(name)] = sum
	}
	return sums, sc.Err()
}

// downloadVerified downloads url into a new private temp file in dir and
// verifies its sha256 before the file is ever made executable. The caller
// owns the returned path.
func downloadVerified(client *http.Client, url, dir, wantSum string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	f, err := os.CreateTemp(dir, ".xbctl-upgrade-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
	fail := func(err error) (string, error) {
		f.Close()
		os.Remove(path)
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return fail(err)
	}
	if n > maxDownloadBytes {
		return fail(fmt.Errorf("%s exceeds %d bytes", url, maxDownloadBytes))
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSum {
		return fail(fmt.Errorf("checksum mismatch for %s: got %s, want %s", url, got, wantSum))
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func fetchChecksums(client *http.Client, url string) (map[string]string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return parseChecksums(io.LimitReader(resp.Body, maxChecksumBytes))
}

// replaceFile atomically swaps dst for src, keeping the old file at dst.bak.
// The backup is a hard link where possible so it costs no extra space.
func replaceFile(src, dst string) (backup string, err error) {
	if fileExists(dst) {
		backup = dst + ".bak"
		os.Remove(backup)
		if err := os.Link(dst, backup); err != nil {
			if err := copyFile(dst, backup); err != nil {
				return "", fmt.Errorf("backup %s: %w", dst, err)
			}
		}
	}
	if err := os.Rename(src, dst); err != nil {
		return backup, fmt.Errorf("replace %s: %w", dst, err)
	}
	return backup, nil
}

func restoreFile(backup, dst string) error {
	if backup == "" {
		return os.Remove(dst)
	}
	tmp := dst + ".rollback"
	if err := copyFile(backup, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// waitServiceActive polls until the unit has stayed active for a short
// while; `systemctl restart` returns before a crashing process exits.
func waitServiceActive(unit string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	stable := 0
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		if exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil {
			stable++
			if stable >= 5 {
				return nil
			}
			continue
		}
		stable = 0
	}
	return fmt.Errorf("%s did not stay active within %s", unit, timeout)
}

func runUpgrade(args []string) error {
	if err := ensureRoot("upgrade"); err != nil {
		return err
	}
	opts, err := parseUpgradeArgs(args)
	if err != nil {
		return err
	}

	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported architecture: %s", arch)
	}

	lock, err := os.OpenFile(upgradeLockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open upgrade lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another upgrade is already running")
	}

	binaryAsset := "xboard-node-linux-" + arch
	cliAsset := "xbctl-linux-" + arch
	client := &http.Client{Timeout: 10 * time.Minute}

	fmt.Printf("Starting upgrade from %s (%s)...\n", opts.repo, opts.version)
	binarySum, cliSum := opts.binarySum, opts.cliSum
	if binarySum == "" {
		url := releaseAssetURL(opts.repo, opts.version, checksumAsset)
		sums, err := fetchChecksums(client, url)
		if err != nil {
			return fmt.Errorf("download %s: %w (pass --sha256 and --xbctl-sha256 to verify manually)", checksumAsset, err)
		}
		binarySum, cliSum = sums[binaryAsset], sums[cliAsset]
		if binarySum == "" || cliSum == "" {
			return fmt.Errorf("%s does not list %s and %s", checksumAsset, binaryAsset, cliAsset)
		}
	}

	fmt.Printf("Downloading %s...\n", binaryAsset)
	newBinary, err := downloadVerified(client, releaseAssetURL(opts.repo, opts.version, binaryAsset), filepath.Dir(defaultBinaryPath), binarySum)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	defer os.Remove(newBinary)
	fmt.Printf("Downloading %s...\n", cliAsset)
	newCLI, err := downloadVerified(client, releaseAssetURL(opts.repo, opts.version, cliAsset), filepath.Dir(defaultCLIPath), cliSum)
	if err != nil {
		return fmt.Errorf("download xbctl: %w", err)
	}
	defer os.Remove(newCLI)

	for _, p := range []string{newBinary, newCLI} {
		if err := os.Chmod(p, 0o755); err != nil {
			return fmt.Errorf("chmod %s: %w", p, err)
		}
	}
	// Executed only after the checksum matched.
	if out, err := exec.Command(newBinary, "-v").CombinedOutput(); err != nil {
		return fmt.Errorf("binary version check failed: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command(newCLI, "version").CombinedOutput(); err != nil {
		return fmt.Errorf("xbctl version check failed: %s", strings.TrimSpace(string(out)))
	}

	backupBinary, err := replaceFile(newBinary, defaultBinaryPath)
	if err != nil {
		return err
	}
	backupCLI, err := replaceFile(newCLI, defaultCLIPath)
	if err != nil {
		if rerr := restoreFile(backupBinary, defaultBinaryPath); rerr != nil {
			return fmt.Errorf("%v; restoring binary failed: %w", err, rerr)
		}
		return err
	}

	// Only (re)create the convenience symlink; never replace a real file.
	if fi, err := os.Lstat("/usr/bin/xbctl"); os.IsNotExist(err) || (err == nil && fi.Mode()&os.ModeSymlink != 0) {
		os.Remove("/usr/bin/xbctl")
		os.Symlink(defaultCLIPath, "/usr/bin/xbctl")
	}

	fmt.Println("Restarting service...")
	runCommand("systemctl", "daemon-reload")
	restartErr := runCommand("systemctl", "restart", serviceName)
	if restartErr == nil {
		restartErr = waitServiceActive(serviceName, healthWaitTimeout)
	}
	if restartErr != nil {
		fmt.Printf("Upgrade failed (%v), rolling back...\n", restartErr)
		var errs []error
		if err := restoreFile(backupBinary, defaultBinaryPath); err != nil {
			errs = append(errs, fmt.Errorf("restore binary: %w", err))
		}
		if err := restoreFile(backupCLI, defaultCLIPath); err != nil {
			errs = append(errs, fmt.Errorf("restore xbctl: %w", err))
		}
		runCommand("systemctl", "daemon-reload")
		if err := runCommand("systemctl", "restart", serviceName); err != nil {
			errs = append(errs, fmt.Errorf("restart after rollback: %w", err))
		} else if err := waitServiceActive(serviceName, healthWaitTimeout); err != nil {
			errs = append(errs, err)
		}
		if len(errs) > 0 {
			return fmt.Errorf("upgrade failed and rollback incomplete (backups: %s, %s): %w", backupBinary, backupCLI, errors.Join(errs...))
		}
		return errors.New("upgrade failed: previous version restored and running")
	}

	newVer := "unknown"
	if out, err := exec.Command(defaultBinaryPath, "-v").CombinedOutput(); err == nil {
		newVer = strings.TrimSpace(string(out))
	}
	if root, err := loadWritableRootConfig(defaultConfigPath); err == nil {
		instances, _ := root.NormalizeInstances()
		writeInstallMetaVersioned(defaultMetaPath, root, newVer, latestInstanceID(instances))
	}

	fmt.Printf("Upgrade complete (version: %s). Previous binaries kept as %s and %s\n", newVer, backupBinary, backupCLI)
	return nil
}
