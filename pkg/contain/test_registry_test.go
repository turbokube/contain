package contain_test

import (
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestTestRegistry(t *testing.T) {
	resp, err := http.Get(fmt.Sprintf("http://%s/v2/", testRegistry))
	if err != nil {
		t.Error(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
	}
	if string(body[:]) != "{}" {
		t.Errorf("unexpected response at /v2/: %s", body)
	}
}
