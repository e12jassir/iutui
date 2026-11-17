//go:build darwin

package platform

import "testing"

func TestBuildDarwinNotificationScript(t *testing.T) {
	tests := []struct {
		name  string
		title string
		body  string
		want  string
	}{
		{
			name:  "plain text",
			title: "ytx",
			body:  "Now playing",
			want:  `display notification "Now playing" with title "ytx"`,
		},
		{
			name:  "escapes quotes and backslashes",
			title: `Say "Hi" \ Admin`,
			body:  `Path C:\ytx\"clip"`,
			want:  `display notification "Path C:\\ytx\\\"clip\"" with title "Say \"Hi\" \\ Admin"`,
		},
		{
			name:  "escapes newlines",
			title: "Line1\nLine2",
			body:  "Body\r\nNext",
			want:  `display notification "Body\r\nNext" with title "Line1\nLine2"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildDarwinNotificationScript(tc.title, tc.body); got != tc.want {
				t.Fatalf("buildDarwinNotificationScript(%q, %q) = %q, want %q", tc.title, tc.body, got, tc.want)
			}
		})
	}
}
