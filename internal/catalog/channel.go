package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

const (
	defaultChannelVideoLimit   = 24
	defaultChannelFetchTimeout = 10 * time.Second
)

// FetchChannelVideos retrieves videos for a YouTube channel by channel ID, handle,
// channel URL, or fallback channel name search using yt-dlp --flat-playlist --dump-json.
func FetchChannelVideos(
	ctx context.Context,
	ytdlpBin string,
	cookiesFile string,
	channelID string,
	channelName string,
	limit int,
) ([]model.Video, error) {
	return fetchChannelVideos(ctx, ytdlpBin, cookiesFile, channelID, channelName, limit, nil)
}

func fetchChannelVideos(
	ctx context.Context,
	ytdlpBin string,
	cookiesFile string,
	channelID string,
	channelName string,
	limit int,
	extraArgs []string,
) ([]model.Video, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = defaultChannelVideoLimit
	}

	target, err := resolveChannelTarget(channelID, channelName, limit)
	if err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultChannelFetchTimeout)
		defer cancel()
	}

	bin := strings.TrimSpace(ytdlpBin)
	if bin == "" {
		bin = "yt-dlp"
	}

	args := []string{
		"--flat-playlist",
		"--playlist-end", strconv.Itoa(limit),
		"--dump-json",
		"--no-warnings",
		"--ignore-errors",
		"--socket-timeout", "8",
	}

	cookies := strings.TrimSpace(cookiesFile)
	if cookies != "" {
		if info, statErr := os.Stat(cookies); statErr == nil && !info.IsDir() && info.Size() > 0 {
			args = append(args, "--cookies", cookies)
		}
	}
	if len(extraArgs) > 0 {
		args = append(args, extraArgs...)
	}

	args = append(args, target)

	runner := &YTDLPCatalog{BinPath: bin}
	out, err := runner.runYTDLP(ctx, args...)
	if err != nil {
		return nil, err
	}

	return parseYTDLPOutput(out)
}

// ChannelVideos retrieves recent videos from the video's channel using this YTDLPCatalog's
// configured binary path, cookie jar, and extra yt-dlp arguments.
func (c *YTDLPCatalog) ChannelVideos(ctx context.Context, v model.Video, limit int) ([]model.Video, error) {
	bin := ""
	cookies := ""
	var extra []string
	if c != nil {
		bin = c.BinPath
		cookies = c.cookieJarPath()
		extra = c.extraArgs()
	}
	channelID, channelName := videoChannelRef(v)
	return fetchChannelVideos(ctx, bin, cookies, channelID, channelName, limit, extra)
}

