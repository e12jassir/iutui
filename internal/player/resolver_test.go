package player

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
	"github.com/e12jassir/iutui/internal/platform"
	"strconv"
)

func writeFakeYTDLP(t *testing.T, body string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "fake-yt-dlp.sh")
	content := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp script: %v", err)
	}
	return scriptPath
}

func TestNewStreamResolver_Defaults(t *testing.T) {
	r := NewStreamResolver("", 1080)
	if r.ytdlpBin != "yt-dlp" {
		t.Errorf("ytdlpBin = %q, want %q", r.ytdlpBin, "yt-dlp")
	}
	if r.quality != 1080 {
		t.Errorf("quality = %d, want %d", r.quality, 1080)
	}
	if cap(r.sem) != 4 {
		t.Errorf("sem capacity = %d, want 4", cap(r.sem))
	}
}

func TestStreamResolver_GetCached(t *testing.T) {
	t.Run("nil resolver returns false", func(t *testing.T) {
		var r *StreamResolver
		if _, ok := r.GetCached("vid1"); ok {
			t.Fatal("expected false for nil resolver")
		}
	})

	t.Run("empty videoID returns false", func(t *testing.T) {
		r := NewStreamResolver("yt-dlp", 720)
		if _, ok := r.GetCached(""); ok {
			t.Fatal("expected false for empty videoID")
		}
	})

	t.Run("cache miss for unknown ID", func(t *testing.T) {
		r := NewStreamResolver("yt-dlp", 720)
		if _, ok := r.GetCached("unknown"); ok {
			t.Fatal("expected cache miss for unknown ID")
		}
	})

	t.Run("cache hit before expiry", func(t *testing.T) {
		r := NewStreamResolver("yt-dlp", 720)
		want := ResolvedStream{
			VideoURL:  "https://cdn.example.com/video.mp4",
			AudioURL:  "https://cdn.example.com/audio.m4a",
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}
		r.mu.Lock()
		r.cache["vid1"] = want
		r.mu.Unlock()

		got, ok := r.GetCached("vid1")
		if !ok {
			t.Fatal("expected cache hit before expiry")
		}
		if got.VideoURL != want.VideoURL || got.AudioURL != want.AudioURL {
			t.Errorf("GetCached() = %+v, want %+v", got, want)
		}
	})

	t.Run("cache miss after expiry", func(t *testing.T) {
		r := NewStreamResolver("yt-dlp", 720)
		r.mu.Lock()
		r.cache["vid-expired"] = ResolvedStream{
			VideoURL:  "https://cdn.example.com/old.mp4",
			ExpiresAt: time.Now().Add(-1 * time.Second),
		}
		r.mu.Unlock()

		if _, ok := r.GetCached("vid-expired"); ok {
			t.Fatal("expected cache miss for expired entry")
		}
	})
}

func TestStreamResolver_SetQuality(t *testing.T) {
	t.Run("nil resolver is safe no-op", func(t *testing.T) {
		var r *StreamResolver
		r.SetQuality(1080)
	})

	t.Run("updates quality and clears cached entries", func(t *testing.T) {
		r := NewStreamResolver("yt-dlp", 720)
		r.mu.Lock()
		r.cache["vid1"] = ResolvedStream{
			VideoURL:  "https://cdn.example.com/v1.mp4",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		}
		r.mu.Unlock()

		r.SetQuality(1080)

		r.mu.RLock()
		gotQuality := r.quality
		r.mu.RUnlock()
		if gotQuality != 1080 {
			t.Errorf("quality = %d, want 1080", gotQuality)
		}
		if _, ok := r.GetCached("vid1"); ok {
			t.Fatal("expected cache to be cleared after SetQuality")
		}
	})
}

