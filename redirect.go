// Copyright 2014 Google Inc. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd

package gum

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
)

// RedirectHandler redirects requests that match a given path component prefix
// to a specified destination base URL.  For example, given a RedirectHandler:
//
//     RedirectHandler{
//         Prefix: "x",
//         Destination: "http://example/",
//     }
//
// The following URLs would be redirected:
//
//     /x          =>  http://example/
//     /x/         =>  http://example/
//     /x/a/b?c=d  =>  http://example/a/b?c=d
//
// The request URL "/x123" would not be handled by this handler.
type RedirectHandler struct {
	// Prefix is the path component prefix this handler should handle.
	// Prefix should not contain leading or trailing slashes.
	Prefix string

	// Destination is the base URL to redirect requests to.  If Destination
	// includes a path, it should typically include a trailing slash.
	Destination *url.URL

	// Status is the HTTP status to return in redirect responses.
	Status int
}

// NewRedirectHandler constructs a new RedirectHandler with the specified
// prefix and destination, and a 301 (Moved Permanently) response status.
func NewRedirectHandler(prefix, destination string) (*RedirectHandler, error) {
	h := &RedirectHandler{
		Prefix: prefix,
		Status: http.StatusMovedPermanently,
	}

	var err error
	h.Destination, err = url.Parse(destination)
	if err != nil {
		return nil, err
	}

	return h, nil
}

func (h *RedirectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// drop scheme and host to ensure URL is relative
	r.URL.Scheme = ""
	r.URL.Host = ""

	// trim path prefix
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/"+h.Prefix)
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/")

	dest := h.Destination.ResolveReference(r.URL)
	http.Redirect(w, r, dest.String(), h.Status)
}

// Register this handler with the provided Router.
func (h *RedirectHandler) Register(router *mux.Router) {
	router.Handle("/"+h.Prefix, h)
	router.PathPrefix("/" + h.Prefix + "/").Handler(h)
}
