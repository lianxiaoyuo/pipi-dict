package freeonline

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFreeAPI(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		success bool
	}{
		{"good", `{"responseStatus":200,"responseData":{"translatedText":"词义"}}`, true},
		{"same", `{"responseStatus":200,"responseData":{"translatedText":"word"}}`, false},
		{"quota", `{"responseStatus":200,"quotaFinished":true}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("q") != "word" || r.URL.Query().Get("langpair") != "en|zh-CN" {
					t.Errorf("bad query: %s", r.URL.RawQuery)
				}
				w.Write([]byte(tc.payload))
			}))
			defer srv.Close()
			res, err := (Client{Endpoint: srv.URL}).Translate(context.Background(), "word")
			if (err == nil) != tc.success {
				t.Fatalf("got %s %v", res, err)
			}
			if tc.success && !strings.Contains(res, "词义") {
				t.Fatal(res)
			}
		})
	}
}