func TestStreamResolver_ResolveAndPrefetch(t *testing.T) {
	t.Run("resolves video and audio URLs with quality format", func(t *testing.T) {
		argsFile := filepath.Join(t.TempDir(), "args.txt")
		bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf '%%s\n' "$@" > %q
printf 'warning: ignored line\nhttps://cdn.example.com/v720.mp4\nhttps://cdn.example.com/a720.m4a\n'
`, argsFile))

		r := NewStreamResolver(bin, 720)
		r.Prefetch(context.Background(), model.Video{ID: "abc123"})

		deadline := time.Now().Add(2 * time.Second)
		var got ResolvedStream
		var ok bool
		for time.Now().Before(deadline) {
			got, ok = r.GetCached("abc123")
			if ok {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !ok {
			t.Fatal("timed out waiting for Prefetch to populate cache")
		}
		if got.VideoURL != "https://cdn.example.com/v720.mp4" {
			t.Errorf("VideoURL = %q, want %q", got.VideoURL, "https://cdn.example.com/v720.mp4")
		}
		if got.AudioURL != "https://cdn.example.com/a720.m4a" {
			t.Errorf("AudioURL = %q, want %q", got.AudioURL, "https://cdn.example.com/a720.m4a")
		}

		rawArgs, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("read recorded args: %v", err)
		}
		argsStr := string(rawArgs)
		wantFmt := "bestvideo[height<=720]+bestaudio/best[height<=720]/best"
		if !strings.Contains(argsStr, wantFmt) {
			t.Errorf("yt-dlp args %q missing format %q", argsStr, wantFmt)
		}
		if !strings.Contains(argsStr, "-g") || !strings.Contains(argsStr, "https://www.youtube.com/watch?v=abc123") {
			t.Errorf("yt-dlp args %q missing -g or target URL", argsStr)
		}
	})

	t.Run("resolves single combined stream URL when quality is 0", func(t *testing.T) {
		argsFile := filepath.Join(t.TempDir(), "args.txt")
		bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf '%%s\n' "$@" > %q
printf 'http://cdn.example.com/combined.mp4\n'
`, argsFile))

		r := NewStreamResolver(bin, 0)
		st, ok := r.resolve(context.Background(), "single1")
		if !ok {
			t.Fatal("resolve() returned false, want true")
		}
		if st.VideoURL != "http://cdn.example.com/combined.mp4" || st.AudioURL != "" {
			t.Errorf("resolve() = %+v, want single VideoURL and empty AudioURL", st)
		}
		rawArgs, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("read args: %v", err)
		}
		if !strings.Contains(string(rawArgs), "bestvideo+bestaudio/best") {
			t.Errorf("args %q missing default best format", string(rawArgs))
		}
	})

	t.Run("resolve returns false on command failure or non-http output", func(t *testing.T) {
		failBin := writeFakeYTDLP(t, "exit 1")
		rFail := NewStreamResolver(failBin, 720)
		if _, ok := rFail.resolve(context.Background(), "vid1"); ok {
			t.Fatal("expected resolve() to fail when yt-dlp exits non-zero")
		}

		emptyBin := writeFakeYTDLP(t, "printf 'not-a-url\\n'")
		rEmpty := NewStreamResolver(emptyBin, 720)
		if _, ok := rEmpty.resolve(context.Background(), "vid1"); ok {
			t.Fatal("expected resolve() to fail when output has no http(s) URLs")
		}
	})

	t.Run("ignores nil resolver, empty ID, live videos, and already cached entries", func(t *testing.T) {
		var nilResolver *StreamResolver
		nilResolver.Prefetch(context.Background(), model.Video{ID: "abc"})

		markerFile := filepath.Join(t.TempDir(), "called.txt")
		bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf 'called\n' >> %q
printf 'https://cdn.example.com/v.mp4\n'
`, markerFile))

		r := NewStreamResolver(bin, 720)
		r.mu.Lock()
		r.cache["cached-id"] = ResolvedStream{
			VideoURL:  "https://cdn.example.com/cached.mp4",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		}
		r.mu.Unlock()

		r.Prefetch(context.Background(), model.Video{ID: ""})
		r.Prefetch(context.Background(), model.Video{ID: "live-id", IsLive: true})
		r.Prefetch(context.Background(), model.Video{ID: "cached-id"})

		time.Sleep(50 * time.Millisecond)
		if _, err := os.Stat(markerFile); !os.IsNotExist(err) {
			t.Fatalf("expected yt-dlp not to be invoked for empty/live/cached videos, stat err=%v", err)
		}
	})
}

func TestStreamResolver_PrefetchDeduplicationAndSemaphore(t *testing.T) {
	t.Run("deduplicates concurrent Prefetch calls for same videoID", func(t *testing.T) {
		tmpDir := t.TempDir()
		countFile := filepath.Join(tmpDir, "invocations.log")
		gateFile := filepath.Join(tmpDir, "release.gate")

		bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf '1\n' >> %q
while [ ! -f %q ]; do
  sleep 0.01
done
printf 'https://cdn.example.com/dedup.mp4\n'
`, countFile, gateFile))

		r := NewStreamResolver(bin, 720)
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.Prefetch(context.Background(), model.Video{ID: "same-vid"})
			}()
		}
		wg.Wait()

		// Wait until the single in-flight invocation records its start.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(countFile); err == nil && len(b) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Release the gate.
		if err := os.WriteFile(gateFile, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write gate file: %v", err)
		}

		// Wait for cache population.
		deadline = time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, ok := r.GetCached("same-vid"); ok {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if _, ok := r.GetCached("same-vid"); !ok {
			t.Fatal("expected same-vid to be cached after deduplicated Prefetch")
		}

		b, err := os.ReadFile(countFile)
		if err != nil {
			t.Fatalf("read invocation log: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) != 1 {
			t.Errorf("expected exactly 1 yt-dlp invocation for concurrent duplicate Prefetch, got %d", len(lines))
		}
	})

	t.Run("respects semaphore limit of 4 and cancels waiting goroutine on ctx Done", func(t *testing.T) {
		tmpDir := t.TempDir()
		startedDir := filepath.Join(tmpDir, "started")
		if err := os.MkdirAll(startedDir, 0o755); err != nil {
			t.Fatalf("mkdir started: %v", err)
		}
		gateFile := filepath.Join(tmpDir, "sem.gate")

		bin := writeFakeYTDLP(t, fmt.Sprintf(`
for last; do :; done
id=${last#*=}
printf '1' > "%s/$id"
while [ ! -f %q ]; do
  sleep 0.01
done
printf 'https://cdn.example.com/%%s.mp4\n' "$id"
`, startedDir, gateFile))

		r := NewStreamResolver(bin, 720)
		ctx := context.Background()
		for _, id := range []string{"v1", "v2", "v3", "v4", "v5"} {
			r.Prefetch(ctx, model.Video{ID: id})
		}

		// Wait until 4 invocations have started.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			entries, _ := os.ReadDir(startedDir)
			if len(entries) >= 4 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Give a brief window to confirm a 5th process does not start while the gate is closed.
		time.Sleep(50 * time.Millisecond)
		entries, err := os.ReadDir(startedDir)
		if err != nil {
			t.Fatalf("readdir started: %v", err)
		}
		if len(entries) != 4 {
			t.Fatalf("active yt-dlp processes = %d, want 4 (semaphore bound)", len(entries))
		}

		// Also verify a 6th Prefetch with an already-canceled context aborts while waiting for semaphore.
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		r.Prefetch(canceledCtx, model.Video{ID: "v6-canceled"})

		// Release the gate and wait for v1..v5 to complete.
		if err := os.WriteFile(gateFile, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write gate file: %v", err)
		}

		deadline = time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			_, ok1 := r.GetCached("v1")
			_, ok2 := r.GetCached("v2")
			_, ok3 := r.GetCached("v3")
			_, ok4 := r.GetCached("v4")
			_, ok5 := r.GetCached("v5")
			if ok1 && ok2 && ok3 && ok4 && ok5 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		for _, id := range []string{"v1", "v2", "v3", "v4", "v5"} {
			if _, ok := r.GetCached(id); !ok {
				t.Errorf("expected %s to be cached after semaphore release", id)
			}
		}
		if _, ok := r.GetCached("v6-canceled"); ok {
			t.Errorf("expected v6-canceled not to be cached when context was canceled")
		}
	})
}

