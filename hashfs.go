// Package hashfs provides a hashing enabled `http.FileServerFS` replacement library.
package hashfs

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

type cacheKey struct {
	fs       fs.FS
	filename string
}

var cachedHashes sync.Map

// FileServer returns a handler that serves HTTP requests with the contents
// of the file system rooted at root. It will expect the requests to contain
// hashed paths. Use http.StripPrefix to wrap and remove any prefixes
// if necessary.
func FileServer(fs fs.FS) http.Handler {
	hfs := http.FileServerFS(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.Clone(r.Context())

		filepath, err := Unhashed(fs, strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			http.Error(w, fmt.Sprint(err), http.StatusBadRequest)
			return
		}
		r.URL.Path = "/" + filepath

		if r.URL.RawPath != "" {
			rawpath, err := Unhashed(fs, strings.TrimPrefix(r.URL.RawPath, "/"))
			if err != nil {
				http.Error(w, fmt.Sprint(err), http.StatusBadRequest)
				return
			}
			r.URL.Path = "/" + rawpath
		}

		hfs.ServeHTTP(w, r)
	})
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

	f, err := fs.Open(filename)
	if err != nil {
		return "", fmt.Errorf("hashfs: error opening file: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
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
