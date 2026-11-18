package catalog

import (
	"context"
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

	args = append(args, target)

	runner := &YTDLPCatalog{BinPath: bin}
	out, err := runner.runYTDLP(ctx, args...)
	if err != nil {
		return nil, err
	}

	return parseYTDLPOutput(out)
}

// ChannelVideos retrieves recent videos from the video's channel using this YTDLPCatalog's
// configured binary path and cookie jar.
func (c *YTDLPCatalog) ChannelVideos(ctx context.Context, v model.Video, limit int) ([]model.Video, error) {
	bin := ""
	cookies := ""
	if c != nil {
		bin = c.BinPath
		cookies = c.CookieJarPath
	}
	channelID, channelName := inferChannelRef(v.Channel)
	return FetchChannelVideos(ctx, bin, cookies, channelID, channelName, limit)
}

// ChannelVideos retrieves recent videos from the video's channel, delegating to
// Direct when configured or falling back to default yt-dlp execution.
func (h *HybridCatalog) ChannelVideos(ctx context.Context, v model.Video, limit int) ([]model.Video, error) {
	if h != nil && h.Direct != nil {
		return h.Direct.ChannelVideos(ctx, v, limit)
	}
	channelID, channelName := inferChannelRef(v.Channel)
	return FetchChannelVideos(ctx, "yt-dlp", "", channelID, channelName, limit)
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