func TestStreamResolver_CookiesAndYTDLPBin(t *testing.T) {
	t.Run("nil receiver is safe no-op for SetCookieJar and SetYTDLPBin", func(t *testing.T) {
		var r *StreamResolver
		r.SetCookieJar("/tmp/cookies.txt")
		r.SetYTDLPBin("/usr/bin/yt-dlp")
	})

	t.Run("SetYTDLPBin ignores empty string and switches binary", func(t *testing.T) {
		usedFile := filepath.Join(t.TempDir(), "used.txt")
		initialBin := writeFakeYTDLP(t, "exit 1")
		updatedBin := writeFakeYTDLP(t, fmt.Sprintf("printf 'updated\\n' > %q\nprintf 'https://cdn.example.com/ok.mp4\\n'", usedFile))

		r := NewStreamResolver(initialBin, 720)
		r.SetYTDLPBin("")
		if _, ok := r.resolve(context.Background(), "vid1"); ok {
			t.Fatal("expected empty SetYTDLPBin to keep initial binary")
		}

		r.SetYTDLPBin(updatedBin)
		st, ok := r.resolve(context.Background(), "vid1")
		if !ok || st.VideoURL != "https://cdn.example.com/ok.mp4" {
			t.Fatalf("expected updated binary to resolve stream, got st=%+v ok=%v", st, ok)
		}
	})

	t.Run("cookieless android succeeds without --cookies even when valid cookie jar is set", func(t *testing.T) {
		tmpDir := t.TempDir()
		callsFile := filepath.Join(tmpDir, "calls.log")
		bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf -- '---CALL---\n' >> %q
printf '%%s\n' "$@" >> %q
printf 'https://cdn.example.com/android-fast.mp4\n'
`, callsFile, callsFile))

		cookieFile := filepath.Join(tmpDir, "cookies.txt")
		if err := os.WriteFile(cookieFile, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
			t.Fatalf("write cookie jar: %v", err)
		}

		r := NewStreamResolver(bin, 720)
		r.SetCookieJar(cookieFile)

		st, ok := r.resolve(context.Background(), "vid-android-ok")
		if !ok || st.VideoURL != "https://cdn.example.com/android-fast.mp4" {
			t.Fatalf("resolve failed: ok=%v st=%+v", ok, st)
		}

		raw, err := os.ReadFile(callsFile)
		if err != nil {
			t.Fatalf("read calls: %v", err)
		}
		got := string(raw)
		if gotCalls := strings.Count(got, "---CALL---"); gotCalls != 1 {
			t.Fatalf("yt-dlp invocations = %d, want 1:\n%s", gotCalls, got)
		}
		if strings.Contains(got, "--cookies") {
			t.Errorf("did not expect --cookies on fast android success:\n%s", got)
		}
		if strings.Contains(got, "player_client=android") {
			t.Errorf("should not force player_client=android:\n%s", got)
		}
		if strings.Contains(got, "player_client=android,web") {
			t.Errorf("did not expect web client on fast android primary path:\n%s", got)
		}
	})

	t.Run("retries with --cookies when cookieless android fails or returns zero URLs", func(t *testing.T) {
		tmpDir := t.TempDir()
		cookieFile := filepath.Join(tmpDir, "cookies.txt")
		if err := os.WriteFile(cookieFile, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
			t.Fatalf("write cookie jar: %v", err)
		}

		cases := []struct {
			name         string
			attempt1Body string
		}{
			{
				name:         "cookieless exits non-zero (age/member restricted)",
				attempt1Body: "exit 1",
			},
			{
				name:         "cookieless exits zero with no HTTP URLs",
				attempt1Body: "printf 'warning only\\n'",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				callsFile := filepath.Join(t.TempDir(), "calls.log")
				bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf -- '---CALL---\n' >> %q
printf '%%s\n' "$@" >> %q
for a in "$@"; do
  if [ "$a" = "--cookies" ]; then
    printf 'https://cdn.example.com/cookie-fallback.mp4\n'
    exit 0
  fi
done
%s
`, callsFile, callsFile, tc.attempt1Body))

				r := NewStreamResolver(bin, 720)
				r.SetCookieJar(cookieFile)

				st, ok := r.resolve(context.Background(), "vid-restricted")
				if !ok || st.VideoURL != "https://cdn.example.com/cookie-fallback.mp4" {
					t.Fatalf("resolve fallback failed: ok=%v st=%+v", ok, st)
				}

				raw, err := os.ReadFile(callsFile)
				if err != nil {
					t.Fatalf("read calls: %v", err)
				}
				calls := strings.Split(strings.TrimPrefix(string(raw), "---CALL---\n"), "---CALL---\n")
				if len(calls) != 2 {
					t.Fatalf("yt-dlp invocations = %d, want 2:\n%s", len(calls), string(raw))
				}
				if strings.Contains(calls[0], "--cookies") || strings.Contains(calls[0], "player_client=android") {
					t.Errorf("attempt 1 should be cookieless without player_client=android:\n%s", calls[0])
				}
				if !strings.Contains(calls[1], "--cookies\n"+cookieFile+"\n") || strings.Contains(calls[1], "player_client=android") {
					t.Errorf("attempt 2 should use --cookies without player_client=android:\n%s", calls[1])
				}
			})
		}
	})

	t.Run("does not retry with --cookies for empty, directory, or missing cookie jar", func(t *testing.T) {
		tmpDir := t.TempDir()
		emptyCookie := filepath.Join(tmpDir, "empty.txt")
		if err := os.WriteFile(emptyCookie, nil, 0o600); err != nil {
			t.Fatalf("write empty cookie jar: %v", err)
		}
		missingCookie := filepath.Join(tmpDir, "missing.txt")

		for _, tc := range []struct {
			name string
			path string
		}{
			{name: "empty path", path: ""},
			{name: "empty file", path: emptyCookie},
			{name: "directory path", path: tmpDir},
			{name: "missing path", path: missingCookie},
		} {
			t.Run(tc.name, func(t *testing.T) {
				callsFile := filepath.Join(t.TempDir(), "calls.log")
				bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf -- '---CALL---\n' >> %q
printf '%%s\n' "$@" >> %q
exit 1
`, callsFile, callsFile))

				r := NewStreamResolver(bin, 720)
				r.SetCookieJar(tc.path)

				if _, ok := r.resolve(context.Background(), "vid-fail"); ok {
					t.Fatal("expected resolve to fail when cookieless fails and cookie jar is invalid")
				}

				raw, err := os.ReadFile(callsFile)
				if err != nil {
					t.Fatalf("read calls: %v", err)
				}
				got := string(raw)
				if gotCalls := strings.Count(got, "---CALL---"); gotCalls != 1 {
					t.Fatalf("yt-dlp invocations = %d, want 1:\n%s", gotCalls, got)
				}
				if strings.Contains(got, "--cookies") {
					t.Errorf("did not expect --cookies retry:\n%s", got)
				}
				if strings.Contains(got, "player_client=android") {
					t.Errorf("should not force player_client=android on single attempt:\n%s", got)
				}
			})
		}
	})
}

func TestStreamResolver_MixPrefetchAndWaitOrGet(t *testing.T) {
	t.Run("Prefetch normalizes RD<11char> seed and skips non-seeded playlists", func(t *testing.T) {
		tmpDir := t.TempDir()
		callsFile := filepath.Join(tmpDir, "calls.txt")
		bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf '%%s\n' "$*" >> %q
printf 'https://cdn.example.com/seed.mp4\n'
`, callsFile))

		r := NewStreamResolver(bin, 720)
		// Non-seeded playlists should not invoke yt-dlp.
		r.Prefetch(context.Background(), model.Video{ID: "PL1234567890"})
		r.Prefetch(context.Background(), model.Video{ID: "RDCLAK5uy_abc123"})
		time.Sleep(40 * time.Millisecond)
		if _, err := os.Stat(callsFile); !os.IsNotExist(err) {
			t.Fatalf("expected playlists not to trigger Prefetch, stat err=%v", err)
		}

		// RD + 11-char video ID normalizes to the 11-char seed video ID.
		r.Prefetch(context.Background(), model.Video{ID: "RDdQw4w9WgXcQ"})
		st, ok := r.WaitOrGet(context.Background(), "RDdQw4w9WgXcQ", 2*time.Second)
		if !ok {
			t.Fatal("WaitOrGet(RDdQw4w9WgXcQ) returned false, want true")
		}
		if st.VideoURL != "https://cdn.example.com/seed.mp4" {
			t.Errorf("VideoURL = %q, want %q", st.VideoURL, "https://cdn.example.com/seed.mp4")
		}
		if _, ok := r.GetCached("dQw4w9WgXcQ"); !ok {
			t.Fatal("expected normalized seed ID dQw4w9WgXcQ to be cached")
		}
	})

	t.Run("Prefetch normalizes RD<11char> Mix card even when IsLive is true", func(t *testing.T) {
		tmpDir := t.TempDir()
		bin := writeFakeYTDLP(t, `printf 'https://cdn.example.com/live-mix-seed.mp4\n'`)
		r := NewStreamResolver(bin, 720)
		r.Prefetch(context.Background(), model.Video{ID: "RDliveMix1234", IsLive: true})
		st, ok := r.WaitOrGet(context.Background(), "RDliveMix1234", 2*time.Second)
		if !ok || st.VideoURL != "https://cdn.example.com/live-mix-seed.mp4" {
			t.Fatalf("expected Mix card with IsLive=true to prefetch seed stream, got ok=%v st=%+v", ok, st)
		}
		_ = tmpDir
	})

	t.Run("WaitOrGet coalesces in-flight lookup, times out, and respects ctx cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		startedFile := filepath.Join(tmpDir, "started")
		gateFile := filepath.Join(tmpDir, "gate")
		bin := writeFakeYTDLP(t, fmt.Sprintf(`
touch %q
while [ ! -f %q ]; do sleep 0.01; done
printf 'https://cdn.example.com/coalesced.mp4\n'
`, startedFile, gateFile))

		r := NewStreamResolver(bin, 720)
		r.Prefetch(context.Background(), model.Video{ID: "wait-vid"})

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(startedFile); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}

		// Short maxWait times out while gate is closed.
		if _, ok := r.WaitOrGet(context.Background(), "wait-vid", 25*time.Millisecond); ok {
			t.Fatal("expected WaitOrGet to time out while lookup is gated")
		}

		// Canceled context returns false promptly.
		cctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		if _, ok := r.WaitOrGet(cctx, "wait-vid", 2*time.Second); ok {
			t.Fatal("expected WaitOrGet to return false when ctx is canceled")
		}

		// Concurrent WaitOrGet succeeds once gate is released.
		resCh := make(chan bool, 1)
		go func() {
			st, ok := r.WaitOrGet(context.Background(), "wait-vid", 2*time.Second)
			resCh <- (ok && st.VideoURL == "https://cdn.example.com/coalesced.mp4")
		}()

		time.Sleep(20 * time.Millisecond)
		if err := os.WriteFile(gateFile, []byte("go"), 0o644); err != nil {
			t.Fatalf("write gate: %v", err)
		}

		if !<-resCh {
			t.Fatal("expected WaitOrGet to return resolved stream once in-flight finished")
		}
	})

	t.Run("WaitOrGet unblocks early when SetQuality invalidates generation", func(t *testing.T) {
		tmpDir := t.TempDir()
		startedFile := filepath.Join(tmpDir, "started")
		gateFile := filepath.Join(tmpDir, "gate")
		bin := writeFakeYTDLP(t, fmt.Sprintf(`
touch %q
while [ ! -f %q ]; do sleep 0.01; done
printf 'https://cdn.example.com/stale.mp4\n'
`, startedFile, gateFile))

		r := NewStreamResolver(bin, 720)
		r.Prefetch(context.Background(), model.Video{ID: "gen-vid"})

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(startedFile); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}

		waitDone := make(chan bool, 1)
		go func() {
			_, ok := r.WaitOrGet(context.Background(), "gen-vid", 3*time.Second)
			waitDone <- ok
		}()

		time.Sleep(20 * time.Millisecond)
		r.SetQuality(1080)
		if err := os.WriteFile(gateFile, []byte("go"), 0o644); err != nil {
			t.Fatalf("write gate: %v", err)
		}

		select {
		case ok := <-waitDone:
			if ok {
				t.Fatal("expected WaitOrGet to return false for invalidated generation")
			}
		case <-time.After(1 * time.Second):
			t.Fatal("WaitOrGet stayed blocked instead of unblocking when old goroutine exited")
		}
	})
}

