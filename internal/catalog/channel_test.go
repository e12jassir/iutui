package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/e12jassir/ytx/internal/model"
)

func TestFetchChannelVideos_TargetAndArgs(t *testing.T) {
	tests := []struct {
		name        string
		channelID   string
		channelName string
		limit       int
		setupCookie bool
		jsonOutput  string
		wantTarget  string
		wantLimit   string
		wantCookies bool
		wantIDs     []string
	}{
		{
			name:        "UC channel ID resolves to channel videos URL",
			channelID:   "UC_x5XG1OV2P6uZZ5FSM9Ttw",
			channelName: "Google Developers",
			limit:       12,
			jsonOutput:  `{"id":"vid11111","title":"Go Talk","channel":"Google Developers","duration":180,"view_count":5000}` + "\n",
			wantTarget:  "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/videos",
			wantLimit:   "12",
			wantIDs:     []string{"vid11111"},
		},
		{
			name:        "@handle resolves to handle videos URL with default limit",
			channelID:   "@PinkFloyd",
			channelName: "",
			limit:       0,
			jsonOutput:  `{"id":"pf111111","title":"Time","channel":"Pink Floyd","duration":413}` + "\n",
			wantTarget:  "https://www.youtube.com/@PinkFloyd/videos",
			wantLimit:   "24",
			wantIDs:     []string{"pf111111"},
		},
		{
			name:        "@handle with trailing /videos avoids duplicate suffix",
			channelID:   "@PinkFloyd/videos",
			channelName: "",
			limit:       10,
			jsonOutput:  `{"id":"pf222222","title":"Money","channel":"Pink Floyd","duration":382}` + "\n",
			wantTarget:  "https://www.youtube.com/@PinkFloyd/videos",
			wantLimit:   "10",
			wantIDs:     []string{"pf222222"},
		},
		{
			name:        "youtube.com URL without scheme and trailing slash is normalized",
			channelID:   "youtube.com/@SodaStereo/",
			channelName: "",
			limit:       8,
			jsonOutput:  `{"id":"ss111111","title":"De Música Ligera","channel":"Soda Stereo","duration":210}` + "\n",
			wantTarget:  "https://www.youtube.com/@SodaStereo/videos",
			wantLimit:   "8",
			wantIDs:     []string{"ss111111"},
		},
		{
			name:        "youtube.com featured URL normalizes to videos",
			channelID:   "http://m.youtube.com/c/RockChannel/featured?sub_confirmation=1",
			channelName: "",
			limit:       5,
			jsonOutput:  `{"id":"rk111111","title":"Track 1","channel":"RockChannel","duration":200}` + "\n",
			wantTarget:  "https://www.youtube.com/c/RockChannel/videos",
			wantLimit:   "5",
			wantIDs:     []string{"rk111111"},
		},
		{
			name:        "youtube.com RSS feed URL extracts channel ID",
			channelID:   "https://www.youtube.com/feeds/videos.xml?channel_id=UC-9-kyTW8ZkZNDHQJ6FgpwQ",
			channelName: "",
			limit:       6,
			jsonOutput:  `{"id":"mu111111","title":"Music Mix","channel":"Music","duration":120}` + "\n",
			wantTarget:  "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/videos",
			wantLimit:   "6",
			wantIDs:     []string{"mu111111"},
		},
		{
			name:        "empty channelID falls back to ytsearch with channelName",
			channelID:   "",
			channelName: "Canserbero",
			limit:       15,
			setupCookie: true,
			jsonOutput:  `{"entries":[{"id":"cn111111","title":"Jeremías 17:5","channel":"Canserbero","duration":318},{"id":"cn222222","title":"Es Épico","channel":"Canserbero","duration":360}]}` + "\n",
			wantTarget:  "ytsearch15:Canserbero",
			wantLimit:   "15",
			wantCookies: true,
			wantIDs:     []string{"cn111111", "cn222222"},
		},
		{
			name:        "non-channel watch URL falls back to ytsearch with default limit",
			channelID:   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			channelName: "Rick Astley",
			limit:       -3,
			jsonOutput:  `{"id":"ra111111","title":"Never Gonna Give You Up","channel":"Rick Astley","duration":213}` + "\n",
			wantTarget:  "ytsearch24:Rick Astley",
			wantLimit:   "24",
			wantIDs:     []string{"ra111111"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			argsFile := filepath.Join(tmp, "args.txt")
			outFile := filepath.Join(tmp, "out.json")
			if err := os.WriteFile(outFile, []byte(tc.jsonOutput), 0o644); err != nil {
				t.Fatalf("write json fixture: %v", err)
			}

			script := fmt.Sprintf(
				"args_file=%s\n: > \"$args_file\"\nfor a in \"$@\"; do\n\tprintf '%%s\\0' \"$a\" >> \"$args_file\"\ndone\ncat %s\n",
				shellQuote(argsFile),
				shellQuote(outFile),
			)
			bin := writeFakeYTDLP(t, tmp, script)

			cookiesPath := ""
			if tc.setupCookie {
				cookiesPath = filepath.Join(tmp, "cookies.txt")
				if err := os.WriteFile(cookiesPath, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\tabc\n"), 0o600); err != nil {
					t.Fatalf("write cookies: %v", err)
				}
			}

			videos, err := FetchChannelVideos(context.Background(), bin, cookiesPath, tc.channelID, tc.channelName, tc.limit)
			if err != nil {
				t.Fatalf("FetchChannelVideos error: %v", err)
			}

			gotIDs := make([]string, len(videos))
			for i, v := range videos {
				gotIDs[i] = v.ID
			}
			if !slices.Equal(gotIDs, tc.wantIDs) {
				t.Fatalf("got IDs %v, want %v", gotIDs, tc.wantIDs)
			}

			args := readRecordedArgs(t, argsFile)
			for _, flag := range []string{"--flat-playlist", "--dump-json", "--no-warnings"} {
				if !slices.Contains(args, flag) {
					t.Errorf("args %v missing flag %q", args, flag)
				}
			}
			if !hasArgPair(args, "--playlist-end", tc.wantLimit) {
				t.Errorf("args %v missing --playlist-end %s", args, tc.wantLimit)
			}
			if tc.wantCookies {
				if !hasArgPair(args, "--cookies", cookiesPath) {
					t.Errorf("args %v missing --cookies %q", args, cookiesPath)
				}
			} else if slices.Contains(args, "--cookies") {
				t.Errorf("args %v unexpectedly contains --cookies", args)
			}
			if gotTarget := lastArg(args); gotTarget != tc.wantTarget {
				t.Errorf("last arg = %q, want %q (all args: %v)", gotTarget, tc.wantTarget, args)
			}
		})
	}
}

