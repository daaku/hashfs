package hashfs_test

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/daaku/ensure"
	"github.com/daaku/hashfs"
)

const (
	unhashedMainJS = "assets/main.js"
	unhashedEmpty  = "assets/empty"
	hashedMainJS   = "assets/main.60797db6e8ff.js"
	hashedEmpty    = "assets/empty.e3b0c44298fc"
)

//go:embed assets/*
var assets embed.FS
var assetsH = hashfs.FileServer(assets)

func TestValidPath(t *testing.T) {
	ensure.DeepEqual(t, hashfs.Path(assets, unhashedMainJS), hashedMainJS)
	ensure.DeepEqual(t, hashfs.Path(assets, unhashedEmpty), hashedEmpty)
}

func TestInvalidPath(t *testing.T) {
	p, err := hashfs.MaybePath(assets, "foo")
	ensure.DeepEqual(t, p, "")
	ensure.Err(t, err, regexp.MustCompile("hashfs: error opening file"))
}

func TestPathPanic(t *testing.T) {
	defer func() {
		ensure.Err(t, recover().(error), regexp.MustCompile("hashfs: error opening file"))
	}()
	hashfs.Path(assets, "foo")
}

func TestValidRequest(t *testing.T) {
	cases := []struct {
		unhashed, hashed string
	}{
		{unhashedMainJS, hashedMainJS},
		{unhashedEmpty, hashedEmpty},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/"+c.hashed, nil)
		w := httptest.NewRecorder()
		assetsH.ServeHTTP(w, r)
		ensure.DeepEqual(t, w.Code, http.StatusOK)
		expected, err := assets.ReadFile(c.unhashed)
		ensure.Nil(t, err)
		if len(expected) == 0 {
			ensure.DeepEqual(t, len(w.Body.Bytes()), 0)
		} else {
			ensure.DeepEqual(t, w.Body.Bytes(), expected)
		}
	}
}

func TestInvalidRequest(t *testing.T) {
	cases := []struct {
		path, err string
	}{
		{"/assets/main.js", "hashfs: error opening file"},
		{"assets/main.000000000000.js", "hashfs: path mismatch for"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/"+c.path, nil)
		w := httptest.NewRecorder()
		assetsH.ServeHTTP(w, r)
		ensure.DeepEqual(t, w.Code, http.StatusBadRequest)
		ensure.StringContains(t, w.Body.String(), c.err)
	}
}
