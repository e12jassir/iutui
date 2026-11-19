package tui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type safeTTYBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeTTYBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeTTYBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *safeTTYBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func resetThumbTestState() {
	asyncWG.Wait()

	SetThumbCacheDir("")
	SetThumbQuality("max")
	SetThumbHTTPClient(nil)
	SetThumbBaseURL("")
	SetOnThumbReady(nil)
	setTTYWriter(nil)
	setTTYOpener(nil)

	thumbCacheLRU.clear()
	kittyLRU.clear()

	fetchingMu.Lock()
	clear(fetchingMap)
	fetchingMu.Unlock()

	prepareMu.Lock()
	clear(preparingMap)
	prepareMu.Unlock()
}

func setupThumbTest(t *testing.T) string {
	t.Helper()
	cacheDir := t.TempDir()
	// Clear ambient terminal vars so non-Kitty tests are deterministic even inside Ghostty/Kitty.
	t.Setenv("YT_PREVIEW", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "")

	resetThumbTestState()
	SetThumbCacheDir(cacheDir)

	t.Cleanup(resetThumbTestState)
	return cacheDir
}

func makeTestJPEG(t *testing.T, w, h int, pixelFn func(x, y int) color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, pixelFn(x, y))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

func makeTestPNG(t *testing.T, w, h int, pixelFn func(x, y int) color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, pixelFn(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func writeThumbFixture(t *testing.T, cacheDir, cleanID, variant string, data []byte) string {
	t.Helper()
	thumbsDir := filepath.Join(cacheDir, "thumbs")
	if err := os.MkdirAll(thumbsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(thumbsDir, fmt.Sprintf("%s_%s.jpg", cleanID, variant))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}

func TestCleanThumbIDVariants(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "regular ID", in: "dQw4w9WgXcQ", want: "dQw4w9WgXcQ"},
		{name: "whitespace trimmed", in: "  dQw4w9WgXcQ \t\n", want: "dQw4w9WgXcQ"},
		{name: "RD mix prefix stripped", in: "RDOgeR2oqZGTs", want: "OgeR2oqZGTs"},
		{name: "RD mix prefix with whitespace", in: "  RDabcde12345  ", want: "abcde12345"},
		{name: "short RD preserved", in: "RD12345", want: "RD12345"},
		{name: "empty whitespace", in: "   ", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanThumbID(tc.in); got != tc.want {
				t.Errorf("cleanThumbID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestConfigSettersAndDefaults(t *testing.T) {
	xdgCache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdgCache)
	resetThumbTestState()
	t.Cleanup(resetThumbTestState)

	if got := getThumbCacheDir(); got != filepath.Join(xdgCache, "iutui") {
		t.Fatalf("default getThumbCacheDir() = %q, want %q", got, filepath.Join(xdgCache, "iutui"))
	}

	customDir := filepath.Join(t.TempDir(), "custom")
	SetThumbCacheDir(customDir)
	if got := getThumbCacheDir(); got != customDir {
		t.Fatalf("overridden getThumbCacheDir() = %q, want %q", got, customDir)
	}

	SetThumbCacheDir("")
	if got := getThumbCacheDir(); got != filepath.Join(xdgCache, "iutui") {
		t.Fatalf("restored getThumbCacheDir() = %q, want %q", got, filepath.Join(xdgCache, "iutui"))
	}

	customClient := &http.Client{Timeout: 2 * time.Second}
	SetThumbHTTPClient(customClient)
	if got := getThumbHTTPClient(); got != customClient {
		t.Fatalf("getThumbHTTPClient() did not return custom client")
	}
	SetThumbHTTPClient(nil)
	if got := getThumbHTTPClient(); got != defaultThumbHTTPClient {
		t.Fatalf("SetThumbHTTPClient(nil) did not restore default client")
	}

	SetThumbBaseURL("http://127.0.0.1:9999/vi/")
	if got := getThumbBaseURL(); got != "http://127.0.0.1:9999/vi" {
		t.Fatalf("getThumbBaseURL() = %q, want trailing slash trimmed", got)
	}
	SetThumbBaseURL("")
	if got := getThumbBaseURL(); got != defaultThumbBaseURL {
		t.Fatalf("SetThumbBaseURL(\"\") = %q, want %q", got, defaultThumbBaseURL)
	}
}

func TestGetThumbnailFileLookupOrder(t *testing.T) {
	cacheDir := setupThumbTest(t)

	if got := GetThumbnailFile(""); got != "" {
		t.Fatalf("empty ID returned %q, want empty", got)
	}
	if got := GetThumbnailFile("../escape"); got != "" {
		t.Fatalf("traversal ID returned %q, want empty", got)
	}

	id := "RDOgeR2oqZGTs"
	clean := "OgeR2oqZGTs"

	// Undersized files (< 1000 bytes) must be ignored.
	writeThumbFixture(t, cacheDir, clean, "maxresdefault", bytes.Repeat([]byte("a"), 999))
	if got := GetThumbnailFile(id); got != "" {
		t.Fatalf("undersized file returned %q, want empty", got)
	}

	hqPath := writeThumbFixture(t, cacheDir, clean, "hqdefault", bytes.Repeat([]byte("h"), 1200))
	if got := GetThumbnailFile(id); got != hqPath {
		t.Fatalf("got %q, want hqdefault %q", got, hqPath)
	}

	mqPath := writeThumbFixture(t, cacheDir, clean, "mqdefault", bytes.Repeat([]byte("m"), 1200))
	if got := GetThumbnailFile(id); got != mqPath {
		t.Fatalf("got %q, want mqdefault %q", got, mqPath)
	}

	maxPath := writeThumbFixture(t, cacheDir, clean, "maxresdefault", bytes.Repeat([]byte("x"), 3200))
	if got := GetThumbnailFile(id); got != maxPath {
		t.Fatalf("got %q, want maxresdefault %q", got, maxPath)
	}
}

func TestFetchThumbnailAsyncMaxResAndFallbacks(t *testing.T) {
	cacheDir := setupThumbTest(t)

	maxPayload := makeTestJPEG(t, 128, 72, func(x, y int) color.RGBA {
		return color.RGBA{R: 20, G: 180, B: 90, A: 255}
	})
	if len(maxPayload) < 3000 {
		// Pad comment/bytes so JPEG payload exceeds 3000 bytes reliably.
		maxPayload = append(maxPayload, bytes.Repeat([]byte{0x00}, 3200-len(maxPayload))...)
	}
	mqPayload := bytes.Repeat([]byte("M"), 1500)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vidmax01/maxresdefault.jpg":
			_, _ = w.Write(maxPayload)
		case "/vid404mq/maxresdefault.jpg":
			http.NotFound(w, r)
		case "/vid404mq/mqdefault.jpg":
			_, _ = w.Write(mqPayload)
		case "/vidsmall/maxresdefault.jpg":
			_, _ = w.Write(bytes.Repeat([]byte("S"), 2500)) // < 3000 rejected
		case "/vidsmall/mqdefault.jpg":
			_, _ = w.Write(mqPayload)
		case "/vidbad01/maxresdefault.jpg":
			_, _ = w.Write(bytes.Repeat([]byte("X"), 500))
		case "/vidbad01/mqdefault.jpg":
			_, _ = w.Write(bytes.Repeat([]byte("Y"), 999)) // < 1000 rejected
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	SetThumbBaseURL(srv.URL)
	SetThumbHTTPClient(srv.Client())

	readyCh := make(chan string, 16)
	SetOnThumbReady(func(videoID string) {
		readyCh <- videoID
	})

	// Short/invalid IDs are ignored without firing callbacks.
	FetchThumbnailAsync("abc")
	FetchThumbnailAsync("../bad")

	// 1. maxresdefault succeeds
	FetchThumbnailAsync("vidmax01")
	select {
	case got := <-readyCh:
		if got != "vidmax01" {
			t.Fatalf("ready videoID = %q, want vidmax01", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for vidmax01 ready callback")
	}
	maxFile := GetThumbnailFile("vidmax01")
	if !strings.HasSuffix(maxFile, "vidmax01_maxresdefault.jpg") {
		t.Fatalf("GetThumbnailFile(vidmax01) = %q, want maxresdefault", maxFile)
	}

	// 2. 404 on maxresdefault falls back to mqdefault
	FetchThumbnailAsync("vid404mq")
	select {
	case got := <-readyCh:
		if got != "vid404mq" {
			t.Fatalf("ready videoID = %q, want vid404mq", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for vid404mq ready callback")
	}
	if got := GetThumbnailFile("vid404mq"); !strings.HasSuffix(got, "vid404mq_mqdefault.jpg") {
		t.Fatalf("GetThumbnailFile(vid404mq) = %q, want mqdefault", got)
	}

	// 3. Small maxresdefault (< 3000 bytes) rejected and falls back to mqdefault
	FetchThumbnailAsync("vidsmall")
	select {
	case got := <-readyCh:
		if got != "vidsmall" {
			t.Fatalf("ready videoID = %q, want vidsmall", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for vidsmall ready callback")
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "thumbs", "vidsmall_maxresdefault.jpg")); !os.IsNotExist(err) {
		t.Fatalf("undersized maxresdefault should not have been saved, err=%v", err)
	}
	if got := GetThumbnailFile("vidsmall"); !strings.HasSuffix(got, "vidsmall_mqdefault.jpg") {
		t.Fatalf("GetThumbnailFile(vidsmall) = %q, want mqdefault", got)
	}

	// 4. Both variants too small -> neither saved, callback not fired, no temp files leaked
	FetchThumbnailAsync("vidbad01")
	asyncWG.Wait()
	select {
	case extra := <-readyCh:
		t.Fatalf("unexpected callback for failed download: %q", extra)
	default:
	}
	if got := GetThumbnailFile("vidbad01"); got != "" {
		t.Fatalf("expected no thumbnail file for vidbad01, got %q", got)
	}

	entries, err := os.ReadDir(filepath.Join(cacheDir, "thumbs"))
	if err != nil {
		t.Fatalf("ReadDir(thumbs): %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".thumb-") {
			t.Fatalf("leaked temp file in thumbsDir: %s", entry.Name())
		}
	}
}

func TestFetchThumbnailAsyncBoundedConcurrencyAndDedup(t *testing.T) {
	_ = setupThumbTest(t)

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var reqCount atomic.Int32
	payload := bytes.Repeat([]byte("Z"), 3200)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		cur := inFlight.Add(1)
		for {
			prev := maxInFlight.Load()
			if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
		inFlight.Add(-1)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	SetThumbBaseURL(srv.URL)
	SetThumbHTTPClient(srv.Client())

	var readyCount atomic.Int32
	SetOnThumbReady(func(string) {
		readyCount.Add(1)
	})

	// Launch 18 distinct downloads + duplicates for video_000.
	for i := 0; i < 18; i++ {
		id := fmt.Sprintf("video_%03d", i)
		FetchThumbnailAsync(id)
		if i == 0 {
			FetchThumbnailAsync(id)
			FetchThumbnailAsync(id)
		}
	}

	asyncWG.Wait()

	if maxSeen := maxInFlight.Load(); maxSeen > 6 || maxSeen < 2 {
		t.Fatalf("max concurrent HTTP fetches = %d, want 2..6 (bounded by 6-worker semaphore)", maxSeen)
	}
	if got := readyCount.Load(); got != 18 {
		t.Fatalf("ready callbacks = %d, want 18", got)
	}
	if got := reqCount.Load(); got != 18 {
		t.Fatalf("HTTP requests = %d, want 18 (deduped duplicates)", got)
	}
}

func TestRenderThumbnailANSIBlockModeAndLetterboxCrop(t *testing.T) {
	cacheDir := setupThumbTest(t)

	if got := RenderThumbnailANSI("anyvid01", 0, 5); got != "" {
		t.Fatalf("zero width returned %q, want empty", got)
	}
	if got := RenderThumbnailANSI("anyvid01", 10, 0); got != "" {
		t.Fatalf("zero height returned %q, want empty", got)
	}

	// Missing thumbnail returns placeholder box and starts async fetch.
	empty := RenderThumbnailANSI("missing1", 18, 5)
	if !strings.Contains(empty, "YouTube 16:9") || len(strings.Split(empty, "\n")) != 5 {
		t.Fatalf("unexpected empty thumb output:\n%s", empty)
	}
	asyncWG.Wait()

	// 1. Synthetic 16:9 JPEG
	jpg16x9 := makeTestJPEG(t, 128, 72, func(x, y int) color.RGBA {
		if x < 64 {
			return color.RGBA{R: 220, G: 40, B: 40, A: 255}
		}
		return color.RGBA{R: 40, G: 80, B: 230, A: 255}
	})
	if len(jpg16x9) < 1000 {
		jpg16x9 = append(jpg16x9, bytes.Repeat([]byte{0}, 1200-len(jpg16x9))...)
	}
	writeThumbFixture(t, cacheDir, "jpg16x9v", "maxresdefault", jpg16x9)

	out16x9 := RenderThumbnailANSI("jpg16x9v", 12, 6)
	lines16x9 := strings.Split(out16x9, "\n")
	if len(lines16x9) != 6 {
		t.Fatalf("16:9 block lines = %d, want 6", len(lines16x9))
	}
	if !strings.Contains(out16x9, "\x1b[38;2;") || !strings.Contains(out16x9, "\x1b[48;2;") {
		t.Fatalf("expected 24-bit truecolor ANSI escapes in block output")
	}
	if !strings.ContainsAny(out16x9, "▀▄▌▐▛▜▙▟▘▝▖▗▚▞") {
		t.Fatalf("expected Unicode quadrant/half-block glyphs in output")
	}
	// Cached hit returns identical output.
	if again := RenderThumbnailANSI("jpg16x9v", 12, 6); again != out16x9 {
		t.Fatalf("second RenderThumbnailANSI call did not match cached output")
	}

	// 2. Synthetic 4:3 PNG (160x120): top/bottom letterbox bars are bright red (255,0,0),
	// while the central 16:9 band (y in [15, 105)) is solid green (0,210,0).
	png4x3 := makeTestPNG(t, 160, 120, func(x, y int) color.RGBA {
		if y < 15 || y >= 105 {
			return color.RGBA{R: 255, G: 0, B: 0, A: 255}
		}
		return color.RGBA{R: 0, G: 210, B: 0, A: 255}
	})
	if len(png4x3) < 1000 {
		png4x3 = append(png4x3, bytes.Repeat([]byte{0}, 1200-len(png4x3))...)
	}
	writeThumbFixture(t, cacheDir, "png4x3vid", "hqdefault", png4x3)

	out4x3 := RenderThumbnailANSI("png4x3vid", 12, 6)
	if strings.Contains(out4x3, "YouTube 16:9") {
		t.Fatalf("expected rendered PNG block art, got empty placeholder")
	}
	// Verify cropped letterbox: no red-dominant ANSI color sequences (38;2;R;G;B or 48;2;R;G;B) appear.
	if strings.Contains(out4x3, ";2;255;0;0m") || strings.Contains(out4x3, ";2;254;") {
		t.Fatalf("4:3 letterbox red bar leaked into cropped 16:9 output:\n%s", out4x3)
	}
	if !strings.Contains(out4x3, ";2;0;21") && !strings.Contains(out4x3, ";2;0;22") {
		t.Fatalf("expected green central region color in cropped 4:3 PNG output, got:\n%s", out4x3)
	}

	// 3. Corrupt image falls back gracefully to empty placeholder.
	writeThumbFixture(t, cacheDir, "corrupt1", "mqdefault", bytes.Repeat([]byte("not-an-image"), 150))
	outCorrupt := RenderThumbnailANSI("corrupt1", 16, 4)
	if !strings.Contains(outCorrupt, "YouTube 16:9") {
		t.Fatalf("expected empty placeholder for corrupt image, got:\n%s", outCorrupt)
	}
}

func TestThumbCacheLRUEviction(t *testing.T) {
	cacheDir := setupThumbTest(t)

	jpg := makeTestJPEG(t, 64, 36, func(x, y int) color.RGBA {
		return color.RGBA{R: uint8(x * 3), G: 120, B: uint8(y * 5), A: 255}
	})
	if len(jpg) < 1000 {
		jpg = append(jpg, bytes.Repeat([]byte{0}, 1200-len(jpg))...)
	}

	for i := 0; i < 130; i++ {
		id := fmt.Sprintf("lruvid_%03d", i)
		writeThumbFixture(t, cacheDir, id, "maxresdefault", jpg)
		out := RenderThumbnailANSI(id, 8, 4)
		if strings.Contains(out, "YouTube 16:9") {
			t.Fatalf("expected rendered thumbnail for %s", id)
		}
	}

	if got := thumbCacheLRU.len(); got != maxThumbCacheEntries {
		t.Fatalf("thumbCacheLRU.len() = %d, want %d", got, maxThumbCacheEntries)
	}

	if _, ok := thumbCacheLRU.get("lruvid_000_8x4_k=false"); ok {
		t.Fatalf("expected oldest entry lruvid_000 to be evicted")
	}
	if _, ok := thumbCacheLRU.get("lruvid_001_8x4_k=false"); ok {
		t.Fatalf("expected second-oldest entry lruvid_001 to be evicted")
	}
	// Touch lruvid_002 so lruvid_003 becomes LRU, then insert one more.
	if _, ok := thumbCacheLRU.get("lruvid_002_8x4_k=false"); !ok {
		t.Fatalf("expected lruvid_002 to still be cached")
	}

	writeThumbFixture(t, cacheDir, "lruvid_130", "maxresdefault", jpg)
	_ = RenderThumbnailANSI("lruvid_130", 8, 4)

	if _, ok := thumbCacheLRU.get("lruvid_002_8x4_k=false"); !ok {
		t.Fatalf("expected recently touched lruvid_002 to remain cached")
	}
	if _, ok := thumbCacheLRU.get("lruvid_003_8x4_k=false"); ok {
		t.Fatalf("expected lruvid_003 to be evicted after lruvid_002 was touched")
	}
}

func TestKittyModeUploadPlaceholdersBrandLogoAndLRUEviction(t *testing.T) {
	cacheDir := setupThumbTest(t)
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("YT_PREVIEW", "kitty")

	if !isKittyGraphicsSupported() {
		t.Fatal("expected isKittyGraphicsSupported() == true in kitty mode")
	}

	ttyBuf := &safeTTYBuffer{}
	setTTYWriter(ttyBuf)

	// 1. Brand logo renders via Kitty virtual placement and stays pinned.
	if got := RenderBrandLogo(0, 3); got != "" {
		t.Fatalf("RenderBrandLogo(0,3) = %q, want empty", got)
	}
	if got := RenderBrandLogo(65, 3); got != "" {
		t.Fatalf("RenderBrandLogo(65,3) = %q, want empty", got)
	}
	brandOut := RenderBrandLogo(10, 3)
	brandLines := strings.Split(brandOut, "\n")
	if len(brandLines) != 3 {
		t.Fatalf("brand logo rows = %d, want 3", len(brandLines))
	}
	if !strings.ContainsRune(brandOut, 0x10EEEE) {
		t.Fatalf("expected U+10EEEE placeholders in RenderBrandLogo output")
	}
	brandUploadSeq := fmt.Sprintf("\x1b_Ga=T,U=1,q=2,f=100,i=%d,c=10,r=3,C=1,m=", brandImgID)
	if !strings.Contains(ttyBuf.String(), brandUploadSeq) {
		t.Fatalf("expected brand upload APC sequence %q in TTY output", brandUploadSeq)
	}
	// Second RenderBrandLogo call hits cache without transmitting again.
	beforeLen := len(ttyBuf.String())
	if again := RenderBrandLogo(10, 3); again != brandOut {
		t.Fatalf("cached RenderBrandLogo mismatch")
	}
	if len(ttyBuf.String()) != beforeLen {
		t.Fatalf("RenderBrandLogo re-uploaded on cache hit")
	}

	// 2. Test Kitty upload + ready callback + placeholder cache hit with 4:3 PNG.
	readyCh := make(chan string, 128)
	SetOnThumbReady(func(videoID string) {
		readyCh <- videoID
	})

	pngThumb := makeTestPNG(t, 80, 60, func(x, y int) color.RGBA {
		return color.RGBA{R: 30, G: 140, B: 220, A: 255}
	})
	if len(pngThumb) < 1000 {
		pngThumb = append(pngThumb, bytes.Repeat([]byte{0}, 1200-len(pngThumb))...)
	}

	// Upload 97 unique video thumbnails to trigger Kitty LRU eviction (> 96).
	for i := 0; i < 97; i++ {
		id := fmt.Sprintf("kittyvid_%03d", i)
		writeThumbFixture(t, cacheDir, id, "maxresdefault", pngThumb)

		// First render initiates async Kitty upload and returns empty placeholder.
		first := RenderThumbnailANSI(id, 10, 5)
		if !strings.Contains(first, "YouTube 16:9") {
			t.Fatalf("expected empty thumb on initial Kitty async render for %s", id)
		}

		select {
		case gotID := <-readyCh:
			if gotID != id {
				t.Fatalf("onThumbReady got %q, want %q", gotID, id)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for Kitty prepare of %s", id)
		}

		// Subsequent render hits thumbCacheLRU and returns U+10EEEE placeholders.
		rendered := RenderThumbnailANSI(id, 10, 5)
		rows := strings.Split(rendered, "\n")
		if len(rows) != 5 {
			t.Fatalf("Kitty placeholder rows = %d, want 5", len(rows))
		}
		if !strings.ContainsRune(rendered, 0x10EEEE) || !strings.Contains(rendered, "\x1b[39m") {
			t.Fatalf("invalid Kitty placeholder output for %s", id)
		}
	}

	firstImgID := videoImageID("kittyvid_000", 10, 5)
	secondImgID := videoImageID("kittyvid_001", 10, 5)
	lastImgID := videoImageID("kittyvid_096", 10, 5)

	ttyOut := ttyBuf.String()
	uploadPrefixFirst := fmt.Sprintf("\x1b_Ga=T,U=1,q=2,f=100,i=%d,c=10,r=5,C=1,m=", firstImgID)
	uploadPrefixLast := fmt.Sprintf("\x1b_Ga=T,U=1,q=2,f=100,i=%d,c=10,r=5,C=1,m=", lastImgID)
	deleteFirst := fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", firstImgID)
	deleteBrand := fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", brandImgID)

	if !strings.Contains(ttyOut, uploadPrefixFirst) {
		t.Fatalf("missing Kitty upload APC escape for first image (%d)", firstImgID)
	}
	if !strings.Contains(ttyOut, uploadPrefixLast) {
		t.Fatalf("missing Kitty upload APC escape for 97th image (%d)", lastImgID)
	}
	if !strings.Contains(ttyOut, deleteFirst) {
		t.Fatalf("expected Kitty LRU eviction delete escape %q", deleteFirst)
	}
	if strings.Contains(ttyOut, deleteBrand) {
		t.Fatalf("pinned brandImgID (%d) must not be evicted", brandImgID)
	}
	if got := kittyLRU.unpinnedLen(); got != maxKittyImages {
		t.Fatalf("kittyLRU.unpinnedLen() = %d, want %d", got, maxKittyImages)
	}

	// Brand logo must still be cached and pinned without re-uploading.
	ttyBuf.Reset()
	if got := RenderBrandLogo(10, 3); got != brandOut {
		t.Fatalf("RenderBrandLogo after 97 video uploads did not return cached brand logo")
	}
	if ttyBuf.String() != "" {
		t.Fatalf("pinned brand logo triggered unexpected TTY write: %q", ttyBuf.String())
	}

	// Re-rendering evicted kittyvid_000 must detect eviction from kittyLRU and re-upload cleanly,
	// evicting kittyvid_001 in turn.
	reRender := RenderThumbnailANSI("kittyvid_000", 10, 5)
	if !strings.Contains(reRender, "YouTube 16:9") {
		t.Fatalf("expected async re-upload placeholder after Kitty LRU eviction")
	}
	select {
	case gotID := <-readyCh:
		if gotID != "kittyvid_000" {
			t.Fatalf("re-upload callback got %q, want kittyvid_000", gotID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for kittyvid_000 re-upload")
	}
	deleteSecond := fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", secondImgID)
	if !strings.Contains(ttyBuf.String(), deleteSecond) {
		t.Fatalf("expected re-upload of kittyvid_000 to evict kittyvid_001 (%q)", deleteSecond)
	}
	if got := RenderThumbnailANSI("kittyvid_000", 10, 5); !strings.ContainsRune(got, 0x10EEEE) {
		t.Fatalf("expected Kitty placeholders after re-upload")
	}
}

func TestKittyUploadTTYAvailabilityAndErrors(t *testing.T) {
	cacheDir := setupThumbTest(t)
	t.Setenv("TERM", "xterm-kitty")

	// Inject failing TTY opener (no ttyWriter) to verify uploadKittyImage aborts cleanly without touching /dev/tty.
	setTTYWriter(nil)
	setTTYOpener(func() (io.WriteCloser, error) {
		return nil, errors.New("no tty")
	})

	jpg := makeTestJPEG(t, 64, 36, func(x, y int) color.RGBA {
		return color.RGBA{R: 100, G: 150, B: 200, A: 255}
	})
	if len(jpg) < 1000 {
		jpg = append(jpg, bytes.Repeat([]byte{0}, 1200-len(jpg))...)
	}
	path := writeThumbFixture(t, cacheDir, "notty001", "maxresdefault", jpg)

	if err := uploadKittyImage(12345, path, 10, 5); err == nil {
		t.Fatal("expected error when TTY is unavailable")
	}
	if RenderBrandLogo(8, 3) != "" {
		t.Fatal("expected RenderBrandLogo to return empty string when TTY is unavailable")
	}

	// Failing ttyWriter during transmitKittyPNG must not record image in kittyLRU.
	setTTYWriter(failingWriter{err: errors.New("broken pipe")})
	if err := uploadKittyImage(12345, path, 10, 5); err == nil {
		t.Fatal("expected error from failing ttyWriter")
	}
	if kittyLRU.touch(12345) {
		t.Fatal("failed upload must not be recorded in kittyLRU")
	}

	if err := transmitKittyPNG(12345, nil, 10, 5); err == nil {
		t.Fatal("expected error for empty PNG payload")
	}
	if _, _, err := renderKittyPlaceholder("notty001", path, 0, 5); err == nil {
		t.Fatal("expected error for invalid Kitty placeholder dimensions")
	}
}

func TestSetThumbQualityAndClearThumbCache(t *testing.T) {
	cacheDir := setupThumbTest(t)
	jpg := makeTestJPEG(t, 64, 36, func(x, y int) color.RGBA {
		return color.RGBA{R: 80, G: 160, B: 220, A: 255}
	})
	if len(jpg) < 3200 {
		jpg = append(jpg, bytes.Repeat([]byte{0}, 3200-len(jpg))...)
	}

	// Write all three variants locally to verify lookup priority changes live with SetThumbQuality.
	writeThumbFixture(t, cacheDir, "qvid001", "maxresdefault", jpg)
	writeThumbFixture(t, cacheDir, "qvid001", "hqdefault", jpg)
	writeThumbFixture(t, cacheDir, "qvid001", "mqdefault", jpg)

	SetThumbQuality("max")
	if got := GetThumbnailFile("qvid001"); !strings.HasSuffix(got, "qvid001_maxresdefault.jpg") {
		t.Fatalf("quality=max got %q, want *_maxresdefault.jpg", got)
	}
	SetThumbQuality("hq")
	if got := GetThumbnailFile("qvid001"); !strings.HasSuffix(got, "qvid001_hqdefault.jpg") {
		t.Fatalf("quality=hq got %q, want *_hqdefault.jpg", got)
	}
	SetThumbQuality("mq")
	if got := GetThumbnailFile("qvid001"); !strings.HasSuffix(got, "qvid001_mqdefault.jpg") {
		t.Fatalf("quality=mq got %q, want *_mqdefault.jpg", got)
	}
	SetThumbQuality("invalid")
	if got := GetThumbnailFile("qvid001"); !strings.HasSuffix(got, "qvid001_maxresdefault.jpg") {
		t.Fatalf("invalid quality should fall back to max, got %q", got)
	}

	// Verify ClearThumbCache evicts video entries from thumbCacheLRU & kittyLRU while keeping pinned brandImgID.
	ttyBuf := &safeTTYBuffer{}
	setTTYWriter(ttyBuf)
	t.Setenv("KITTY_WINDOW_ID", "1")
	t.Setenv("TERM", "xterm-kitty")

	if brand := RenderBrandLogo(10, 3); !strings.ContainsRune(brand, 0x10EEEE) {
		t.Fatalf("expected pinned brand logo placeholder, got %q", brand)
	}
	pngThumb := makeTestPNG(t, 32, 18, func(x, y int) color.RGBA {
		return color.RGBA{R: 120, G: 90, B: 200, A: 255}
	})
	vidPath := writeThumbFixture(t, cacheDir, "qvid002", "maxresdefault", pngThumb)
	vidImgID := videoImageID("qvid002", 8, 4)
	if _, gotID, err := renderKittyPlaceholder("qvid002", vidPath, 8, 4); err != nil || gotID != vidImgID {
		t.Fatalf("renderKittyPlaceholder = (%d, %v), want %d", gotID, err, vidImgID)
	}
	thumbCacheLRU.put("qvid002_8x4_k=true", thumbCacheValue{ansi: "cached", kittyImgID: vidImgID}, false)

	ttyBuf.Reset()
	ClearThumbCache()

	if _, ok := thumbCacheLRU.get("qvid002_8x4_k=true"); ok {
		t.Fatal("expected ClearThumbCache to evict video entry from thumbCacheLRU")
	}
	if _, ok := thumbCacheLRU.get("__brand_logo_10x3"); !ok {
		t.Fatal("expected ClearThumbCache to keep pinned brand logo in thumbCacheLRU")
	}
	if kittyLRU.touch(vidImgID) {
		t.Fatal("expected ClearThumbCache to evict unpinned video image from kittyLRU")
	}
	if !kittyLRU.touch(brandImgID) {
		t.Fatal("expected ClearThumbCache to keep pinned brandImgID in kittyLRU")
	}
	delVid := fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", vidImgID)
	delBrand := fmt.Sprintf("\x1b_Ga=d,d=i,i=%d,q=2\x1b\\", brandImgID)
	out := ttyBuf.String()
	if !strings.Contains(out, delVid) {
		t.Fatalf("expected Kitty delete sequence %q for video image, got %q", delVid, out)
	}
	if strings.Contains(out, delBrand) {
		t.Fatalf("ClearThumbCache must not delete pinned brandImgID (%q)", delBrand)
	}
}
