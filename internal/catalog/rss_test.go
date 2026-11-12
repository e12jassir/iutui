package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		name       string
		handler    http.HandlerFunc
		timeout    time.Duration
		wantErr    bool
		wantCount  int
		wantFirst  string
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
