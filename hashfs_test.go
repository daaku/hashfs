package hashfs

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/daaku/ensure"
)

const (
	unhashedMainJS = "assets/main.js"
	unhashedEmpty  = "assets/empty"
	hashedMainJS   = "assets/main.60797db6e8ff.js"
	hashedEmpty    = "assets/empty.e3b0c44298fc"
)

//go:embed assets/*
var assets embed.FS
var assetsH = FileServer(assets)

func TestValidPath(t *testing.T) {
	ensure.DeepEqual(t, Path(assets, unhashedMainJS), hashedMainJS)
	ensure.DeepEqual(t, Path(assets, unhashedEmpty), hashedEmpty)
}

func TestInvalidPath(t *testing.T) {
	p, err := MaybePath(assets, "foo")
	ensure.DeepEqual(t, p, "")
	ensure.Err(t, err, regexp.MustCompile("hashfs: error opening file"))
}

func TestPathPanic(t *testing.T) {
	defer func() {
		ensure.Err(t, recover().(error), regexp.MustCompile("hashfs: error opening file"))
	}()
	Path(assets, "foo")
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

func TestHashCSSAsset(t *testing.T) {
	out, err := hashCSSAssets(assets, "assets/main.css")
	ensure.Nil(t, err)
	ensure.DeepEqual(t, out,
		`@font-face {
  font-family: "Foo";
  src:
    url(foo.b5bb9d8014a0) format("woff2"),
    url('bar.7d865e959b24.txt') format("woff"),
    url('missing') format("woff"),
    url("fonts/baz.bf07a7fbb825.txt") format("truetype");
}

@import "boom.8d7a531d714c.css";
`)
}

func TestHashCSSAssetSub(t *testing.T) {
	out, err := hashCSSAssets(assets, "assets/sub/main.css")
	ensure.Nil(t, err)
	ensure.DeepEqual(t, out, `@import "../boom.8d7a531d714c.css";
`)
}

func TestHashCSSRequest(t *testing.T) {
	const unhashed = "assets/main.css"
	const hashed = "assets/main.3b8e3d604b9f.css"
	r := httptest.NewRequest("GET", "/"+hashed, nil)
	w := httptest.NewRecorder()
	assetsH.ServeHTTP(w, r)
	ensure.DeepEqual(t, w.Code, http.StatusOK)
}
