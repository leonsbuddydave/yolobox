package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func resetVolumes(args []string) error {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "remove volumes")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return errHelp
		}
		return err
	}
	if !*force {
		return fmt.Errorf("reset requires --force (this will delete all cached data)")
	}

	cfg, err := loadConfigFromEnv()
	if err != nil {
		return err
	}
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}

	warn("Removing yolobox volumes...")
	volumes := []string{"yolobox-home", "yolobox-cache"}
	args = append([]string{"volume", "rm"}, volumes...)
	if err := execCommand(runtimePath, args); err != nil {
		return err
	}
	success("Fresh start! All volumes removed.")
	return nil
}

func uninstallYolobox(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "confirm uninstall")
	keepVolumes := fs.Bool("keep-volumes", false, "keep Docker volumes")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return errHelp
		}
		return err
	}
	if !*force {
		fmt.Println("This will remove:")
		fmt.Println("  - yolobox binary")
		fmt.Println("  - ~/.config/yolobox/ (config and cache)")
		if !*keepVolumes {
			fmt.Println("  - Docker volumes (yolobox-home, yolobox-cache)")
		}
		fmt.Println("")
		return fmt.Errorf("run with --force to confirm (use --keep-volumes to preserve Docker data)")
	}

	configDir, err := os.UserConfigDir()
	if err == nil {
		yoloboxConfig := filepath.Join(configDir, "yolobox")
		if _, err := os.Stat(yoloboxConfig); err == nil {
			info("Removing %s...", yoloboxConfig)
			_ = os.RemoveAll(yoloboxConfig)
		}
	}

	if !*keepVolumes {
		cfg, err := loadConfigFromEnv()
		if err == nil {
			runtimePath, err := resolveRuntime(cfg.Runtime)
			if err == nil {
				info("Removing Docker volumes...")
				volumes := []string{"yolobox-home", "yolobox-cache", "yolobox-output"}
				_ = execCommand(runtimePath, append([]string{"volume", "rm", "-f"}, volumes...))
			}
		}
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find executable path: %w", err)
	}

	info("Removing %s...", execPath)
	if err := os.Remove(execPath); err != nil {
		return fmt.Errorf("failed to remove binary: %w (try: sudo rm %s)", err, execPath)
	}

	success("yolobox has been uninstalled. Goodbye!")
	return nil
}

func execCommand(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func upgradeYolobox() error {
	info("Checking for updates...")

	resp, err := http.Get("https://api.github.com/repos/leonsbuddydave/yolobox/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to check for updates: HTTP %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("failed to parse release info: %w", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(Version, "v")

	if latestVersion == currentVersion {
		success("Already at latest version (%s)", Version)
	} else {
		info("New version available: %s (current: %s)", latestVersion, currentVersion)

		binaryName := fmt.Sprintf("yolobox-%s-%s", runtime.GOOS, runtime.GOARCH)
		var downloadURL string
		for _, asset := range release.Assets {
			if asset.Name == binaryName {
				downloadURL = asset.BrowserDownloadURL
				break
			}
		}

		if downloadURL == "" {
			return fmt.Errorf("no binary available for %s/%s", runtime.GOOS, runtime.GOARCH)
		}

		info("Downloading %s...", binaryName)
		resp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("failed to download: %w", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != 200 {
			return fmt.Errorf("failed to download: HTTP %d", resp.StatusCode)
		}

		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return fmt.Errorf("failed to resolve executable path: %w", err)
		}

		tmpFile, err := os.CreateTemp(filepath.Dir(execPath), "yolobox-upgrade-*")
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()

		_, err = io.Copy(tmpFile, resp.Body)
		if closeErr := tmpFile.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to write binary: %w", err)
		}

		if err := os.Chmod(tmpPath, 0755); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to chmod: %w", err)
		}

		if err := os.Rename(tmpPath, execPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to replace binary: %w", err)
		}

		success("Binary upgraded to %s", latestVersion)
	}

	info("Pulling latest Docker image...")
	cfg := defaultConfig()
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}
	if err := execCommand(runtimePath, []string{"pull", cfg.Image}); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	success("Upgrade complete!")
	return nil
}