func TestFetchChannelVideos_Errors(t *testing.T) {
	t.Run("missing both channelID and channelName returns descriptive error", func(t *testing.T) {
		_, err := FetchChannelVideos(context.Background(), "yt-dlp", "", "   ", "  ", 24)
		if err == nil {
			t.Fatal("expected error for empty channelID and channelName, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "channel") {
			t.Errorf("expected descriptive channel error, got %v", err)
		}
	})

	t.Run("canceled context fails immediately", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := FetchChannelVideos(ctx, "yt-dlp", "", "UC_x5XG1OV2P6uZZ5FSM9Ttw", "", 10)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("yt-dlp failure returns classified YTDLPError", func(t *testing.T) {
		tmp := t.TempDir()
		bin := writeFakeYTDLP(t, tmp, `echo "ERROR: HTTP Error 403: Forbidden" >&2; exit 1`)

		_, err := FetchChannelVideos(context.Background(), bin, "", "@Example", "", 10)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !IsErrKind(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("caller deadline is respected", func(t *testing.T) {
		tmp := t.TempDir()
		bin := writeFakeYTDLP(t, tmp, `sleep 2`)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
		defer cancel()

		_, err := FetchChannelVideos(ctx, bin, "", "@SlowChannel", "", 5)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
	})
}

func TestCatalogChannelVideosWrappers(t *testing.T) {
	t.Run("YTDLPCatalog ChannelVideos delegates bin and cookies", func(t *testing.T) {
		tmp := t.TempDir()
		argsFile := filepath.Join(tmp, "args.txt")
		script := fmt.Sprintf(
			"args_file=%s\n: > \"$args_file\"\nfor a in \"$@\"; do\n\tprintf '%%s\\0' \"$a\" >> \"$args_file\"\ndone\nprintf '{\"id\":\"wrap1111\",\"title\":\"Wrapped\",\"channel\":\"Pink Floyd\",\"duration\":100}\\n'\n",
			shellQuote(argsFile),
		)
		bin := writeFakeYTDLP(t, tmp, script)
		jar := filepath.Join(tmp, "cookies.txt")
		if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\t1\n"), 0o600); err != nil {
			t.Fatalf("write jar: %v", err)
		}

		cat := NewYTDLPCatalog(tmp, time.Minute, bin)
		cat.CookieJarPath = jar
		defer cat.Wait()

		videos, err := cat.ChannelVideos(context.Background(), model.Video{Channel: "Pink Floyd"}, 10)
		if err != nil {
			t.Fatalf("YTDLPCatalog.ChannelVideos error: %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "wrap1111" {
			t.Fatalf("unexpected videos: %+v", videos)
		}

		args := readRecordedArgs(t, argsFile)
		if !hasArgPair(args, "--cookies", jar) {
			t.Errorf("expected --cookies %q in %v", jar, args)
		}
		if lastArg(args) != "ytsearch10:Pink Floyd" {
			t.Errorf("last arg = %q, want %q", lastArg(args), "ytsearch10:Pink Floyd")
		}
	})

	t.Run("YTDLPCatalog ChannelVideos infers handle and canonical UC ID", func(t *testing.T) {
		tmp := t.TempDir()
		argsFile := filepath.Join(tmp, "args.txt")
		script := fmt.Sprintf(
			"args_file=%s\n: > \"$args_file\"\nfor a in \"$@\"; do\n\tprintf '%%s\\0' \"$a\" >> \"$args_file\"\ndone\nprintf '{\"id\":\"inf11111\",\"title\":\"Inferred\",\"channel\":\"Chan\",\"duration\":90}\\n'\n",
			shellQuote(argsFile),
		)
		bin := writeFakeYTDLP(t, tmp, script)
		cat := NewYTDLPCatalog(tmp, time.Minute, bin)
		defer cat.Wait()

		if _, err := cat.ChannelVideos(context.Background(), model.Video{Channel: "@LofiGirl"}, 5); err != nil {
			t.Fatalf("handle ChannelVideos error: %v", err)
		}
		if got := lastArg(readRecordedArgs(t, argsFile)); got != "https://www.youtube.com/@LofiGirl/videos" {
			t.Errorf("handle target = %q, want https://www.youtube.com/@LofiGirl/videos", got)
		}

		if _, err := cat.ChannelVideos(context.Background(), model.Video{Channel: "UC_x5XG1OV2P6uZZ5FSM9Ttw"}, 5); err != nil {
			t.Fatalf("UC ID ChannelVideos error: %v", err)
		}
		if got := lastArg(readRecordedArgs(t, argsFile)); got != "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/videos" {
			t.Errorf("UC ID target = %q, want https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/videos", got)
		}
	})

	t.Run("HybridCatalog ChannelVideos uses Direct when present", func(t *testing.T) {
		tmp := t.TempDir()
		bin := writeFakeYTDLP(t, tmp, `printf '{"id":"hyb11111","title":"Hybrid Direct","channel":"Ch","duration":60}\n'`)
		h := NewHybridCatalog(nil, NewYTDLPCatalog(tmp, time.Minute, bin))

		videos, err := h.ChannelVideos(context.Background(), model.Video{Channel: "Ch"}, 7)
		if err != nil {
			t.Fatalf("HybridCatalog.ChannelVideos error: %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "hyb11111" {
			t.Fatalf("unexpected videos: %+v", videos)
		}
	})

	t.Run("HybridCatalog ChannelVideos falls back to default yt-dlp when Direct is nil", func(t *testing.T) {
		tmp := t.TempDir()
		writeFakeYTDLP(t, tmp, `printf '{"id":"path1111","title":"PATH Fallback","channel":"Soda Stereo","duration":240}\n'`)
		t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

		h := NewHybridCatalog(nil, nil)
		videos, err := h.ChannelVideos(context.Background(), model.Video{Channel: "Soda Stereo"}, 6)
		if err != nil {
			t.Fatalf("HybridCatalog.ChannelVideos fallback error: %v", err)
		}
		if len(videos) != 1 || videos[0].ID != "path1111" {
			t.Fatalf("unexpected videos: %+v", videos)
		}
	})
}
