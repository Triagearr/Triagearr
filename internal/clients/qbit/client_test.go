package qbit_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/qbit"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func newServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestListTorrents_NoAuthBypass(t *testing.T) {
	srv := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"hash":"abc","name":"Foo","category":"tv","save_path":"/dl","size":100,
				 "added_on":1700000000,"ratio":2.5,"uploaded":250,"num_complete":12,
				 "num_incomplete":3,"state":"uploading","last_activity":1700001000,
				 "private":true,"tags":"hd,french"},
				{"hash":"def","name":"Bar","category":"movies","save_path":"/dl","size":50,
				 "added_on":1700000000,"ratio":0.4,"uploaded":20,"num_complete":0,
				 "num_incomplete":0,"state":"stalledUP","last_activity":1700001000,
				 "private":false,"tags":""}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))

	c, err := qbit.New(qbit.Options{BaseURL: srv.URL})
	require.NoError(t, err)
	tors, err := c.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Len(t, tors, 2)
	require.Equal(t, "abc", string(tors[0].Hash))
	require.Equal(t, 12, tors[0].Seeders)
	require.Equal(t, 3, tors[0].Leechers)
	require.InDelta(t, 2.5, tors[0].Ratio, 1e-9)
	require.True(t, tors[0].Private)
	require.Equal(t, "hd,french", tors[0].Tags)
	require.False(t, tors[1].Private)
	require.Empty(t, tors[1].Tags)
}

func TestLogin_HappyPath(t *testing.T) {
	var loginCount int
	srv := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginCount++
			_ = r.ParseForm()
			require.Equal(t, "admin", r.FormValue("username"))
			require.Equal(t, "secret", r.FormValue("password"))
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fake", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if _, err := r.Cookie("SID"); err != nil {
				http.Error(w, "no cookie", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))

	c, err := qbit.New(qbit.Options{BaseURL: srv.URL, Username: "admin", Password: "secret"})
	require.NoError(t, err)
	_, err = c.ListTorrents(context.Background())
	require.NoError(t, err)
	_, err = c.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, loginCount, "login should be cached across calls")
}

func TestLogin_Failure(t *testing.T) {
	srv := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("Fails."))
	}))
	c, err := qbit.New(qbit.Options{BaseURL: srv.URL, Username: "x", Password: "y"})
	require.NoError(t, err)
	_, err = c.ListTorrents(context.Background())
	require.Error(t, err)
}

func TestTorrentFiles(t *testing.T) {
	srv := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v2/torrents/files", r.URL.Path)
		require.Equal(t, "abc", r.URL.Query().Get("hash"))
		_, _ = w.Write([]byte(`[{"name":"Foo.mkv","size":42,"progress":1.0}]`))
	}))
	c, err := qbit.New(qbit.Options{BaseURL: srv.URL})
	require.NoError(t, err)
	files, err := c.TorrentFiles(context.Background(), "abc")
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "Foo.mkv", files[0].Name)
}

func TestDelete_DeleteFilesTrue(t *testing.T) {
	var seen struct {
		method, path, hashes, deleteFiles string
	}
	srv := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method = r.Method
		seen.path = r.URL.Path
		_ = r.ParseForm()
		seen.hashes = r.FormValue("hashes")
		seen.deleteFiles = r.FormValue("deleteFiles")
		w.WriteHeader(http.StatusOK)
	}))
	c, err := qbit.New(qbit.Options{BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, c.Delete(context.Background(), "abc", triagearr.DeleteOpts{DeleteFiles: true}))
	require.Equal(t, http.MethodPost, seen.method)
	require.Equal(t, "/api/v2/torrents/delete", seen.path)
	require.Equal(t, "abc", seen.hashes)
	require.Equal(t, "true", seen.deleteFiles)
}

func TestDelete_5xx_Transient(t *testing.T) {
	srv := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	c, err := qbit.New(qbit.Options{BaseURL: srv.URL})
	require.NoError(t, err)
	err = c.Delete(context.Background(), "abc", triagearr.DeleteOpts{DeleteFiles: true})
	require.Error(t, err)
	require.True(t, errors.Is(err, triagearr.ErrTransient))
}
