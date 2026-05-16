package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type versionCache struct {
	LatestVersion string    `json:"latest_version"`
	CheckedAt     time.Time `json:"checked_at"`
}

const versionCheckInterval = 24 * time.Hour

func versionCachePath() string {
	configDir, _ := os.UserConfigDir()
	return filepath.Join(configDir, "yolobox", "version-check.json")
}

func checkForUpdates() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		doVersionCheck()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func doVersionCheck() {
	cachePath := versionCachePath()

	var cache versionCache
	if data, err := os.ReadFile(cachePath); err == nil {
		if err := json.Unmarshal(data, &cache); err == nil {
			if time.Since(cache.CheckedAt) < versionCheckInterval {
				showUpdateMessage(cache.LatestVersion)
				return
			}
		}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/leonsbuddydave/yolobox/releases/latest")
	if err != nil {
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	cache = versionCache{
		LatestVersion: latestVersion,
		CheckedAt:     time.Now(),
	}
	if data, err := json.Marshal(cache); err == nil {
		if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err == nil {
			_ = os.WriteFile(cachePath, data, 0644)
		}
	}

	showUpdateMessage(latestVersion)
}

func showUpdateMessage(latestVersion string) {
	if isNewerVersion(latestVersion, Version) {
		fmt.Fprintf(os.Stderr, "\n%s💡 yolobox v%s available:%s https://github.com/leonsbuddydave/yolobox/releases/tag/v%s\n",
			colorYellow, latestVersion, colorReset, latestVersion)
		fmt.Fprintf(os.Stderr, "   Run %syolobox upgrade%s to update\n\n", colorBold, colorReset)
	}
}

func comparableVersion(version string) string {
	match := versionPattern.FindString(strings.TrimSpace(version))
	if match == "" {
		return ""
	}
	if strings.HasPrefix(match, "v") {
		return match
	}
	return "v" + match
}

func isNewerVersion(latestVersion, currentVersion string) bool {
	latest := comparableVersion(latestVersion)
	if latest == "" {
		return false
	}
	current := comparableVersion(currentVersion)
	if current == "" {
		return true
	}
	return compareSemver(latest, current) > 0
}

func compareSemver(a, b string) int {
	parse := func(version string) [3]int {
		version = strings.TrimPrefix(version, "v")
		var parts [3]int
		for i, part := range strings.SplitN(version, ".", 3) {
			parts[i], _ = strconv.Atoi(part)
		}
		return parts
	}

	av := parse(a)
	bv := parse(b)
	for i := 0; i < len(av); i++ {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}

func printVersion() {
	fmt.Printf("%syolobox%s %s%s%s (%s/%s)\n", colorBold, colorReset, colorCyan, Version, colorReset, runtime.GOOS, runtime.GOARCH)
}
