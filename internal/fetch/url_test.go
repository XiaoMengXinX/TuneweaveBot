package fetch

import "testing"

func TestValidateURL(t *testing.T) {
	for _, tt := range []struct {
		url          string
		allowPrivate bool
		wantErr      bool
	}{
		{url: "https://8.8.8.8/a.mp3"},
		{url: "http://8.8.8.8:8080/a.mp3"},
		{url: "https://127.0.0.1/a.mp3", wantErr: true},
		{url: "https://10.0.0.1/a.mp3", wantErr: true},
		{url: "https://192.168.1.1/a.mp3", wantErr: true},
		{url: "https://169.254.1.1/a.mp3", wantErr: true},
		{url: "https://[::1]/a.mp3", wantErr: true},
		{url: "https://[fe80::1]/a.mp3", wantErr: true},
		{url: "ftp://8.8.8.8/a.mp3", wantErr: true},
		{url: "https:///a.mp3", wantErr: true},
		{url: "https://127.0.0.1/a.mp3", allowPrivate: true},
		{url: "https://192.168.1.1/a.mp3", allowPrivate: true},
	} {
		if err := ValidateURL(tt.url, tt.allowPrivate); (err != nil) != tt.wantErr {
			t.Errorf("ValidateURL(%q, %v) = %v, want error %v", tt.url, tt.allowPrivate, err, tt.wantErr)
		}
	}
}
