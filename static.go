// Copyright 2014 Google Inc. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd

package gum

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	fsnotify "gopkg.in/fsnotify.v1"
)

const (
	relShortlink = "shortlink"
	relCanonical = "canonical"
	attrAltHref  = "data-alt-href"
)

// StaticHandler handles short URLs parsed from static HTML files.  Files are
// parsed and searched for rel="shortlink" and rel="canonical" links.  If both
// are found, a redirect is registered for the pair.
type StaticHandler struct {
	base    string
	watcher *fsnotify.Watcher
}

// NewStaticHandler constructs a new StaticHandler with the specified base path
// of HTML files.
func NewStaticHandler(base string) (*StaticHandler, error) {
	if stat, err := os.Stat(base); err != nil {
		return nil, err
	} else if !stat.IsDir() {
		return nil, fmt.Errorf("Specified base path %q is not a directory", base)
	}

	return &StaticHandler{base: base}, nil
}

// Mappings implements Handler.
func (h *StaticHandler) Mappings(mappings chan<- Mapping) {
	loadFiles(h.base, mappings)

	var err error
	h.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		glog.Errorf("error creating file watcher: %v", err)
		return
	}

	go func() {
		for {
			select {
			case ev := <-h.watcher.Events:
				if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					// ignore Remove and Rename events
					continue
				}

				stat, err := os.Stat(ev.Name)
				if err != nil {
					glog.Errorf("Error reading file stats for %q: %v", ev.Name, err)
				}

				// add watcher for newly created directories
				if ev.Op&fsnotify.Create == fsnotify.Create && stat.IsDir() {
					h.watcher.Add(ev.Name)
				}

				// if event is Create or Write, reload files
				if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
					loadFiles(ev.Name, mappings)
				}
			case err := <-h.watcher.Errors:
				glog.Errorf("Watcher error: %v", err)
			}
		}
	}()

	// setup initial file watchers for h.base and all sub-directories
	err = filepath.Walk(h.base, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			err = h.watcher.Add(path)
			if err != nil {
				glog.Errorf("error watching path %q: %v", path, err)
			}
		}
		return nil
	})
	if err != nil {
		glog.Errorf("error setting up watchers for %q: %v", h.base, err)
	}
}

// Register is a noop for this handler.
func (h *StaticHandler) Register(mux *http.ServeMux) {}

func loadFiles(base string, mappings chan<- Mapping) {
	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			glog.Errorf("error reading file %q: %v", path, err)
			return nil
		}
		if info.IsDir() || filepath.Ext(path) != ".html" {
			// skip directories and non-HTML files
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			glog.Errorf("error opening file %q: %v", path, err)
			return nil
		}
		defer f.Close()

		fileMappings, err := parseFile(f)
		if err != nil {
			glog.Errorf("error parsing file %q: %v", path, err)
			return nil
		}

		for _, m := range fileMappings {
			mappings <- m
		}
		return nil
	}

	err := filepath.Walk(base, walkFn)
	if err != nil {
		glog.Errorf("Walk(%q) returned error: %v", base, err)
	}
}

// parseFile parses r as HTML and returns the URLs of the first links found
// with the "shortlink" and "canonical" rel values.
func parseFile(r io.Reader) (mappings []Mapping, err error) {
	var permalink string
	var shortlinks []string

	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.DataAtom == atom.Link || n.DataAtom == atom.A {
				var href, rel, altHref string
				for _, a := range n.Attr {
					if a.Key == atom.Href.String() {
						href = a.Val
					}
					if a.Key == atom.Rel.String() {
						rel = a.Val
					}
					if a.Key == attrAltHref {
						altHref = a.Val
					}
				}
				if href != "" && rel != "" {
					for _, v := range strings.Split(rel, " ") {
						if v == relShortlink {
							shortlinks = append(shortlinks, href)
							shortlinks = append(shortlinks, strings.Split(altHref, " ")...)
						}
						if v == relCanonical && permalink == "" {
							permalink = href
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}

	f(doc)

	if len(shortlinks) > 0 && permalink != "" {
		for _, link := range shortlinks {
			shorturl, err := url.Parse(link)
			if err != nil {
				glog.Errorf("error parsing shortlink %q: %v", link, err)
			}
			if path := shorturl.Path; len(path) > 1 {
				mappings = append(mappings, Mapping{ShortPath: path, Permalink: permalink})
			}
		}
	}

	return mappings, nil
}
