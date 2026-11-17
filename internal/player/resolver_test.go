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

	"github.com/e12jassir/ytx/internal/model"
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
	if cap(r.sem) != 3 {
		t.Errorf("sem capacity = %d, want 3", cap(r.sem))
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

	t.Run("respects semaphore limit of 3 and cancels waiting goroutine on ctx Done", func(t *testing.T) {
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
		for _, id := range []string{"v1", "v2", "v3", "v4"} {
			r.Prefetch(ctx, model.Video{ID: id})
		}

		// Wait until 3 invocations have started.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			entries, _ := os.ReadDir(startedDir)
			if len(entries) >= 3 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Give a brief window to confirm a 4th process does not start while the gate is closed.
		time.Sleep(50 * time.Millisecond)
		entries, err := os.ReadDir(startedDir)
		if err != nil {
			t.Fatalf("readdir started: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("active yt-dlp processes = %d, want 3 (semaphore bound)", len(entries))
		}

		// Also verify a 5th Prefetch with an already-canceled context aborts while waiting for semaphore.
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		r.Prefetch(canceledCtx, model.Video{ID: "v5-canceled"})

		// Release the gate and wait for v1..v4 to complete.
		if err := os.WriteFile(gateFile, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write gate file: %v", err)
		}

		deadline = time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			_, ok1 := r.GetCached("v1")
			_, ok2 := r.GetCached("v2")
			_, ok3 := r.GetCached("v3")
			_, ok4 := r.GetCached("v4")
			if ok1 && ok2 && ok3 && ok4 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		for _, id := range []string{"v1", "v2", "v3", "v4"} {
			if _, ok := r.GetCached(id); !ok {
				t.Errorf("expected %s to be cached after semaphore release", id)
			}
		}
		if _, ok := r.GetCached("v5-canceled"); ok {
			t.Errorf("expected v5-canceled not to be cached when context was canceled")
		}
	})
}
