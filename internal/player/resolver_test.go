package player

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
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
		if !strings.Contains(got, "--extractor-args\nyoutube:player_client=android\n") {
			t.Errorf("expected youtube:player_client=android extractor-args:\n%s", got)
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
				if strings.Contains(calls[0], "--cookies") || !strings.Contains(calls[0], "--extractor-args\nyoutube:player_client=android\n") {
					t.Errorf("attempt 1 should use cookieless android:\n%s", calls[0])
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
				if !strings.Contains(got, "--extractor-args\nyoutube:player_client=android\n") {
					t.Errorf("expected android extractor-args on single attempt:\n%s", got)
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
