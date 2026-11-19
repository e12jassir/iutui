package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestChannelFeedURL(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{
			name: "channel ID",
			ref:  "UC_x5XG1OV2P6uZZ5FSM9Ttw",
			want: "https://www.youtube.com/feeds/videos.xml?channel_id=UC_x5XG1OV2P6uZZ5FSM9Ttw",
		},
		{
			name: "channel ID with whitespace",
			ref:  "  UC_x5XG1OV2P6uZZ5FSM9Ttw \n",
			want: "https://www.youtube.com/feeds/videos.xml?channel_id=UC_x5XG1OV2P6uZZ5FSM9Ttw",
		},
		{
			name: "full channel URL",
			ref:  "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw",
			want: "https://www.youtube.com/feeds/videos.xml?channel_id=UC_x5XG1OV2P6uZZ5FSM9Ttw",
		},
		{
			name: "schemeless channel URL with subpath",
			ref:  "youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/videos?view=0",
			want: "https://www.youtube.com/feeds/videos.xml?channel_id=UC_x5XG1OV2P6uZZ5FSM9Ttw",
		},
		{
			name: "already a feed URL",
			ref:  "http://youtube.com/feeds/videos.xml?channel_id=UC_x5XG1OV2P6uZZ5FSM9Ttw",
			want: "https://www.youtube.com/feeds/videos.xml?channel_id=UC_x5XG1OV2P6uZZ5FSM9Ttw",
		},
		{
			name: "empty",
			ref:  "   ",
			want: "",
		},
		{
			name: "unsupported handle",
			ref:  "https://www.youtube.com/@GoogleDevelopers",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ChannelFeedURL(tc.ref)
			if got != tc.want {
				t.Fatalf("ChannelFeedURL(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

const sampleAtomFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/" xmlns="http://www.w3.org/2005/Atom">
  <title>Test Channel</title>
  <entry>
    <id>yt:video:older12345</id>
    <yt:videoId>older12345</yt:videoId>
    <title>Older Video</title>
    <author><name>Author One</name></author>
    <published>2024-01-10T08:00:00+00:00</published>
    <media:group>
      <media:thumbnail url="https://i.ytimg.com/vi/older12345/hqdefault.jpg" width="480" height="360"/>
      <media:community>
        <media:statistics views="450"/>
      </media:community>
    </media:group>
  </entry>
  <entry>
    <id>yt:video:newest9876</id>
    <yt:videoId>newest9876</yt:videoId>
    <title>Newest Video</title>
    <author><name>Author Two</name></author>
    <published>2024-05-20T15:30:00+00:00</published>
    <media:group>
      <media:thumbnail url="https://i.ytimg.com/vi/newest9876/hqdefault.jpg" width="480" height="360"/>
      <media:community>
        <media:statistics views="1500000"/>
      </media:community>
    </media:group>
  </entry>
  <entry>
    <id>yt:video:middle5555</id>
    <yt:videoId>middle5555</yt:videoId>
    <title>Middle Video</title>
    <author><name></name></author>
    <published>2024-03-01T12:00:00Z</published>
    <media:group>
      <media:community>
        <media:statistics views="2500"/>
      </media:community>
    </media:group>
  </entry>
  <entry>
    <yt:videoId></yt:videoId>
    <title>Missing ID Should Skip</title>
  </entry>
</feed>`

func TestParseAtomFeed(t *testing.T) {
	tests := []struct {
		name    string
		xmlData []byte
		wantErr bool
		wantIDs []string
	}{
		{
			name:    "valid feed sorted newest-first",
			xmlData: []byte(sampleAtomFeed),
			wantIDs: []string{"newest9876", "middle5555", "older12345"},
		},
		{
			name:    "empty input",
			xmlData: []byte("   "),
			wantErr: true,
		},
		{
			name:    "malformed xml",
			xmlData: []byte(`<feed><entry><title>unclosed`),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			videos, err := ParseAtomFeed(tc.xmlData)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(videos) != len(tc.wantIDs) {
				t.Fatalf("got %d videos, want %d", len(videos), len(tc.wantIDs))
			}
			for i, wantID := range tc.wantIDs {
				if videos[i].ID != wantID {
					t.Errorf("videos[%d].ID = %q, want %q", i, videos[i].ID, wantID)
				}
			}

			if videos[0].Title != "Newest Video" || videos[0].Channel != "Author Two" || videos[0].Views != "1.5M vistas" {
				t.Errorf("unexpected newest video fields: %+v", videos[0])
			}
			if videos[0].Thumbnail != "https://i.ytimg.com/vi/newest9876/hqdefault.jpg" {
				t.Errorf("unexpected thumbnail: %q", videos[0].Thumbnail)
			}
			if videos[1].Channel != "Test Channel" {
				t.Errorf("expected feed title fallback for empty author, got %q", videos[1].Channel)
			}
			if videos[2].Views != "450 vistas" {
				t.Errorf("unexpected views for older video: %q", videos[2].Views)
			}
		})
	}
}

func TestFetchRSSFeed(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		timeout   time.Duration
		wantErr   bool
		wantCount int
		wantFirst string
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/atom+xml")
				_, _ = w.Write([]byte(sampleAtomFeed))
			},
			timeout:   2 * time.Second,
			wantCount: 3,
			wantFirst: "newest9876",
		},
		{
			name: "http 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			},
			timeout: 2 * time.Second,
			wantErr: true,
		},
		{
			name: "invalid xml body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not xml"))
			},
			timeout: 2 * time.Second,
			wantErr: true,
		},
		{
			name: "context timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-time.After(300 * time.Millisecond):
					_, _ = w.Write([]byte(sampleAtomFeed))
				case <-r.Context().Done():
				}
			},
			timeout: 30 * time.Millisecond,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			videos, err := FetchRSSFeed(ctx, srv.Client(), srv.URL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(videos) != tc.wantCount {
				t.Fatalf("got %d videos, want %d", len(videos), tc.wantCount)
			}
			if videos[0].ID != tc.wantFirst {
				t.Errorf("first video ID = %q, want %q", videos[0].ID, tc.wantFirst)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newRSSRedirectClient(srv *httptest.Server) *http.Client {
	targetURL, err := url.Parse(srv.URL)
	if err != nil {
		panic(err)
	}
	base := srv.Client().Transport
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			cloned := req.Clone(req.Context())
			cloned.URL.Scheme = targetURL.Scheme
			cloned.URL.Host = targetURL.Host
			return base.RoundTrip(cloned)
		}),
	}
}

func TestFetchSubscriptionsRSS(t *testing.T) {
	const (
		chanOne  = "UCchanOne111111111111111"
		chanTwo  = "UCchanTwo222222222222222"
		chanFail = "UCchanFail33333333333333"
	)

	feedOne := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/" xmlns="http://www.w3.org/2005/Atom">
  <title>Channel One</title>
  <entry>
    <yt:videoId>vidOld00001</yt:videoId>
    <title>Old Video</title>
    <author><name>Channel One</name></author>
    <published>2024-02-01T10:00:00Z</published>
  </entry>
  <entry>
    <yt:videoId>vidOverlap1</yt:videoId>
    <title>Overlap Video</title>
    <author><name>Channel One</name></author>
    <published>2024-05-10T12:00:00Z</published>
    <media:group><media:community><media:statistics views="1200"/></media:community></media:group>
  </entry>
  <entry>
    <yt:videoId>vidNewest01</yt:videoId>
    <title>Newest Video</title>
    <author><name>Channel One</name></author>
    <published>2024-06-01T08:00:00Z</published>
  </entry>
</feed>`

	feedTwo := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/" xmlns="http://www.w3.org/2005/Atom">
  <title>Channel Two</title>
  <entry>
    <yt:videoId>vidOverlap1</yt:videoId>
    <title>Overlap Video</title>
    <author><name>Channel Two</name></author>
    <published>2024-05-10T12:00:00Z</published>
  </entry>
  <entry>
    <yt:videoId>vidMid00002</yt:videoId>
    <title>Middle Video</title>
    <author><name>Channel Two</name></author>
    <published>2024-04-15T09:30:00Z</published>
  </entry>
</feed>`

	t.Run("merges feeds, deduplicates IDs, sorts newest-first, tolerates failures, ignores non-UC refs", func(t *testing.T) {
		var (
			mu   sync.Mutex
			hits = make(map[string]int)
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cid := r.URL.Query().Get("channel_id")
			mu.Lock()
			hits[cid]++
			mu.Unlock()

			switch cid {
			case chanOne:
				_, _ = w.Write([]byte(feedOne))
			case chanTwo:
				_, _ = w.Write([]byte(feedTwo))
			case chanFail:
				http.Error(w, "upstream error", http.StatusInternalServerError)
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		refs := []string{
			chanOne,
			"https://www.youtube.com/channel/" + chanTwo,
			"https://www.youtube.com/channel/" + chanOne, // duplicate feed URL
			chanFail,
			"@PinkFloyd",
			"Soda Stereo",
			"   ",
		}

		videos, err := FetchSubscriptionsRSS(context.Background(), newRSSRedirectClient(srv), refs)
		if err != nil {
			t.Fatalf("FetchSubscriptionsRSS error: %v", err)
		}

		gotIDs := make([]string, len(videos))
		for i, v := range videos {
			gotIDs[i] = v.ID
		}
		wantIDs := []string{"vidNewest01", "vidOverlap1", "vidMid00002", "vidOld00001"}
		if !slices.Equal(gotIDs, wantIDs) {
			t.Fatalf("got IDs %v, want %v", gotIDs, wantIDs)
		}

		if videos[0].ChannelID != chanOne {
			t.Errorf("videos[0].ChannelID = %q, want %q", videos[0].ChannelID, chanOne)
		}
		if videos[2].ChannelID != chanTwo {
			t.Errorf("videos[2].ChannelID = %q, want %q", videos[2].ChannelID, chanTwo)
		}
		if videos[1].Views != "1.2K vistas" {
			t.Errorf("videos[1].Views = %q, want 1.2K vistas", videos[1].Views)
		}

		mu.Lock()
		defer mu.Unlock()
		if hits[chanOne] != 1 {
			t.Errorf("expected deduplicated channel one to be fetched once, got %d", hits[chanOne])
		}
		if hits[chanTwo] != 1 {
			t.Errorf("expected channel two to be fetched once, got %d", hits[chanTwo])
		}
		if hits[chanFail] != 1 {
			t.Errorf("expected failing channel to be attempted once, got %d", hits[chanFail])
		}
	})

	t.Run("returns empty slice without error when no refs resolve to RSS URL", func(t *testing.T) {
		videos, err := FetchSubscriptionsRSS(context.Background(), nil, []string{"", "@Handle", "Channel Name", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(videos) != 0 {
			t.Fatalf("expected 0 videos, got %+v", videos)
		}
	})

	t.Run("returns error when every resolved feed fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		}))
		defer srv.Close()

		_, err := FetchSubscriptionsRSS(context.Background(), newRSSRedirectClient(srv), []string{chanOne, chanTwo})
		if err == nil {
			t.Fatal("expected error when all feeds fail, got nil")
		}
	})

	t.Run("caps merged items at 100 newest-first", func(t *testing.T) {
		buildLargeFeed := func(prefix string, startHour int) string {
			out := `<?xml version="1.0" encoding="UTF-8"?><feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns="http://www.w3.org/2005/Atom"><title>Large</title>`
			base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < 60; i++ {
				ts := base.Add(time.Duration(startHour+i) * time.Hour).Format(time.RFC3339)
				out += fmt.Sprintf(`<entry><yt:videoId>%s%04d</yt:videoId><title>Video %s %d</title><published>%s</published></entry>`, prefix, i, prefix, i, ts)
			}
			return out + `</feed>`
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("channel_id") {
			case chanOne:
				_, _ = w.Write([]byte(buildLargeFeed("aa", 0)))
			case chanTwo:
				_, _ = w.Write([]byte(buildLargeFeed("bb", 60)))
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		videos, err := FetchSubscriptionsRSS(context.Background(), newRSSRedirectClient(srv), []string{chanOne, chanTwo})
		if err != nil {
			t.Fatalf("FetchSubscriptionsRSS error: %v", err)
		}
		if len(videos) != 100 {
			t.Fatalf("got %d videos, want cap of 100", len(videos))
		}
		if videos[0].ID != "bb0059" {
			t.Errorf("videos[0].ID = %q, want newest bb0059", videos[0].ID)
		}
		if videos[99].ID != "aa0020" {
			t.Errorf("videos[99].ID = %q, want aa0020", videos[99].ID)
		}
	})
}