func TestStreamResolver_InnerTubeFastPathAndFallback(t *testing.T) {
	t.Run("resolves audio and HD video via InnerTube without spawning yt-dlp", func(t *testing.T) {
		markerFile := filepath.Join(t.TempDir(), "ytdlp-called.txt")
		bin := writeFakeYTDLP(t, fmt.Sprintf("touch %q\nexit 1\n", markerFile))

		var gotClients []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Context struct {
					Client struct {
						ClientName string `json:"clientName"`
					} `json:"client"`
				} `json:"context"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			gotClients = append(gotClients, req.Context.Client.ClientName)
			_, _ = w.Write([]byte(`{
				"playabilityStatus": {"status": "OK"},
				"streamingData": {
					"adaptiveFormats": [
						{"url": "https://cdn.example.com/audio-128.m4a", "mimeType": "audio/mp4; codecs=\"mp4a.40.2\"", "bitrate": 130000},
						{"url": "https://cdn.example.com/video-1080.mp4", "mimeType": "video/mp4; codecs=\"avc1\"", "height": 1080, "bitrate": 2500000},
						{"url": "https://cdn.example.com/video-720.mp4", "mimeType": "video/mp4; codecs=\"avc1\"", "height": 720, "bitrate": 1200000}
					]
				}
			}`))
		}))
		defer srv.Close()

		r := NewStreamResolver(bin, 720)
		r.SetInnerTubeEndpoint(srv.URL)

		stAudio, ok := r.WaitOrGetForMode(context.Background(), "fastVid0001", true, time.Second)
		if !ok || stAudio.AudioURL != "https://cdn.example.com/audio-128.m4a" || stAudio.VideoURL != "" {
			t.Fatalf("unexpected audio-only InnerTube result: ok=%v st=%+v", ok, stAudio)
		}

		stVideo, ok := r.WaitOrGetForMode(context.Background(), "fastVid0002", false, time.Second)
		if !ok || stVideo.VideoURL != "https://cdn.example.com/video-720.mp4" || stVideo.AudioURL != "https://cdn.example.com/audio-128.m4a" {
			t.Fatalf("unexpected 720p video InnerTube result: ok=%v st=%+v", ok, stVideo)
		}
		if _, err := os.Stat(markerFile); !os.IsNotExist(err) {
			t.Fatalf("expected InnerTube fast path to skip yt-dlp binary completely")
		}
		if len(gotClients) == 0 || gotClients[0] != "ANDROID_VR" {
			t.Fatalf("expected ANDROID_VR primary InnerTube client, got %v", gotClients)
		}
	})

	t.Run("falls back cleanly to yt-dlp when InnerTube returns unplayable status", func(t *testing.T) {
		bin := writeFakeYTDLP(t, "printf 'https://cdn.example.com/fallback-ytdlp.m4a\n'\n")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"playabilityStatus": {"status": "LOGIN_REQUIRED"}}`))
		}))
		defer srv.Close()

		r := NewStreamResolver(bin, 720)
		r.SetInnerTubeEndpoint(srv.URL)

		st, ok := r.WaitOrGetForMode(context.Background(), "lockedVid01", true, time.Second)
		if !ok || st.AudioURL != "https://cdn.example.com/fallback-ytdlp.m4a" {
			t.Fatalf("expected yt-dlp fallback stream URL, got ok=%v st=%+v", ok, st)
		}
	})
}

