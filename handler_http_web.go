package main

import (
	"errors"
	"expvar"
	"net"
	"net/http"
	"net/http/pprof"
	"net/netip"
	"regexp"
	"strings"
	"text/template"

	"github.com/phuslu/log"
)

// Refer: https://juejin.cn/post/6908623305129852942
type RouterModifier byte

const (
	_                      RouterModifier = iota
	RouterModifierSpace                   = 1
	RouterModifierEqual                   = 2 // =
	RouterModifierWavy                    = 3 // ~
	RouterModifierWavyStar                = 4 // ~*
	RouterModifierPrefix                  = 5 // ^~
	RouterModifierWildcard                = 6 // *?[]
)

type httpWebRouter struct {
	location string
	handler  HTTPHandler
	modifier RouterModifier
	pattern  string
	re       *regexp.Regexp
}

func (r *httpWebRouter) Load() error {
	r.pattern = r.location

	var err error
	if before, after, found := strings.Cut(r.location, " "); found {
		switch before {
		case " ":
			r.modifier = RouterModifierSpace
		case "=":
			r.modifier = RouterModifierEqual
		case "~":
			r.modifier = RouterModifierWavy
			r.re, err = regexp.Compile(after)
		case "~*":
			r.modifier = RouterModifierWavyStar
			r.re, err = regexp.Compile("(?i)" + after)
		case "^~":
			r.modifier = RouterModifierPrefix
		default:
			return errors.New("unknown location modifier")
		}
		r.pattern = after
		if err != nil {
			return err
		}
	} else if strings.ContainsAny(r.location, "*?[]") {
		r.modifier = RouterModifierWildcard
	} else {
		r.modifier = RouterModifierEqual
	}

	err = r.handler.Load()
	if err != nil {
		return nil
	}

	return nil
}

func (r *httpWebRouter) Match(path string) bool {
	switch r.modifier {
	case RouterModifierSpace:
		return strings.HasPrefix(path, r.pattern)
	case RouterModifierEqual:
		return r.pattern == path
	case RouterModifierWavy:
		return r.re.MatchString(path)
	case RouterModifierWavyStar:
		return r.re.MatchString(path)
	case RouterModifierPrefix:
		return strings.HasPrefix(path, r.pattern)
	case RouterModifierWildcard:
		return WildcardMatch(r.location, path)
	default:
		return false
	}
}

type HTTPWebHandler struct {
	Config    HTTPConfig
	Transport *http.Transport
	Functions template.FuncMap

	routers []httpWebRouter
	mux     *http.ServeMux
}

func (h *HTTPWebHandler) Load() error {
	var routers []httpWebRouter
	for _, web := range h.Config.Web {
		switch {
		case web.Cgi.Enabled:
			routers = append(routers, httpWebRouter{
				location: web.Location,
				handler: &HTTPWebCgiHandler{
					Location:   web.Location,
					Runtime:    web.Cgi.Runtime,
					Root:       web.Cgi.Root,
					DefaultApp: web.Cgi.DefaultAPP,
					Param:      web.Cgi.Param,
				},
			})
		case web.Dav.Enabled:
			routers = append(routers, httpWebRouter{
				location: web.Location,
				handler: &HTTPWebDavHandler{
					Root:              web.Dav.Root,
					AuthBasicUserFile: web.Dav.AuthBasicUserFile,
				},
			})
		case web.Index.Root != "" || web.Index.Body != "" || web.Index.File != "":
			routers = append(routers, httpWebRouter{
				location: web.Location,
				handler: &HTTPWebIndexHandler{
					Functions: h.Functions,
					Location:  web.Location,
					Root:      web.Index.Root,
					Headers:   web.Index.Headers,
					Body:      web.Index.Body,
					File:      web.Index.File,
				},
			})
		case web.Proxy.Pass != "":
			routers = append(routers, httpWebRouter{
				location: web.Location,
				handler: &HTTPWebProxyHandler{
					Transport:         h.Transport,
					Functions:         h.Functions,
					Pass:              web.Proxy.Pass,
					AuthBasicUserFile: web.Proxy.AuthBasicUserFile,
					SetHeaders:        web.Proxy.SetHeaders,
					DumpFailure:       web.Proxy.DumpFailure,
				},
			})
		}
	}

	var root HTTPHandler
	h.mux = http.NewServeMux()
	for _, x := range routers {
		err := x.Load()
		if err != nil {
			log.Fatal().Err(err).Str("web_location", x.location).Msgf("%T.Load() return error: %+v", x.handler, err)
		}
		log.Info().Str("web_location", x.location).Msgf("%T.Load() ok", x.handler)

		if x.pattern == "/" {
			root = x.handler
			continue
		}

		if x.modifier == RouterModifierEqual {
			h.mux.Handle(x.location, x.handler)
		} else {
			h.routers = append(h.routers, x)
		}
	}

	h.mux.HandleFunc("/debug/", func(rw http.ResponseWriter, req *http.Request) {
		if ap, err := netip.ParseAddrPort(req.RemoteAddr); err == nil && !ap.Addr().IsGlobalUnicast() {
			http.Error(rw, "403 forbidden", http.StatusForbidden)
			return
		}

		switch req.URL.Path {
		case "/debug/vars":
			expvar.Handler().ServeHTTP(rw, req)
		case "/debug/pprof/cmdline":
			pprof.Cmdline(rw, req)
		case "/debug/pprof/profile":
			pprof.Profile(rw, req)
		case "/debug/pprof/symbol":
			pprof.Symbol(rw, req)
		case "/debug/pprof/trace":
			pprof.Trace(rw, req)
		default:
			pprof.Index(rw, req)
		}
	})

	h.mux.HandleFunc("/", func(rw http.ResponseWriter, req *http.Request) {
		if root != nil {
			root.ServeHTTP(rw, req)
			return
		}
		http.NotFound(rw, req)
	})

	return nil
}

func (h *HTTPWebHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if config, ok := h.Config.ServerConfig[req.Host]; ok && !config.DisableHttp3 && req.ProtoMajor != 3 {
		_, port, _ := net.SplitHostPort(req.Context().Value(http.LocalAddrContextKey).(net.Addr).String())
		rw.Header().Add("Alt-Svc", `h3=":`+port+`"; ma=2592000,h3-29=":`+port+`"; ma=2592000`)
	}
	for _, x := range h.routers {
		if x.Match(req.URL.Path) {
			x.handler.ServeHTTP(rw, req)
			return
		}
	}
	h.mux.ServeHTTP(rw, req)
}
