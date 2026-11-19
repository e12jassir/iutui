package download

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

func TestNewManager(t *testing.T) {
	tests := []struct {
		name      string
		ytdlpBin  string
		wantYtdlp string
	}{
		{
			name:      "default binary when empty",
			ytdlpBin:  "",
			wantYtdlp: "yt-dlp",
		},
		{
			name:      "custom binary preserved",
			ytdlpBin:  "/usr/local/bin/yt-dlp",
			wantYtdlp: "/usr/local/bin/yt-dlp",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			dlDir := t.TempDir()
			m := NewManager(stateDir, dlDir, tc.ytdlpBin)
			if m.YtdlpBin != tc.wantYtdlp {
				t.Fatalf("YtdlpBin = %q, want %q", m.YtdlpBin, tc.wantYtdlp)
			}
			if m.StateDir != stateDir {
				t.Fatalf("StateDir = %q, want %q", m.StateDir, stateDir)
			}
			if m.DownloadDir != dlDir {
				t.Fatalf("DownloadDir = %q, want %q", m.DownloadDir, dlDir)
			}
		})
	}
}

func TestEnqueueArgsAndMeta(t *testing.T) {
	tests := []struct {
		name      string
		video     model.Video
		audioOnly bool
		wantArgs  func(dlDir, id string) []string
	}{
		{
			name: "video download",
			video: model.Video{
				ID:      "dQw4w9WgXcQ",
				Title:   "Never Gonna Give You Up",
				Channel: "Rick Astley",
			},
			audioOnly: false,
			wantArgs: func(dlDir, id string) []string {
				return []string{
					"-o", filepath.Join(dlDir, "%(title)s.%(ext)s"),
					"--", "https://www.youtube.com/watch?v=" + id,
				}
			},
		},
		{
			name: "audio only download",
			video: model.Video{
				ID:      "abc123XYZ",
				Title:   "Track One",
				Channel: "Artist",
			},
			audioOnly: true,
			wantArgs: func(dlDir, id string) []string {
				return []string{
					"-x", "--audio-format", "mp3", "--embed-thumbnail",
					"-o", filepath.Join(dlDir, "%(title)s.%(ext)s"),
					"--", "https://www.youtube.com/watch?v=" + id,
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			dlDir := t.TempDir()
			m := NewManager(stateDir, dlDir, "yt-dlp-mock")

			var gotBin string
			var gotArgs []string
			var statusDuringRun Status

			m.Runner = func(ctx context.Context, bin string, args ...string) error {
				gotBin = bin
				gotArgs = append([]string(nil), args...)

				items, err := m.List()
				if err != nil {
					return err
				}
				for _, it := range items {
					if it.Video.ID == tc.video.ID {
						statusDuringRun = it.Status
					}
				}
				return nil
			}

			if err := m.Enqueue(context.Background(), tc.video, tc.audioOnly); err != nil {
				t.Fatalf("Enqueue() unexpected error: %v", err)
			}

			if gotBin != "yt-dlp-mock" {
				t.Fatalf("runner bin = %q, want %q", gotBin, "yt-dlp-mock")
			}

			wantArgs := tc.wantArgs(dlDir, tc.video.ID)
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Fatalf("runner args = %v, want %v", gotArgs, wantArgs)
			}

			if statusDuringRun != StatusDownloading {
				t.Fatalf("status during run = %q, want %q", statusDuringRun, StatusDownloading)
			}

			metaPath := filepath.Join(stateDir, "downloads", tc.video.ID+".meta")
			raw, err := os.ReadFile(metaPath)
			if err != nil {
				t.Fatalf("read meta file: %v", err)
			}
			wantMeta := tc.video.Title + "\n" + tc.video.Channel + "\n"
			if string(raw) != wantMeta {
				t.Fatalf("meta content = %q, want %q", string(raw), wantMeta)
			}

			items, err := m.List()
			if err != nil {
				t.Fatalf("List() unexpected error: %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("List() len = %d, want 1", len(items))
			}
			if items[0].Status != StatusDone {
				t.Fatalf("item status = %q, want %q", items[0].Status, StatusDone)
			}
			if items[0].AudioOnly != tc.audioOnly {
				t.Fatalf("item AudioOnly = %v, want %v", items[0].AudioOnly, tc.audioOnly)
			}
			if items[0].OutputPath != filepath.Join(dlDir, "%(title)s.%(ext)s") {
				t.Fatalf("item OutputPath = %q, want %q", items[0].OutputPath, filepath.Join(dlDir, "%(title)s.%(ext)s"))
			}
		})
	}
}

func TestEnqueueFailureAndCancellation(t *testing.T) {
	tests := []struct {
		name           string
		video          model.Video
		setupCtx       func() (context.Context, context.CancelFunc)
		runner         Runner
		wantErrSubstr  string
		wantItemInList bool
		wantStatus     Status
	}{
		{
			name:  "runner returns error",
			video: model.Video{ID: "fail1", Title: "Broken Video", Channel: "Chan"},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			runner: func(ctx context.Context, bin string, args ...string) error {
				return errors.New("network timeout")
			},
			wantErrSubstr:  "network timeout",
			wantItemInList: true,
			wantStatus:     StatusFailed,
		},
		{
			name:  "context cancelled",
			video: model.Video{ID: "cancel1", Title: "Cancelled Video", Channel: "Chan"},
			setupCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			runner: func(ctx context.Context, bin string, args ...string) error {
				return ctx.Err()
			},
			wantErrSubstr:  context.Canceled.Error(),
			wantItemInList: true,
			wantStatus:     StatusFailed,
		},
		{
			name:  "empty video ID rejected",
			video: model.Video{ID: "", Title: "No ID", Channel: "Chan"},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			runner: func(ctx context.Context, bin string, args ...string) error {
				t.Fatal("runner should not be called for invalid ID")
				return nil
			},
			wantErrSubstr:  "required",
			wantItemInList: false,
		},
		{
			name:  "path traversal ID rejected",
			video: model.Video{ID: "../evil", Title: "Evil", Channel: "Chan"},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			runner: func(ctx context.Context, bin string, args ...string) error {
				t.Fatal("runner should not be called for invalid ID")
				return nil
			},
			wantErrSubstr:  "invalid video ID",
			wantItemInList: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			dlDir := t.TempDir()
			m := NewManager(stateDir, dlDir, "")
			m.Runner = tc.runner

			ctx, cancel := tc.setupCtx()
			defer cancel()

			err := m.Enqueue(ctx, tc.video, false)
			if err == nil {
				t.Fatalf("Enqueue() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstr) {
				t.Fatalf("Enqueue() error = %q, want substring %q", err.Error(), tc.wantErrSubstr)
			}

			items, listErr := m.List()
			if listErr != nil {
				t.Fatalf("List() unexpected error: %v", listErr)
			}

			if !tc.wantItemInList {
				if len(items) != 0 {
					t.Fatalf("List() len = %d, want 0", len(items))
				}
				return
			}

			if len(items) != 1 {
				t.Fatalf("List() len = %d, want 1", len(items))
			}
			if items[0].Status != tc.wantStatus {
				t.Fatalf("item status = %q, want %q", items[0].Status, tc.wantStatus)
			}
			if !strings.Contains(items[0].Err, tc.wantErrSubstr) {
				t.Fatalf("item Err = %q, want substring %q", items[0].Err, tc.wantErrSubstr)
			}
		})
	}
}

func TestListMergesDiskMetaAndActiveQueue(t *testing.T) {
	stateDir := t.TempDir()
	dlDir := t.TempDir()
	metaDir := filepath.Join(stateDir, "downloads")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "old123.meta"), []byte("Old Title\nOld Channel\n"), 0o644); err != nil {
		t.Fatalf("write old meta: %v", err)
	}

	m := NewManager(stateDir, dlDir, "")
	m.Runner = func(ctx context.Context, bin string, args ...string) error {
		return nil
	}

	activeVideo := model.Video{
		ID:      "new456",
		Title:   "New Title",
		Channel: "New Channel",
	}
	if err := m.Enqueue(context.Background(), activeVideo, true); err != nil {
		t.Fatalf("Enqueue() error: %v", err)
	}

	items, err := m.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("List() len = %d, want 2", len(items))
	}

	if items[0].Video.ID != "new456" || !items[0].AudioOnly || items[0].Status != StatusDone {
		t.Fatalf("items[0] = %+v, want active new456 AudioOnly=true Status=done", items[0])
	}
	if items[1].Video.ID != "old123" || items[1].Video.Title != "Old Title" || items[1].Video.Channel != "Old Channel" {
		t.Fatalf("items[1] = %+v, want disk old123", items[1])
	}
}

func TestRunWithProgressAndLocalFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script test")
	}

	stateDir := t.TempDir()
	dlDir := t.TempDir()
	binDir := t.TempDir()
	argsLog := filepath.Join(binDir, "args.log")
	outFile := filepath.Join(dlDir, "Real Video.mp4")
	fakeBin := filepath.Join(binDir, "yt-dlp")

	script := fmt.Sprintf(`#!/bin/sh
: > %q
for a in "$@"; do
  printf '%%s\n' "$a" >> %q
done
printf '%%s\n' 'IUTUI_PROG| 25.0%%|2.50MiB/s|00:12'
printf '%%s\n' 'IUTUI_PROG| 80.5%%|3.10MiB/s|00:03' >&2
printf 'video-data' > %q
printf 'IUTUI_FILE|%%s\n' %q
`, argsLog, argsLog, outFile, outFile)

	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}

	m := NewManager(stateDir, dlDir, fakeBin)

	var progMu sync.Mutex
	var updates []Item
	m.OnProgress = func(it Item) {
		progMu.Lock()
		updates = append(updates, it)
		progMu.Unlock()
	}

	v := model.Video{
		ID:      "prog123",
		Title:   "Real Video",
		Channel: "Demo Channel",
	}

	if err := m.Enqueue(context.Background(), v, false); err != nil {
		t.Fatalf("Enqueue() unexpected error: %v", err)
	}

	rawArgs, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	gotArgs := strings.Split(strings.TrimSuffix(string(rawArgs), "\n"), "\n")
	wantArgs := []string{
		"--newline",
		"--progress-template",
		"download:IUTUI_PROG|%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s",
		"--print",
		"after_move:IUTUI_FILE|%(filepath)s",
		"-o", filepath.Join(dlDir, "%(title)s.%(ext)s"),
		"--", "https://www.youtube.com/watch?v=" + v.ID,
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("yt-dlp args = %v, want %v", gotArgs, wantArgs)
	}

	progMu.Lock()
	var saw25, saw80, sawFile, sawDone bool
	for _, u := range updates {
		if u.Progress == 25.0 && u.Speed == "2.50MiB/s" && u.ETA == "00:12" {
			saw25 = true
		}
		if u.Progress == 80.5 && u.Speed == "3.10MiB/s" && u.ETA == "00:03" {
			saw80 = true
		}
		if u.FilePath == outFile {
			sawFile = true
		}
		if u.Status == StatusDone && u.Progress == 100 && u.FilePath == outFile {
			sawDone = true
		}
	}
	progMu.Unlock()

	if !saw25 || !saw80 || !sawFile || !sawDone {
		t.Fatalf("missing expected OnProgress updates: saw25=%v saw80=%v sawFile=%v sawDone=%v updates=%+v",
			saw25, saw80, sawFile, sawDone, updates)
	}

	metaPath := filepath.Join(stateDir, "downloads", v.ID+".meta")
	rawMeta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	wantMeta := v.Title + "\n" + v.Channel + "\n" + outFile + "\n"
	if string(rawMeta) != wantMeta {
		t.Fatalf("meta content = %q, want %q", string(rawMeta), wantMeta)
	}

	items, err := m.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(items) != 1 || items[0].FilePath != outFile || items[0].Progress != 100 || items[0].Status != StatusDone {
		t.Fatalf("List() = %+v, want FilePath=%q Progress=100 Status=done", items, outFile)
	}

	if got := m.LocalFile(v.ID); got != outFile {
		t.Fatalf("m.LocalFile(%q) = %q, want %q", v.ID, got, outFile)
	}

	// Fresh manager should recover FilePath from .meta on disk.
	m2 := NewManager(stateDir, dlDir, "")
	diskItems, err := m2.List()
	if err != nil {
		t.Fatalf("m2.List() error: %v", err)
	}
	if len(diskItems) != 1 || diskItems[0].FilePath != outFile {
		t.Fatalf("m2.List() = %+v, want FilePath=%q", diskItems, outFile)
	}
	if got := m2.LocalFile(v.ID); got != outFile {
		t.Fatalf("m2.LocalFile(%q) = %q, want %q", v.ID, got, outFile)
	}

	// Invalid/missing IDs return empty.
	for _, badID := range []string{"", "../evil", "unknown"} {
		if got := m.LocalFile(badID); got != "" {
			t.Fatalf("m.LocalFile(%q) = %q, want empty", badID, got)
		}
	}

	// Zero-byte file should not count as valid local file.
	if err := os.WriteFile(outFile, nil, 0o644); err != nil {
		t.Fatalf("truncate outFile: %v", err)
	}
	if got := m.LocalFile(v.ID); got != "" {
		t.Fatalf("LocalFile for empty file = %q, want empty", got)
	}

	// Removing file causes LocalFile to return "".
	if err := os.Remove(outFile); err != nil {
		t.Fatalf("remove outFile: %v", err)
	}
	if got := m.LocalFile(v.ID); got != "" {
		t.Fatalf("m.LocalFile after remove = %q, want empty", got)
	}
	if got := m2.LocalFile(v.ID); got != "" {
		t.Fatalf("m2.LocalFile after remove = %q, want empty", got)
	}
}

