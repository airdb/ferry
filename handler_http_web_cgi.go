package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/phuslu/log"
)

type HTTPWebCgiHandler struct {
	Runtime    []string
	Location   string
	Root       string
	DefaultApp string
	Param      map[string]string

	phpcgi string
	param  map[string]*template.Template
}

func (h *HTTPWebCgiHandler) Load() (err error) {
	root := h.Root
	if root == "" {
		return errors.New("empty cgi root")
	}

	if strings.HasSuffix(h.Location, ".php") ||
		strings.HasSuffix(h.DefaultApp, ".php") ||
		slices.Contains(h.Runtime, "php-cgi") {
		h.phpcgi, err = exec.LookPath("php-cgi")
		if err != nil {
			return err
		}
	}

	if h.param == nil {
		h.param = make(map[string]*template.Template)
	}
	for k, v := range h.Param {
		if h.param[k], err = template.New(k).Parse(v); err != nil {
			return err
		}
	}

	return
}

func (h *HTTPWebCgiHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ri := req.Context().Value(RequestInfoContextKey).(*RequestInfo)

	var fullname string
	if strings.TrimRight(req.URL.Path, "/") == strings.TrimRight(h.Location, "/") && h.DefaultApp != "" {
		fullname = h.DefaultApp
		if !strings.HasPrefix(fullname, "/") {
			fullname = filepath.Join(h.Root, fullname)
		}
	}

	if fullname == "" {
		fullname = filepath.Join(h.Root, strings.TrimPrefix(req.URL.Path, h.Location))
	}

	log.Info().Context(ri.LogContext).Str("fullname", fullname).Msg("web cgi request")

	vars := map[string]any{
		"document_root": h.Root,
	}

	envs := []string{"SCRIPT_FILENAME=" + fullname}
	for k, v := range h.param {
		var sb strings.Builder
		if err := v.Execute(&sb, vars); err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		switch k {
		case "SCRIPT_FILENAME":
			fullname = sb.String()
		}
		envs = append(envs, fmt.Sprintf("%s=%s", k, sb.String()))
	}

	if slices.Contains(h.Runtime, "php-cgi") && strings.HasSuffix(fullname, ".php") && h.phpcgi != "" {
		/*
			/etc/php/8.1/cgi/conf.d/99-enable-headers.ini
				cgi.rfc2616_headers = 1
				cgi.force_redirect = 0
				force_cgi_redirect = 0
		*/
		(&cgi.Handler{
			Path: h.phpcgi,
			Dir:  h.Root,
			Root: h.Root,
			Args: []string{fullname},
			Env:  envs,
		}).ServeHTTP(rw, req)
		return
	}

	if slices.Contains(h.Runtime, "cgi") && strings.HasSuffix(fullname, ".cgi") {
		(&cgi.Handler{
			Path: fullname,
			Root: h.Root,
			Env:  envs,
		}).ServeHTTP(rw, req)
		return
	}

	fi, err := os.Stat(fullname)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	if fi != nil && fi.IsDir() {
		index := filepath.Join(fullname, "index.html")
		fi, err = os.Stat(index)
		if err == nil && !fi.IsDir() {
			fullname = index
		}
	}
	http.ServeFile(rw, req, fullname)
}