func TestStreamResolver_DisablesUnverifiedInnerTubeByDefaultAndInvalidatesCache(t *testing.T) {
	t.Run("NewStreamResolver disables unverified InnerTube fast path by default", func(t *testing.T) {
		for _, bin := range []string{"", "yt-dlp"} {
			r := NewStreamResolver(bin, 720)
			if r.enableInnerTube {
				t.Fatalf("NewStreamResolver(%q, 720) enabled unverified InnerTube by default", bin)
			}
		}
	})

	t.Run("Invalidate evicts cached video and audio entries", func(t *testing.T) {
		r := NewStreamResolver("yt-dlp", 720)
		r.mu.Lock()
		r.cache["badVid00001"] = ResolvedStream{
			VideoURL:  "https://cdn.example.com/403.mp4",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		}
		r.audioCache["badVid00001"] = ResolvedStream{
			AudioURL:  "https://cdn.example.com/403.webm",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		}
		r.mu.Unlock()

		r.Invalidate("badVid00001")

		if _, ok := r.GetCachedForMode("badVid00001", false); ok {
			t.Fatal("expected video cache entry to be evicted after Invalidate")
		}
		if _, ok := r.GetCachedForMode("badVid00001", true); ok {
			t.Fatal("expected audio cache entry to be evicted after Invalidate")
		}
	})
}