func TestRunWithProgressFailurePreservesTwoLineMeta(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script test")
	}

	stateDir := t.TempDir()
	dlDir := t.TempDir()
	fakeBin := filepath.Join(t.TempDir(), "yt-dlp")

	script := `#!/bin/sh
echo "ERROR: HTTP Error 403: Forbidden" >&2
exit 1
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake yt-dlp: %v", err)
	}

	m := NewManager(stateDir, dlDir, fakeBin)
	v := model.Video{ID: "err403", Title: "Blocked", Channel: "Chan"}

	err := m.Enqueue(context.Background(), v, false)
	if err == nil {
		t.Fatalf("Enqueue() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP Error 403") {
		t.Fatalf("Enqueue() error = %q, want HTTP Error 403", err.Error())
	}

	rawMeta, readErr := os.ReadFile(filepath.Join(stateDir, "downloads", v.ID+".meta"))
	if readErr != nil {
		t.Fatalf("read meta: %v", readErr)
	}
	wantMeta := "Blocked\nChan\n"
	if string(rawMeta) != wantMeta {
		t.Fatalf("meta content = %q, want %q", string(rawMeta), wantMeta)
	}
}

func TestMaxConcurrentBoundsConcurrency(t *testing.T) {
	stateDir := t.TempDir()
	dlDir := t.TempDir()

	var active int32
	var maxActive int32
	enteredCh := make(chan string, 3)
	releaseCh := make(chan struct{})

	// Construct directly via struct literal (MaxConcurrent == 0 -> defaults to 2).
	m := &Manager{
		StateDir:    stateDir,
		DownloadDir: dlDir,
		Runner: func(ctx context.Context, bin string, args ...string) error {
			cur := atomic.AddInt32(&active, 1)
			for {
				prev := atomic.LoadInt32(&maxActive)
				if cur <= prev || atomic.CompareAndSwapInt32(&maxActive, prev, cur) {
					break
				}
			}
			enteredCh <- args[len(args)-1]
			<-releaseCh
			atomic.AddInt32(&active, -1)
			return nil
		},
	}

	errCh := make(chan error, 3)
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("conc%d", i)
		go func(vid string) {
			errCh <- m.Enqueue(context.Background(), model.Video{
				ID:      vid,
				Title:   "Title " + vid,
				Channel: "Chan",
			}, false)
		}(id)
	}

	// Wait until 2 downloads have entered the runner.
	for i := 0; i < 2; i++ {
		select {
		case <-enteredCh:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for 2 concurrent downloads to enter runner")
		}
	}

	// Wait until all 3 items are queued (so the 3rd goroutine is waiting on semaphore).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items, err := m.List()
		if err != nil {
			t.Fatalf("List() error: %v", err)
		}
		if len(items) == 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	items, err := m.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items in queue, got %d", len(items))
	}

	// Give the 3rd goroutine a brief moment to prove it cannot bypass MaxConcurrent=2.
	select {
	case extra := <-enteredCh:
		t.Fatalf("3rd download %q entered runner while 2 already active", extra)
	case <-time.After(40 * time.Millisecond):
	}

	if got := atomic.LoadInt32(&active); got != 2 {
		t.Fatalf("active count = %d, want 2", got)
	}

	// Unblock all runners.
	close(releaseCh)

	select {
	case <-enteredCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for 3rd download to enter runner")
	}

	for i := 0; i < 3; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Enqueue() error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for Enqueue completion")
		}
	}

	if got := atomic.LoadInt32(&maxActive); got != 2 {
		t.Fatalf("maxActive = %d, want 2", got)
	}

	finalItems, err := m.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	for _, it := range finalItems {
		if it.Status != StatusDone || it.Progress != 100 {
			t.Fatalf("expected StatusDone/100, got %+v", it)
		}
	}
}

func TestCancelInFlightDownload(t *testing.T) {
	t.Run("cancel running subprocess", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX shell script test")
		}

		stateDir := t.TempDir()
		dlDir := t.TempDir()
		fakeBin := filepath.Join(t.TempDir(), "yt-dlp")

		script := `#!/bin/sh
