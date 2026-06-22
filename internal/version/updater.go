package version

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	githubRepo    = "JohnnyVBut/cascade"
	checkInterval = 24 * time.Hour
	httpTimeout   = 10 * time.Second
	// Delay first check so the container has time to come fully online.
	initialDelay = 10 * time.Second
)

// UpdateStatus is the cached result of the last update check.
type UpdateStatus struct {
	LatestVersion   string    `json:"latestVersion"`
	ReleaseURL      string    `json:"releaseURL"`
	UpdateAvailable bool      `json:"updateAvailable"`
	CheckedAt       time.Time `json:"checkedAt"`
	Error           string    `json:"error,omitempty"`
}

var (
	mu     sync.RWMutex
	status UpdateStatus
)

// GetStatus returns the latest cached UpdateStatus (safe for concurrent use).
func GetStatus() UpdateStatus {
	mu.RLock()
	defer mu.RUnlock()
	return status
}

// Start launches the background update-check goroutine.
// It checks immediately after initialDelay, then every checkInterval.
// Safe to call multiple times — only the first call has effect.
func Start() {
	go func() {
		time.Sleep(initialDelay)
		check()
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()
		for range ticker.C {
			check()
		}
	}()
}

// Check forces an immediate update check, bypassing the 24h cache.
// Safe to call concurrently — it runs synchronously and updates the shared status.
func Check() {
	check()
}

// check fetches the latest release from GitHub and updates the cached status.
func check() {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)

	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		setError(fmt.Sprintf("build request: %v", err))
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cascade-update-checker/"+Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		setError(fmt.Sprintf("http: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		setError(fmt.Sprintf("github api returned %d", resp.StatusCode))
		return
	}

	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		setError(fmt.Sprintf("decode: %v", err))
		return
	}

	available := compareSemver(payload.TagName, Version) > 0
	log.Printf("version: update check — current=%s latest=%s updateAvailable=%v",
		Version, payload.TagName, available)

	mu.Lock()
	status = UpdateStatus{
		LatestVersion:   payload.TagName,
		ReleaseURL:      payload.HTMLURL,
		UpdateAvailable: available,
		CheckedAt:       time.Now().UTC(),
	}
	mu.Unlock()
}

func setError(msg string) {
	log.Printf("version: update check failed: %s", msg)
	mu.Lock()
	status.Error = msg
	status.CheckedAt = time.Now().UTC()
	mu.Unlock()
}

// compareSemver returns:
//
//	-1  if a < b
//	 0  if a == b
//	+1  if a > b
//
// Handles "v1.2.3", "1.2.3", "v1.2.3-rc1" (pre-release suffix stripped).
// Non-parseable or dev-mode versions ("dev") are treated as 0.0.0.
func compareSemver(a, b string) int {
	pa := parseSemver(a)
	pb := parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	// Strip pre-release suffix (e.g. "-rc1", "-alpha")
	if idx := strings.IndexByte(v, '-'); idx != -1 {
		v = v[:idx]
	}
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		out[i], _ = strconv.Atoi(parts[i])
	}
	return out
}
