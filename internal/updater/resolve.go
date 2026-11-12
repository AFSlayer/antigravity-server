package updater

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultReleasesURL  = "https://antigravity-hub-auto-updater-974169037036.us-central1.run.app/releases"
	DefaultDownloadPage = "https://antigravity.google/download"

	hubBucketBaseURL = "https://storage.googleapis.com/antigravity-public/antigravity-hub"
	maxMetadataBytes = 4 << 20
)

var (
	hubURLRe           = regexp.MustCompile(`https://storage\.googleapis\.com/antigravity-public/antigravity-hub/[^"'<> ]+`)
	hubBuildVersionRe  = regexp.MustCompile(`/antigravity-hub/(([0-9][0-9.]*)-[0-9]+)/`)
	autoUpdaterURLRe   = regexp.MustCompile(`https://antigravity-hub-auto-updater-[a-z0-9-]+\.[a-z0-9.-]+\.run\.app/releases`)
	releaseVersionRe   = regexp.MustCompile(`^\d+\.\d+(?:\.\d+)*$`)
	releaseExecutionRe = regexp.MustCompile(`^\d+$`)
)

// ValidateDownloadURL verifies that the URL points to the trusted Google Cloud Storage hub bucket
// or a local loopback test server, rejecting userinfo (@), path traversal (..), and encoded slashes.
func ValidateDownloadURL(rawURL string) error {
	if rawURL == "" || strings.TrimSpace(rawURL) != rawURL {
		return fmt.Errorf("untrusted download URL domain: %s", rawURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return fmt.Errorf("untrusted download URL domain: %s", rawURL)
	}
	rawPath := u.EscapedPath()
	lowerRawPath := strings.ToLower(rawPath)
	if rawPath == "" || !strings.HasPrefix(rawPath, "/") ||
		strings.Contains(u.Path, `\`) ||
		strings.Contains(lowerRawPath, "%2f") || strings.Contains(lowerRawPath, "%5c") ||
		hasDotPathSegment(rawPath) || hasDotPathSegment(u.Path) {
		return fmt.Errorf("untrusted download URL domain: %s", rawURL)
	}

	switch u.Scheme {
	case "https":
		if !strings.EqualFold(u.Hostname(), "storage.googleapis.com") {
			return fmt.Errorf("untrusted download URL domain: %s", rawURL)
		}
		if port := u.Port(); port != "" && port != "443" {
			return fmt.Errorf("untrusted download URL domain: %s", rawURL)
		}
		const hubPrefix = "/antigravity-public/antigravity-hub/"
		if !strings.HasPrefix(rawPath, hubPrefix) || len(rawPath) <= len(hubPrefix) || !strings.HasPrefix(path.Clean(u.Path), hubPrefix) {
			return fmt.Errorf("untrusted download URL domain: %s", rawURL)
		}
		return nil
	case "http":
		host := u.Hostname()
		if host != "127.0.0.1" && !strings.EqualFold(host, "localhost") {
			return fmt.Errorf("untrusted download URL domain: %s", rawURL)
		}
		port, perr := strconv.Atoi(u.Port())
		if perr != nil || port < 1 || port > 65535 {
			return fmt.Errorf("untrusted download URL domain: %s", rawURL)
		}
		return nil
	default:
		return fmt.Errorf("untrusted download URL domain: %s", rawURL)
	}
}

func hasDotPathSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		switch strings.ToLower(seg) {
		case ".", "..", "%2e", "%2e%2e", ".%2e", "%2e.":
			return true
		}
	}
	return false
}

// PlatformSlug maps runtime.GOOS and runtime.GOARCH to the official Antigravity hub artifact path.
func PlatformSlug(goos, goarch string) (string, error) {
	switch goos {
	case "linux":
		switch goarch {
		case "amd64":
			return "linux-x64/Antigravity.tar.gz", nil
		case "arm64":
			return "linux-arm/Antigravity.tar.gz", nil
		}
	case "darwin":
		switch goarch {
		case "arm64":
			return "darwin-arm/Antigravity.dmg", nil
		case "amd64":
			return "darwin-x64/Antigravity.dmg", nil
		}
	case "windows":
		switch goarch {
		case "amd64":
			return "windows-x64/Antigravity-x64.exe", nil
		case "arm64":
			return "windows-arm/Antigravity-arm64.exe", nil
		}
	}
	return "", fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
}

// UpdateInfo holds metadata about an official Antigravity language_server release.
type UpdateInfo struct {
	Platform        string `json:"platform"`
	LatestVersion   string `json:"latest_version"`
	RawBuildVersion string `json:"raw_build_version"`
	DownloadURL     string `json:"download_url"`
	UpdateAvailable bool   `json:"update_available"`
}

type hubReleaseEntry struct {
	Version     string `json:"version"`
	ExecutionID string `json:"execution_id"`
}

type hubReleasesEnvelope struct {
	Versions []hubReleaseEntry `json:"versions"`
}

func fetchMetadataBody(client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "antigravity-server-updater")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", rawURL, resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("decompress gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxMetadataBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if len(body) > maxMetadataBytes {
		return nil, fmt.Errorf("response body from %s exceeds %d bytes", rawURL, maxMetadataBytes)
	}
	return body, nil
}

func hubArtifactURL(version, executionID, goos, goarch, slug string) string {
	artifactSlug := slug
	if goos == "windows" && version == "2.0.0" {
		switch goarch {
		case "amd64":
			artifactSlug = "windows-x64/Antigravity.exe"
		case "arm64":
			artifactSlug = "windows-arm/Antigravity.exe"
		}
	}
	return fmt.Sprintf("%s/%s-%s/%s", hubBucketBaseURL, version, executionID, artifactSlug)
}

func parseReleasesJSON(body []byte, slug, goos, goarch string) (*UpdateInfo, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errors.New("empty releases payload")
	}

	var entries []hubReleaseEntry
	switch trimmed[0] {
	case '[':
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return nil, fmt.Errorf("parse releases array: %w", err)
		}
	case '{':
		var env hubReleasesEnvelope
		if err := json.Unmarshal(trimmed, &env); err != nil {
			return nil, fmt.Errorf("parse releases object: %w", err)
		}
		entries = env.Versions
	default:
		return nil, errors.New("unexpected releases payload format")
	}

	if len(entries) == 0 {
		return nil, errors.New("releases feed returned no versions")
	}

	latest := entries[0]
	if !releaseVersionRe.MatchString(latest.Version) || !releaseExecutionRe.MatchString(latest.ExecutionID) {
		return nil, fmt.Errorf("invalid release entry: version=%q execution_id=%q", latest.Version, latest.ExecutionID)
	}

	return &UpdateInfo{
		Platform:        fmt.Sprintf("%s/%s", goos, goarch),
		LatestVersion:   latest.Version,
		RawBuildVersion: latest.Version + "-" + latest.ExecutionID,
		DownloadURL:     hubArtifactURL(latest.Version, latest.ExecutionID, goos, goarch, slug),
	}, nil
}

func parseDownloadPageHTML(client *http.Client, body []byte, slug, goos, goarch, skipReleasesURL string) (*UpdateInfo, error) {
	text := string(body)

	var staticInfo *UpdateInfo
	for _, m := range hubURLRe.FindAllString(text, -1) {
		if strings.HasSuffix(m, slug) && ValidateDownloadURL(m) == nil {
			rawBuild := ""
			version := "unknown"
			if verMatch := hubBuildVersionRe.FindStringSubmatch(m); len(verMatch) > 2 {
				rawBuild = verMatch[1]
				version = verMatch[2]
			}
			staticInfo = &UpdateInfo{
				Platform:        fmt.Sprintf("%s/%s", goos, goarch),
				LatestVersion:   version,
				RawBuildVersion: rawBuild,
				DownloadURL:     m,
			}
			break
		}
	}

	if dynURL := autoUpdaterURLRe.FindString(text); dynURL != "" && dynURL != skipReleasesURL && client != nil {
		if dynBody, err := fetchMetadataBody(client, dynURL); err == nil {
			if dynInfo, parseErr := parseReleasesJSON(dynBody, slug, goos, goarch); parseErr == nil {
				if staticInfo == nil || staticInfo.LatestVersion == "unknown" || !isNewerVersion(staticInfo.LatestVersion, dynInfo.LatestVersion) {
					return dynInfo, nil
				}
			}
		}
	}

	if staticInfo != nil {
		return staticInfo, nil
	}
	return nil, fmt.Errorf("no official download artifact found for %s (%s)", slug, goos+"/"+goarch)
}

// ResolveLatest queries the official Antigravity hub releases feed (or a custom metadata URL)
// and discovers the latest build for the requested platform, falling back to the download page
// if the primary releases feed is unreachable.
func ResolveLatest(sourceURL string, goos, goarch string) (*UpdateInfo, error) {
	slug, err := PlatformSlug(goos, goarch)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}

	if sourceURL == "" {
		if relBody, relErr := fetchMetadataBody(client, DefaultReleasesURL); relErr == nil {
			if info, parseErr := parseReleasesJSON(relBody, slug, goos, goarch); parseErr == nil {
				return info, nil
			} else {
				err = parseErr
			}
		} else {
			err = relErr
		}

		pageBody, pageErr := fetchMetadataBody(client, DefaultDownloadPage)
		if pageErr != nil {
			return nil, fmt.Errorf("releases feed failed (%v) and fallback page failed: %w", err, pageErr)
		}
		info, parseErr := parseDownloadPageHTML(client, pageBody, slug, goos, goarch, DefaultReleasesURL)
		if parseErr != nil {
			return nil, fmt.Errorf("releases feed failed (%v) and fallback page parse failed: %w", err, parseErr)
		}
		return info, nil
	}

	body, err := fetchMetadataBody(client, sourceURL)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
		return parseReleasesJSON(trimmed, slug, goos, goarch)
	}
	return parseDownloadPageHTML(client, body, slug, goos, goarch, "")
}

// isNewerVersion reports whether latest is strictly newer than current.
// When both strings parse as dot-separated numeric release versions, older
// upstream snapshots (such as a cached static download page lagging behind an
// installed point release) return false instead of triggering a downgrade.
func isNewerVersion(latest, current string) bool {
	if latest == current {
		return false
	}
	if cmp, ok := compareReleaseVersions(latest, current); ok {
		return cmp > 0
	}
	return true
}

func compareReleaseVersions(a, b string) (int, bool) {
	aCore, aPre, aOK := parseReleaseVersion(a)
	bCore, bPre, bOK := parseReleaseVersion(b)
	if !aOK || !bOK {
		return 0, false
	}
	maxLen := len(aCore)
	if len(bCore) > maxLen {
		maxLen = len(bCore)
	}
	for i := 0; i < maxLen; i++ {
		var av, bv int
		if i < len(aCore) {
			av = aCore[i]
		}
		if i < len(bCore) {
			bv = bCore[i]
		}
		if av != bv {
			if av > bv {
				return 1, true
			}
			return -1, true
		}
	}
	switch {
	case aPre == "" && bPre != "":
		return 1, true
	case aPre != "" && bPre == "":
		return -1, true
	case aPre != bPre:
		if aPre > bPre {
			return 1, true
		}
		return -1, true
	default:
		return 0, true
	}
}

func parseReleaseVersion(raw string) ([]int, string, bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}
	pre := ""
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}
	if s == "" {
		return nil, "", false
	}
	parts := strings.Split(s, ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		if p == "" {
			return nil, "", false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, "", false
		}
		nums[i] = n
	}
	return nums, pre, true
}

// CheckUpdate compares the installed version with the latest official version.
func CheckUpdate(currentVersion string) (*UpdateInfo, error) {
	info, err := ResolveLatest("", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	if currentVersion != "" && currentVersion != "unknown" && info.LatestVersion != "" && info.LatestVersion != "unknown" {
		info.UpdateAvailable = isNewerVersion(info.LatestVersion, currentVersion)
	}
	return info, nil
}
