package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/e12jassir/iutui/internal/model"
)

const (
	defaultInnerTubePlayerURL = "https://www.youtube.com/youtubei/v1/player?prettyPrint=false"
	defaultMetaTTL            = 6 * time.Hour
	defaultMetaMaxBatch       = 24
	defaultMetaWorkers        = 6
	metaCacheFileName         = "video_meta.json"
)

// VideoMeta holds enriched view count and publish date metadata for a single video.
type VideoMeta struct {
	Views     string `json:"views,omitempty"`
	Published string `json:"published,omitempty"`
}

type cachedVideoMeta struct {
	Views     string    `json:"views,omitempty"`
	Published string    `json:"published,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

// MetadataEnricher fetches and caches per-video view counts and publish dates
// using YouTube's public InnerTube player endpoint when flat playlist feeds omit them.
type MetadataEnricher struct {
	CacheDir    string
	EndpointURL string
	HTTPClient  *http.Client
	TTL         time.Duration
	MaxBatch    int
	Workers     int

	mu      sync.Mutex
	writeMu sync.Mutex
	loaded  bool
	cache   map[string]cachedVideoMeta
	nowFn   func() time.Time
}

// NewMetadataEnricher creates a MetadataEnricher backed by cacheDir/video_meta.json.
func NewMetadataEnricher(cacheDir string) *MetadataEnricher {
	return &MetadataEnricher{
		CacheDir:    cacheDir,
		EndpointURL: defaultInnerTubePlayerURL,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
		TTL:         defaultMetaTTL,
		MaxBatch:    defaultMetaMaxBatch,
		Workers:     defaultMetaWorkers,
		cache:       make(map[string]cachedVideoMeta),
		nowFn:       time.Now,
	}
}

func (m *MetadataEnricher) now() time.Time {
	if m != nil && m.nowFn != nil {
		return m.nowFn()
	}
	return time.Now()
}

func (m *MetadataEnricher) ttl() time.Duration {
	if m != nil && m.TTL > 0 {
		return m.TTL
	}
	return defaultMetaTTL
}

func (m *MetadataEnricher) ensureLoadedLocked() {
	if m.loaded {
		return
	}
	m.loaded = true
	if m.cache == nil {
		m.cache = make(map[string]cachedVideoMeta)
	}
	if strings.TrimSpace(m.CacheDir) == "" {
		return
	}
	path := filepath.Join(m.CacheDir, metaCacheFileName)
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return
	}
	var disk map[string]cachedVideoMeta
	if err := json.Unmarshal(data, &disk); err != nil {
		return
	}
	now := m.now()
	ttl := m.ttl()
	for id, entry := range disk {
		if entry.Views == "" && entry.Published == "" {
			continue
		}
		if now.Sub(entry.FetchedAt) <= ttl {
			m.cache[id] = entry
		}
	}
}

// Cached returns cached metadata for id if present and within TTL.
func (m *MetadataEnricher) Cached(id string) (VideoMeta, bool) {
	if m == nil {
		return VideoMeta{}, false
	}
	id = strings.TrimSpace(id)
	if !isSingleYouTubeID(id) {
		return VideoMeta{}, false
	}
	now := m.now()
	ttl := m.ttl()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureLoadedLocked()
	entry, ok := m.cache[id]
	if !ok || now.Sub(entry.FetchedAt) > ttl {
		return VideoMeta{}, false
	}
	if entry.Views == "" && entry.Published == "" {
		return VideoMeta{}, false
	}
	return VideoMeta{Views: entry.Views, Published: entry.Published}, true
}

// ApplyCached fills missing Views and Published fields in videos from the local cache.
func (m *MetadataEnricher) ApplyCached(videos []model.Video) {
	if m == nil || len(videos) == 0 {
		return
	}
	now := m.now()
	ttl := m.ttl()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureLoadedLocked()

	for i := range videos {
		v := &videos[i]
		if v.Views != "" && v.Published != "" {
			continue
		}
		if !isSingleYouTubeID(v.ID) {
			continue
		}
		entry, ok := m.cache[v.ID]
		if !ok || now.Sub(entry.FetchedAt) > ttl {
			continue
		}
		if v.Views == "" && entry.Views != "" {
			v.Views = entry.Views
		}
		if v.Published == "" && entry.Published != "" {
			v.Published = entry.Published
		}
	}
}

// Enrich returns Views and Published metadata for ids, serving valid cached
// entries immediately and fetching uncached IDs concurrently via InnerTube.
func (m *MetadataEnricher) Enrich(ctx context.Context, ids []string) (map[string]VideoMeta, error) {
	if m == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	maxBatch := m.MaxBatch
	if maxBatch <= 0 {
		maxBatch = defaultMetaMaxBatch
	}
	workers := m.Workers
	if workers <= 0 {
		workers = defaultMetaWorkers
	}

	now := m.now()
	ttl := m.ttl()

	result := make(map[string]VideoMeta)
	var toFetch []string
	seen := make(map[string]struct{}, len(ids))

	m.mu.Lock()
	m.ensureLoadedLocked()
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if !isSingleYouTubeID(id) {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}

		if entry, ok := m.cache[id]; ok && now.Sub(entry.FetchedAt) <= ttl && (entry.Views != "" || entry.Published != "") {
			result[id] = VideoMeta{Views: entry.Views, Published: entry.Published}
			continue
		}
		if len(toFetch) < maxBatch {
			toFetch = append(toFetch, id)
		}
	}
	m.mu.Unlock()

	if len(toFetch) == 0 {
		return result, nil
	}

	if workers > len(toFetch) {
		workers = len(toFetch)
	}

	jobs := make(chan string, len(toFetch))
	for _, id := range toFetch {
		jobs <- id
	}
	close(jobs)

	var (
		fetchedMu sync.Mutex
		fetched   = make(map[string]VideoMeta, len(toFetch))
		wg        sync.WaitGroup
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if ctx.Err() != nil {
					return
				}
				if meta, ok := m.fetchSingle(ctx, id); ok {
					fetchedMu.Lock()
					fetched[id] = meta
					fetchedMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if len(fetched) > 0 {
		fetchedAt := m.now()
		m.mu.Lock()
		m.ensureLoadedLocked()
		for id, meta := range fetched {
			m.cache[id] = cachedVideoMeta{
				Views:     meta.Views,
				Published: meta.Published,
				FetchedAt: fetchedAt,
			}
			result[id] = meta
		}
		m.mu.Unlock()

		m.persistToDisk()
	}

	if ctxErr := ctx.Err(); ctxErr != nil && len(result) == 0 {
		return nil, ctxErr
	}
	return result, nil
}

func (m *MetadataEnricher) fetchSingle(ctx context.Context, videoID string) (VideoMeta, bool) {
	endpoint := strings.TrimSpace(m.EndpointURL)
	if endpoint == "" {
		endpoint = defaultInnerTubePlayerURL
	}
	client := m.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	reqBody, err := json.Marshal(struct {
		Context struct {
			Client struct {
				ClientName    string `json:"clientName"`
				ClientVersion string `json:"clientVersion"`
				HL            string `json:"hl"`
				GL            string `json:"gl"`
			} `json:"client"`
		} `json:"context"`
		VideoID string `json:"videoId"`
	}{
		VideoID: videoID,
	})
	if err != nil {
		return VideoMeta{}, false
	}

	var parsedReq map[string]any
	if err := json.Unmarshal(reqBody, &parsedReq); err == nil {
		clientMap := parsedReq["context"].(map[string]any)["client"].(map[string]any)
		clientMap["clientName"] = "WEB"
		clientMap["clientVersion"] = "2.20250101.00.00"
		clientMap["hl"] = "es"
		clientMap["gl"] = "CO"
		if rebuilt, merr := json.Marshal(parsedReq); merr == nil {
			reqBody = rebuilt
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return VideoMeta{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return VideoMeta{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return VideoMeta{}, false
	}

	const maxBodyBytes = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return VideoMeta{}, false
	}

	var payload struct {
		VideoDetails struct {
			ViewCount string `json:"viewCount"`
		} `json:"videoDetails"`
		Microformat struct {
			PlayerMicroformatRenderer struct {
				PublishDate string `json:"publishDate"`
				UploadDate  string `json:"uploadDate"`
			} `json:"playerMicroformatRenderer"`
		} `json:"microformat"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return VideoMeta{}, false
	}

	var meta VideoMeta
	if rawViews := strings.TrimSpace(payload.VideoDetails.ViewCount); rawViews != "" {
		if count, perr := strconv.ParseInt(rawViews, 10, 64); perr == nil && count > 0 {
			meta.Views = formatHumanViews(rawViews)
		}
	}
	pub := strings.TrimSpace(payload.Microformat.PlayerMicroformatRenderer.PublishDate)
	if pub == "" {
		pub = strings.TrimSpace(payload.Microformat.PlayerMicroformatRenderer.UploadDate)
	}
	meta.Published = pub

	if meta.Views == "" && meta.Published == "" {
		return VideoMeta{}, false
	}
	return meta, true
}

func (m *MetadataEnricher) persistToDisk() {
	if m == nil || strings.TrimSpace(m.CacheDir) == "" {
		return
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	now := m.now()
	ttl := m.ttl()

	m.mu.Lock()
	snapshot := make(map[string]cachedVideoMeta, len(m.cache))
	for id, entry := range m.cache {
		if now.Sub(entry.FetchedAt) > ttl {
			delete(m.cache, id)
			continue
		}
		if entry.Views == "" && entry.Published == "" {
			continue
		}
		snapshot[id] = entry
	}
	m.mu.Unlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	if err := os.MkdirAll(m.CacheDir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(m.CacheDir, ".video-meta-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	_ = os.Rename(tmpName, filepath.Join(m.CacheDir, metaCacheFileName))
}

func isSingleYouTubeID(id string) bool {
	if len(id) != 11 || model.IsPlaylistID(id) {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlnum && c != '_' && c != '-' {
			return false
		}
	}
	return true
}