// ResolveChannelReference resolves a YouTube channel reference (UC ID, /channel/UC... URL,
// @handle, /@handle URL, /c/..., or /user/...) into a canonical UC... channel ID and
// optional display label. Direct UC references resolve in 0ms without network I/O.
func ResolveChannelReference(ctx context.Context, ytdlpBin, rawRef string) (channelID, label string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}

	ref := strings.TrimSpace(rawRef)
	if ref == "" {
		return "", "", fmt.Errorf("catalog: empty channel reference")
	}

	if feedURL := ChannelFeedURL(ref); feedURL != "" {
		if cid := channelIDFromFeedURL(feedURL); strings.HasPrefix(cid, "UC") && len(cid) > 2 {
			return cid, "", nil
		}
	}

	channelURL, err := resolveChannelReferenceURL(ref)
	if err != nil {
		return "", "", err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultChannelFetchTimeout)
		defer cancel()
	}

	bin := strings.TrimSpace(ytdlpBin)
	if bin == "" {
		bin = "yt-dlp"
	}

	args := []string{
		"--no-warnings",
		"--playlist-items", "1",
		"--flat-playlist",
		"--dump-single-json",
		channelURL,
	}

	runner := &YTDLPCatalog{BinPath: bin}
	out, err := runner.runYTDLP(ctx, args...)
	if err != nil {
		return "", "", err
	}

	var doc struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
		Channel   string `json:"channel"`
		Uploader  string `json:"uploader"`
		Title     string `json:"title"`
		Entries   []struct {
			ChannelID string `json:"channel_id"`
			Channel   string `json:"channel"`
			Uploader  string `json:"uploader"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &doc); err != nil {
		return "", "", fmt.Errorf("catalog: decode channel json for %q: %w", ref, err)
	}

	cid := firstUCID(doc.ChannelID, doc.ID)
	if cid == "" && len(doc.Entries) > 0 {
		cid = firstUCID(doc.Entries[0].ChannelID)
	}
	if cid == "" {
		return "", "", fmt.Errorf("catalog: could not resolve UC channel ID for %q", ref)
	}

	lbl := strings.TrimSpace(doc.Channel)
	if lbl == "" {
		lbl = strings.TrimSpace(doc.Uploader)
	}
	if lbl == "" && len(doc.Entries) > 0 {
		lbl = strings.TrimSpace(doc.Entries[0].Channel)
		if lbl == "" {
			lbl = strings.TrimSpace(doc.Entries[0].Uploader)
		}
	}
	if lbl == "" {
		lbl = strings.TrimSuffix(strings.TrimSpace(doc.Title), " - Videos")
	}

	return cid, lbl, nil
}

func firstUCID(candidates ...string) string {
	for _, raw := range candidates {
		s := strings.TrimSpace(raw)
		if strings.HasPrefix(s, "UC") && len(s) > 2 && !strings.ContainsAny(s, "/?# \t\r\n") {
			return s
		}
	}
	return ""
}

func resolveChannelReferenceURL(ref string) (string, error) {
	if strings.HasPrefix(ref, "@") && len(ref) > 1 && !strings.ContainsAny(ref, " \t\r\n?#") {
		handle := strings.TrimRight(ref, "/")
		if handle == "@" {
			return "", fmt.Errorf("catalog: invalid channel handle %q", ref)
		}
		return "https://www.youtube.com/" + handle, nil
	}

	candidate := ref
	if !strings.Contains(candidate, "://") {
		if !strings.Contains(strings.ToLower(candidate), "youtube.com/") {
			return "", fmt.Errorf("catalog: unsupported channel reference %q", ref)
		}
		candidate = "https://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("catalog: invalid channel URL %q: %w", ref, err)
	}
	host := strings.ToLower(u.Hostname())
	if host != "youtube.com" && !strings.HasSuffix(host, ".youtube.com") {
		return "", fmt.Errorf("catalog: non-YouTube URL %q", ref)
	}
	cleanPath := strings.TrimRight(u.Path, "/")
	if cleanPath == "" ||
		(!strings.HasPrefix(cleanPath, "/@") &&
			!strings.HasPrefix(cleanPath, "/c/") &&
			!strings.HasPrefix(cleanPath, "/user/") &&
			!strings.HasPrefix(cleanPath, "/channel/")) {
		return "", fmt.Errorf("catalog: unsupported YouTube channel path in %q", ref)
	}
	if host == "youtube.com" || host == "m.youtube.com" {
		u.Host = "www.youtube.com"
	}
	u.Scheme = "https"
	u.Path = cleanPath
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// ChannelVideos retrieves recent videos from the video's channel, delegating to
// Direct when configured or falling back to default yt-dlp execution.
func (h *HybridCatalog) ChannelVideos(ctx context.Context, v model.Video, limit int) ([]model.Video, error) {
	if h != nil && h.Direct != nil {
		return h.Direct.ChannelVideos(ctx, v, limit)
	}
	channelID, channelName := videoChannelRef(v)
	return FetchChannelVideos(ctx, "yt-dlp", "", channelID, channelName, limit)
}

func videoChannelRef(v model.Video) (channelID, channelName string) {
	channelID, channelName = inferChannelRef(v.Channel)
	if cid := strings.TrimSpace(v.ChannelID); cid != "" {
		channelID = cid
	}
	return channelID, channelName
}

func resolveChannelTarget(channelID, channelName string, limit int) (string, error) {
	id := strings.TrimSpace(channelID)
	name := strings.TrimSpace(channelName)

	switch {
	case strings.HasPrefix(id, "UC") && len(id) > 2 && !strings.ContainsAny(id, " \t\r\n?#"):
		cleaned := strings.TrimRight(id, "/")
		if !strings.Contains(cleaned, "/") {
			return "https://www.youtube.com/channel/" + cleaned + "/videos", nil
		}
	case strings.HasPrefix(id, "@") && len(id) > 1 && !strings.ContainsAny(id, " \t\r\n?#"):
		handle := strings.TrimRight(id, "/")
		if !strings.HasSuffix(handle, "/videos") {
			handle += "/videos"
		}
		return "https://www.youtube.com/" + handle, nil
	case strings.Contains(id, "youtube.com/"):
		if normalized := normalizeYouTubeChannelURL(id); normalized != "" {
			return normalized, nil
		}
	}

	if name != "" {
		return fmt.Sprintf("ytsearch%d:%s", limit, name), nil
	}

	return "", fmt.Errorf("catalog: channel drill-down requires a channel ID/URL or channel name")
}

func normalizeYouTubeChannelURL(raw string) string {
	candidate := strings.TrimSpace(raw)
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return ""
	}

	host := strings.ToLower(u.Hostname())
	if host != "youtube.com" && !strings.HasSuffix(host, ".youtube.com") {
		return ""
	}

	if strings.HasSuffix(u.Path, "/feeds/videos.xml") {
		if cid := strings.TrimSpace(u.Query().Get("channel_id")); cid != "" {
			return "https://www.youtube.com/channel/" + cid + "/videos"
		}
	}

	cleanPath := strings.TrimRight(u.Path, "/")
	if cleanPath == "" || cleanPath == "/watch" || cleanPath == "/playlist" || cleanPath == "/results" {
		return ""
	}

	switch {
	case strings.HasSuffix(cleanPath, "/videos"),
		strings.HasSuffix(cleanPath, "/streams"),
		strings.HasSuffix(cleanPath, "/shorts"):
	case strings.HasSuffix(cleanPath, "/featured"):
		cleanPath = strings.TrimSuffix(cleanPath, "/featured") + "/videos"
	default:
		cleanPath += "/videos"
	}

	if host == "youtube.com" || host == "m.youtube.com" {
		u.Host = "www.youtube.com"
	}
	u.Scheme = "https"
	u.Path = cleanPath
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func inferChannelRef(channel string) (channelID, channelName string) {
	trimmed := strings.TrimSpace(channel)
	if trimmed == "" {
		return "", ""
	}
	if strings.Contains(trimmed, "youtube.com/") && !strings.ContainsAny(trimmed, " \t\r\n") {
		return trimmed, trimmed
	}
	if strings.HasPrefix(trimmed, "@") && len(trimmed) > 1 && !strings.ContainsAny(trimmed, " \t\r\n") {
		return trimmed, trimmed
	}
	if isCanonicalYouTubeChannelID(trimmed) {
		return trimmed, trimmed
	}
	return "", trimmed
}

func isCanonicalYouTubeChannelID(s string) bool {
	if len(s) != 24 || !strings.HasPrefix(s, "UC") {
		return false
	}
	for i := 2; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}
