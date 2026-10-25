package catalogapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthDoesNotWaitForLookupLock(t *testing.T) {
	s, _, _, _ := lookupFixture(t)
	s.mu.Lock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- lookupRequest(s, "GET", "/v1/health", strings.Repeat("s", 32), nil) }()
	select {
	case w := <-done:
		s.mu.Unlock()
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"catalog"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	case <-time.After(2 * time.Second):
		s.mu.Unlock()
		<-done
		t.Fatal("health queued behind lookup")
	}
}
