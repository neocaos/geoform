package geoserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neocaos/geoform/internal/geoserver"
)

func TestListWorkspaces(t *testing.T) {
	for name, body := range map[string]string{
		"varios": `{"workspaces":{"workspace":[{"name":"a"},{"name":"b"}]}}`,
		"uno":    `{"workspaces":{"workspace":{"name":"a"}}}`,
		"vacio":  `{"workspaces":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/geoserver/rest/workspaces.json" {
					t.Errorf("ruta %s", r.URL.Path)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			got, err := geoserver.NewClient(srv.URL+"/geoserver", "u", "p").ListWorkspaces(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]int{"varios": 2, "uno": 1, "vacio": 0}[name]
			if len(got) != want {
				t.Errorf("got %v", got)
			}
		})
	}
}