func TestStreamResolver_ProcessGroupCleanupOnTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	childPIDFile := filepath.Join(tmpDir, "deno-child.pid")
	bin := writeFakeYTDLP(t, fmt.Sprintf(`
sleep 5 &
child=$!
printf '%%s\n' "$child" > %q
wait "$child"
`, childPIDFile))

	r := NewStreamResolver(bin, 720)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	if _, ok := r.resolve(ctx, "orphanVid01"); ok {
		t.Fatal("expected resolve to time out")
	}

	rawPID, err := os.ReadFile(childPIDFile)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil || childPID <= 0 {
		t.Fatalf("invalid child pid %q: %v", string(rawPID), err)
	}
	t.Cleanup(func() {
		_ = platform.KillProcessGroup(childPID)
	})

	deadline := time.Now().Add(500 * time.Millisecond)
	for platform.ProcessAlive(childPID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if platform.ProcessAlive(childPID) {
		t.Fatalf("expected child process %d in yt-dlp process group to be killed on ctx timeout", childPID)
	}
}

func TestStreamResolver_ExtractorArgsFastPathAndFallbacks(t *testing.T) {
	t.Run("audio-only and <=480p try android webpage,configs skip first, then configs skip, then cookies", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			audioOnly bool
			quality   int
		}{
			{name: "audio-only", audioOnly: true, quality: 720},
			{name: "480p video", audioOnly: false, quality: 480},
			{name: "360p video", audioOnly: false, quality: 360},
		} {
			t.Run(tc.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				cookieFile := filepath.Join(tmpDir, "cookies.txt")
				if err := os.WriteFile(cookieFile, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
					t.Fatalf("write cookies: %v", err)
				}

				// Subcase 1: Attempt 1 (android fast-path) succeeds immediately.
				calls1 := filepath.Join(tmpDir, "calls1.log")
				bin1 := writeFakeYTDLP(t, fmt.Sprintf(`
printf -- '---CALL---\n' >> %q
printf '%%s\n' "$@" >> %q
printf 'https://cdn.example.com/fast-android.m4a\n'
`, calls1, calls1))
				r1 := NewStreamResolver(bin1, tc.quality)
				r1.SetCookieJar(cookieFile)
				if _, ok := r1.resolveForMode(context.Background(), "fastVid001", tc.audioOnly); !ok {
					t.Fatal("expected attempt 1 to succeed")
				}
				raw1, _ := os.ReadFile(calls1)
				if strings.Count(string(raw1), "---CALL---") != 1 {
					t.Fatalf("expected 1 call, got:\n%s", string(raw1))
				}
				if !strings.Contains(string(raw1), "--extractor-args\nyoutube:player_client=android;player_skip=webpage,configs\n") {
					t.Fatalf("expected android fast-path extractor-args on attempt 1, got:\n%s", string(raw1))
				}

				// Subcase 2: Attempt 1 fails -> Attempt 2 (cookieless player_skip=configs) -> Attempt 3 (--cookies).
				calls3 := filepath.Join(tmpDir, "calls3.log")
				bin3 := writeFakeYTDLP(t, fmt.Sprintf(`
printf -- '---CALL---\n' >> %q
printf '%%s\n' "$@" >> %q
for a in "$@"; do
  if [ "$a" = "--cookies" ]; then
    printf 'https://cdn.example.com/cookie-ok.m4a\n'
    exit 0
  fi
done
exit 1
`, calls3, calls3))
				r3 := NewStreamResolver(bin3, tc.quality)
				r3.SetCookieJar(cookieFile)
				st, ok := r3.resolveForMode(context.Background(), "restricted01", tc.audioOnly)
				if !ok {
					t.Fatalf("expected 3rd attempt with cookies to succeed, got st=%+v", st)
				}
				raw3, _ := os.ReadFile(calls3)
				parts := strings.Split(strings.TrimPrefix(string(raw3), "---CALL---\n"), "---CALL---\n")
				if len(parts) != 3 {
					t.Fatalf("expected 3 attempts (android -> configs -> cookies), got %d:\n%s", len(parts), string(raw3))
				}
				if !strings.Contains(parts[0], "youtube:player_client=android;player_skip=webpage,configs") || strings.Contains(parts[0], "--cookies") {
					t.Errorf("attempt 1 mismatch:\n%s", parts[0])
				}
				if !strings.Contains(parts[1], "--extractor-args\nyoutube:player_skip=configs\n") || strings.Contains(parts[1], "player_client=android") || strings.Contains(parts[1], "--cookies") {
					t.Errorf("attempt 2 mismatch:\n%s", parts[1])
				}
				if !strings.Contains(parts[2], "--cookies\n"+cookieFile+"\n") {
					t.Errorf("attempt 3 mismatch:\n%s", parts[2])
				}
			})
		}
	})

	t.Run("HD/4K video uses player_skip=configs cookieless then cookies", func(t *testing.T) {
		for _, q := range []int{720, 1080, 0} {
			tmpDir := t.TempDir()
			callsFile := filepath.Join(tmpDir, "calls.log")
			bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf '%%s\n' "$@" > %q
printf 'https://cdn.example.com/hd.mp4\nhttps://cdn.example.com/hd.m4a\n'
`, callsFile))
			r := NewStreamResolver(bin, q)
			if _, ok := r.resolveForMode(context.Background(), "hdVid000001", false); !ok {
				t.Fatalf("resolveForMode(q=%d) failed", q)
			}
			raw, _ := os.ReadFile(callsFile)
			if !strings.Contains(string(raw), "--extractor-args\nyoutube:player_skip=configs\n") {
				t.Fatalf("expected youtube:player_skip=configs for q=%d, got:\n%s", q, string(raw))
			}
			if strings.Contains(string(raw), "player_client=android") {
				t.Fatalf("must not use android client for HD q=%d:\n%s", q, string(raw))
			}
		}
	})
}

func TestStreamResolver_SetExtraArgs(t *testing.T) {
	var nilResolver *StreamResolver
	nilResolver.SetExtraArgs([]string{"--geo-bypass"})

	argsFile := filepath.Join(t.TempDir(), "extra-args.txt")
	bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf '%%s\n' "$@" > %q
printf 'https://cdn.example.com/extra.mp4\n'
`, argsFile))

	r := NewStreamResolver(bin, 720)
	extra := []string{"--geo-bypass", "--proxy", "http://127.0.0.1:8080"}
	r.SetExtraArgs(extra)
	extra[0] = "--mutated"

	if _, ok := r.resolve(context.Background(), "extraVid001"); !ok {
		t.Fatal("resolve failed")
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if lines[len(lines)-1] != "https://www.youtube.com/watch?v=extraVid001" {
		t.Fatalf("expected target URL to remain last arg, got %v", lines)
	}
	gotStr := string(raw)
	if !strings.Contains(gotStr, "--geo-bypass\n--proxy\nhttp://127.0.0.1:8080\n") {
		t.Fatalf("expected extra args in yt-dlp invocation, got:\n%s", gotStr)
	}
}

func TestStreamResolver_SetPrefetchLimitAndInteractivePriority(t *testing.T) {
	t.Run("SetPrefetchLimit(0) disables background Prefetch completely", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "called.txt")
		bin := writeFakeYTDLP(t, fmt.Sprintf("touch %q\nprintf 'https://cdn.example.com/v.mp4\\n'\n", marker))

		r := NewStreamResolver(bin, 720)
		r.SetPrefetchLimit(0)
		r.Prefetch(context.Background(), model.Video{ID: "disabledPf1"})
		time.Sleep(60 * time.Millisecond)

		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("expected SetPrefetchLimit(0) not to spawn any background yt-dlp process")
		}
	})

	t.Run("SetPrefetchLimit bounds concurrency and clamps at 8", func(t *testing.T) {
		tmpDir := t.TempDir()
		startedDir := filepath.Join(tmpDir, "started")
		_ = os.MkdirAll(startedDir, 0o755)
		gate := filepath.Join(tmpDir, "gate")

		bin := writeFakeYTDLP(t, fmt.Sprintf(`
for last; do :; done
id=${last#*=}
touch "%s/$id"
while [ ! -f %q ]; do sleep 0.01; done
printf 'https://cdn.example.com/%%s.mp4\n' "$id"
`, startedDir, gate))

		r := NewStreamResolver(bin, 720)
		r.SetPrefetchLimit(20)
		if cap(r.sem) != 8 {
			t.Fatalf("cap(r.sem) = %d after SetPrefetchLimit(20), want 8", cap(r.sem))
		}
		r.SetPrefetchLimit(2)
		for _, id := range []string{"p1", "p2", "p3", "p4"} {
			r.Prefetch(context.Background(), model.Video{ID: id})
		}

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			entries, _ := os.ReadDir(startedDir)
			if len(entries) >= 2 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond)
		entries, _ := os.ReadDir(startedDir)
		if len(entries) != 2 {
			_ = os.WriteFile(gate, []byte("go"), 0o600)
			t.Fatalf("active prefetch processes = %d, want 2", len(entries))
		}
		_ = os.WriteFile(gate, []byte("go"), 0o600)
	})

	t.Run("background Prefetch waits while interactive WaitOrGetForMode is active", func(t *testing.T) {
		tmpDir := t.TempDir()
		playStarted := filepath.Join(tmpDir, "play-started")
		playGate := filepath.Join(tmpDir, "play-gate")
		bgStarted := filepath.Join(tmpDir, "bg-started")

		bin := writeFakeYTDLP(t, fmt.Sprintf(`
for last; do :; done
case "$last" in
  *playVid0001*)
    touch %q
    while [ ! -f %q ]; do sleep 0.01; done
    printf 'https://cdn.example.com/play.m4a\n'
    ;;
  *bgVid000001*)
    touch %q
    printf 'https://cdn.example.com/bg.m4a\n'
    ;;
esac
`, playStarted, playGate, bgStarted))

		r := NewStreamResolver(bin, 720)
		r.SetAudioOnly(true)

		playDone := make(chan ResolvedStream, 1)
		go func() {
			st, _ := r.WaitOrGetForMode(context.Background(), "playVid0001", true, 2*time.Second)
			playDone <- st
		}()

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(playStarted); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}

		// Queue background Prefetch while interactive resolution is active.
		r.Prefetch(context.Background(), model.Video{ID: "bgVid000001"})
		time.Sleep(60 * time.Millisecond)
		if _, err := os.Stat(bgStarted); !os.IsNotExist(err) {
			_ = os.WriteFile(playGate, []byte("go"), 0o600)
			t.Fatal("expected background Prefetch to wait while interactive WaitOrGetForMode is running")
		}

		_ = os.WriteFile(playGate, []byte("go"), 0o600)
		st := <-playDone
		if st.AudioURL != "https://cdn.example.com/play.m4a" {
			t.Fatalf("unexpected interactive stream: %+v", st)
		}

		deadline = time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, ok := r.GetCachedForMode("bgVid000001", true); ok {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("expected background Prefetch to resume and cache once interactive lookup finished")
	})
}

