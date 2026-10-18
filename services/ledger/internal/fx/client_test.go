package fx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func rateServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rates" || r.URL.Query().Get("from") != "USD" || r.URL.Query().Get("to") != "NGN" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func rateBody(rate string, fetchedAt time.Time) string {
	return fmt.Sprintf(`{"from":"USD","to":"NGN","rate":%q,"source":"static","fetched_at":%q}`,
		rate, fetchedAt.UTC().Format(time.RFC3339))
}

func TestClientRate(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"ok", 200, rateBody("1500.000000000000", now), nil},
		{"unknown pair", 404, `{"error":"rate not found"}`, ErrRateUnavailable},
		{"server error", 500, `oops`, ErrRateServiceDown},
		{"stale rate", 200, rateBody("1500", now.Add(-time.Hour)), ErrRateStale},
		{"bad json", 200, `not json`, ErrRateServiceDown},
		{"zero rate", 200, rateBody("0", now), ErrRateServiceDown},
		{"non numeric rate", 200, rateBody("abc", now), ErrRateServiceDown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := rateServer(t, c.status, c.body)
			q, err := NewClient(srv.URL+"/", 10*time.Minute).Rate(context.Background(), "USD", "NGN")
			if c.wantErr == nil {
				if err != nil {
					t.Fatal(err)
				}
				if q.Rate.String() != "1500" || q.Source != "static" {
					t.Fatalf("unexpected quote: %+v", q)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("got %v, want %v", err, c.wantErr)
			}
		})
	}
}

func TestClientRate_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	_, err := NewClient(url, time.Minute).Rate(context.Background(), "USD", "NGN")
	if !errors.Is(err, ErrRateServiceDown) {
		t.Fatalf("got %v, want ErrRateServiceDown", err)
	}
}