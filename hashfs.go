// Package hashfs provides a hashing enabled `http.FileServerFS` replacement library.
package hashfs

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

type cacheKey struct {
	fs       fs.FS
	filename string
}

var (
	cachedHashes sync.Map
	cachedCSS    sync.Map
)

func setImmutable(h http.Header) {
	h.Set("cache-control", "public, immutable, max-age=31557600")
}

func isCSSFilename(filename string) bool {
	return path.Ext(filename) == ".css"
}

// FileServer returns a handler that serves HTTP requests with the contents
// of the file system rooted at root. It will expect the requests to contain
// hashed paths. Use http.StripPrefix to wrap and remove any prefixes
// if necessary.
func FileServer(fs fs.FS) http.Handler {
	hfs := http.FileServerFS(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filename, err := Unhashed(fs, strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			http.Error(w, fmt.Sprint(err), http.StatusBadRequest)
			return
		}

		if isCSSFilename(filename) {
			content, err := hashCSSAssets(fs, filename)
			if err == nil {
				setImmutable(w.Header())
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
				io.WriteString(w, content)
				return
			}
		}

		r = r.Clone(r.Context())
		r.URL.Path = "/" + filename
		if r.URL.RawPath != "" {
			rawpath, err := Unhashed(fs, strings.TrimPrefix(r.URL.RawPath, "/"))
			if err != nil {
				http.Error(w, fmt.Sprint(err), http.StatusBadRequest)
				return
			}
			r.URL.Path = "/" + rawpath
		}

		setImmutable(w.Header())
		hfs.ServeHTTP(w, r)
	})
}

func hashCSSAssets(fs fs.FS, filename string) (string, error) {
	key := cacheKey{
		fs:       fs,
		filename: filename,
	}
	content, found := cachedCSS.Load(key)
	if found {
		return content.(string), nil
	}

	f, err := fs.Open(filename)
	if err != nil {
		return "", fmt.Errorf("hashfs: unexpected error opening css file %q: %w", filename, err)
	}

	var out strings.Builder
	l := css.NewLexer(parse.NewInput(f))
outer:
	for {
		tt, text := l.Next()
		switch tt {
		default:
			out.Write(text)
		case css.AtKeywordToken:
			out.Write(text)
			if bytes.EqualFold(text, []byte("@import")) {
				for {
					tt, text := l.Next()
					switch tt {
					default:
						out.Write(text)
						continue outer
					case css.ErrorToken:
						out.Write(text)
						if errors.Is(l.Err(), io.EOF) {
							break outer
						}
					case css.WhitespaceToken:
						out.Write(text)
					case css.StringToken:
						target := string(text[1 : len(text)-1])
						hashed := transformPath(fs, filename, target)
						out.WriteByte(text[0])
						out.WriteString(hashed)
						out.WriteByte(text[0])
						continue outer
					}
				}
			}
		case css.URLToken:
			out.Write(transformURL(fs, filename, text))
		case css.ErrorToken:
			out.Write(text)
			if errors.Is(l.Err(), io.EOF) {
				break outer
			}
		}
	}

	outStr := out.String()
	cachedCSS.Store(key, outStr)
	return outStr, nil
}

var (
	urlBarePre    = []byte(`url(`)
	urlBarePost   = []byte(`)`)
	urlSinglePre  = []byte(`url('`)
	urlSinglePost = []byte(`')`)
	urlDobulePre  = []byte(`url("`)
	urlDobulePost = []byte(`")`)
)

func transformPath(fs fs.FS, basepath string, target string) string {
	abs := path.Join(path.Dir(basepath), target)
	hashed, err := MaybePath(fs, abs)
	if err != nil {
		return target
	}
	return path.Join(path.Dir(target), path.Base(hashed))
}

func transformURL(fs fs.FS, basepath string, v []byte) []byte {
	pre := urlBarePre
	post := urlBarePost
	if bytes.HasPrefix(v, urlDobulePre) {
		pre = urlDobulePre
		post = urlDobulePost
	} else if bytes.HasPrefix(v, urlSinglePre) {
		pre = urlSinglePre
		post = urlSinglePost
	}

	target := string(v[len(pre) : len(v)-len(post)])
	hashed := transformPath(fs, basepath, target)
	return slices.Concat(pre, []byte(hashed), post)
}

// Path returns the hashed path of filename. It panics if the filename is not
// found or other errors. Use MaybePath for errors instead of panics.
func Path(fs fs.FS, filename string) string {
	hashed, err := MaybePath(fs, filename)
	if err != nil {
		panic(err)
	}
	return hashed
}

// MaybePath returns the hashed path of filename. The hash is injected before
// the extension, or at the end if an extension is not found.
func MaybePath(fs fs.FS, filename string) (string, error) {
	key := cacheKey{
		fs:       fs,
		filename: filename,
	}
	urlpath, found := cachedHashes.Load(key)
	if found {
		return urlpath.(string), nil
	}

	var r io.Reader
	if isCSSFilename(filename) {
		content, err := hashCSSAssets(fs, filename)
		if err == nil {
			r = strings.NewReader(content)
		}
	}
	if r == nil {
		f, err := fs.Open(filename)
		if err != nil {
			return "", fmt.Errorf("hashfs: error opening file: %w", err)
		}
		defer f.Close()
		r = f
	}

	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	ext := filepath.Ext(filename)
	hashBytes := h.Sum(nil)
	newP := fmt.Sprintf("%s.%x%s", filename[0:len(filename)-len(ext)], hashBytes[:6], ext)
	cachedHashes.Store(key, newP)
	return newP, nil
}

// Unhashed returns the original unhashed filename from a hashed path.
// It will ensure the hash matches.
func Unhashed(fs fs.FS, urlpath string) (string, error) {
	urlpathL := len(urlpath)
	ext := filepath.Ext(urlpath)
	extL := len(ext)
	hash := filepath.Ext(urlpath[0 : urlpathL-extL])
	if hash == "" { // assume extensionless, use ext as hash
		hash = ext
		ext = ""
		extL = 0
	}
	filename := urlpath[0:urlpathL-extL-len(hash)] + ext
	expectedPath, err := MaybePath(fs, filename)
	if err != nil {
		return "", err
	}
	if expectedPath != urlpath {
		return "", fmt.Errorf("hashfs: path mismatch for %q", urlpath)
	}
	return filename, nil
}