func TestStreamResolver_WaitOrGetVideoModeAndTrueCoalescing(t *testing.T) {
	t.Run("WaitOrGetForMode resolves video (!audioOnly) interactively and coalesces concurrent callers without killing on maxWait", func(t *testing.T) {
		tmpDir := t.TempDir()
		countFile := filepath.Join(tmpDir, "count.log")
		gate := filepath.Join(tmpDir, "gate")

		bin := writeFakeYTDLP(t, fmt.Sprintf(`
printf '1\n' >> %q
while [ ! -f %q ]; do sleep 0.01; done
printf 'https://cdn.example.com/video720.mp4\nhttps://cdn.example.com/audio720.m4a\n'
`, countFile, gate))

		r := NewStreamResolver(bin, 720)

		// Caller 1 has a tiny maxWait (30ms) and times out, but must NOT kill yt-dlp!
		if _, ok := r.WaitOrGetForMode(context.Background(), "coalVid0001", false, 30*time.Millisecond); ok {
			t.Fatal("expected short maxWait caller to time out while gate is closed")
		}

		// Caller 2 & Caller 3 join concurrently with 2s maxWait and must share the same single process.
		var wg sync.WaitGroup
		results := make([]ResolvedStream, 2)
		oks := make([]bool, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				results[idx], oks[idx] = r.WaitOrGetForMode(context.Background(), "coalVid0001", false, 2*time.Second)
			}(i)
		}

		time.Sleep(25 * time.Millisecond)
		_ = os.WriteFile(gate, []byte("go"), 0o600)
		wg.Wait()

		for i := 0; i < 2; i++ {
			if !oks[i] || results[i].VideoURL != "https://cdn.example.com/video720.mp4" || results[i].AudioURL != "https://cdn.example.com/audio720.m4a" {
				t.Fatalf("caller %d got ok=%v st=%+v", i, oks[i], results[i])
			}
		}

		raw, err := os.ReadFile(countFile)
		if err != nil {
			t.Fatalf("read count: %v", err)
		}
		if calls := len(strings.Split(strings.TrimSpace(string(raw)), "\n")); calls != 1 {
			t.Fatalf("expected 1 coalesced yt-dlp subprocess across all 3 WaitOrGetForMode callers, got %d", calls)
		}
	})
}