printf 'IUTUI_PROG| 15.0%%|1.00MiB/s|00:30\n'
exec sleep 30
`
		if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake yt-dlp: %v", err)
		}

		startedCh := make(chan struct{}, 1)
		m := NewManager(stateDir, dlDir, fakeBin)
		m.OnProgress = func(it Item) {
			if it.Progress == 15.0 {
				select {
				case startedCh <- struct{}{}:
				default:
				}
			}
		}

		v := model.Video{ID: "subcancel1", Title: "Long Download", Channel: "Chan"}
		errCh := make(chan error, 1)
		go func() {
			errCh <- m.Enqueue(context.Background(), v, false)
		}()

		select {
		case <-startedCh:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for fake yt-dlp progress")
		}

		if m.Cancel("nonexistent") {
			t.Fatal("Cancel(nonexistent) = true, want false")
		}
		if !m.Cancel(v.ID) {
			t.Fatal("Cancel(active) = false, want true")
		}
		if m.Cancel(v.ID) {
			t.Fatal("second Cancel(active) = true, want false")
		}

		var err error
		select {
		case err = <-errCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for cancelled Enqueue to return")
		}

		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("Enqueue() error = %v, want context.Canceled", err)
		}

		items, listErr := m.List()
		if listErr != nil {
			t.Fatalf("List() error: %v", listErr)
		}
		if len(items) != 1 || items[0].Status != StatusFailed {
			t.Fatalf("List() = %+v, want 1 failed item", items)
		}
		if m.Cancel(v.ID) {
			t.Fatal("Cancel after exit = true, want false")
		}
	})

	t.Run("cancel pending item waiting on semaphore", func(t *testing.T) {
		stateDir := t.TempDir()
		dlDir := t.TempDir()

		entered := make(chan struct{})
		release := make(chan struct{})
		m := NewManager(stateDir, dlDir, "")
		m.MaxConcurrent = 1
		m.Runner = func(ctx context.Context, bin string, args ...string) error {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-release
			return nil
		}

		v1 := model.Video{ID: "hold1", Title: "Holder", Channel: "Chan"}
		v2 := model.Video{ID: "pend2", Title: "Pending", Channel: "Chan"}

		err1Ch := make(chan error, 1)
		go func() {
			err1Ch <- m.Enqueue(context.Background(), v1, false)
		}()
		<-entered

		pendingCh := make(chan struct{}, 1)
		m.OnProgress = func(it Item) {
			if it.Video.ID == v2.ID && it.Status == StatusPending {
				select {
				case pendingCh <- struct{}{}:
				default:
				}
			}
		}

		err2Ch := make(chan error, 1)
		go func() {
			err2Ch <- m.Enqueue(context.Background(), v2, false)
		}()

		select {
		case <-pendingCh:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for v2 StatusPending")
		}

		if !m.Cancel(v2.ID) {
			t.Fatal("Cancel(pending v2) = false, want true")
		}

		select {
		case err2 := <-err2Ch:
			if !errors.Is(err2, context.Canceled) {
				t.Fatalf("v2 Enqueue() error = %v, want context.Canceled", err2)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for cancelled pending download to exit")
		}

		close(release)
		if err1 := <-err1Ch; err1 != nil {
			t.Fatalf("v1 Enqueue() unexpected error: %v", err1)
		}
	})
}
