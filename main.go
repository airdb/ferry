package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	stdLog "log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	quic "github.com/lucas-clemente/quic-go"
	"github.com/oschwald/maxminddb-golang"
	"github.com/phuslu/log"
	"github.com/robfig/cron/v3"
	"golang.org/x/net/http2"
)

var (
	version = "r1984"
	timeNow = time.Now
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-version" {
		println(version)
		return
	}

	StartSupervisor()

	rand.Seed(timeNow().UnixNano())
	executable, err := os.Executable()
	if err != nil {
		println("cannot get executable path")
		os.Exit(1)
	}

	var validate bool
	flag.BoolVar(&validate, "validate", false, "parse the liner conf and exit")
	flag.Parse()

	config, err := NewConfig(flag.Arg(0))
	if err != nil {
		log.Fatal().Err(err).Str("filename", flag.Arg(0)).Msg("NewConfig() error")
		os.Exit(1)
	}

	// main logger
	var forwardLogger log.Logger
	if log.IsTerminal(os.Stderr.Fd()) {
		log.DefaultLogger = log.Logger{
			Level:      log.ParseLevel(config.Log.Level),
			Caller:     1,
			TimeFormat: "15:04:05",
			Writer: &log.ConsoleWriter{
				ANSIColor: true,
			},
		}
		forwardLogger = log.Logger{
			Level:  log.ParseLevel(config.Log.Level),
			Writer: log.DefaultLogger.Writer,
		}
	} else {
		// main logger
		log.DefaultLogger = log.Logger{
			Level: log.ParseLevel(config.Log.Level),
			Writer: &log.Writer{
				Filename:   executable + ".log",
				MaxBackups: 1,
				MaxSize:    config.Log.Maxsize,
				LocalTime:  config.Log.Localtime,
			},
		}
		// forward logger
		forwardLogger = log.Logger{
			Level: log.ParseLevel(config.Log.Level),
			Writer: &log.Writer{
				Filename:   "forward.log",
				MaxBackups: config.Log.Backups,
				MaxSize:    config.Log.Maxsize,
				LocalTime:  config.Log.Localtime,
			},
		}
	}

	// global dailer
	resolver := &Resolver{
		Resolver: &net.Resolver{
			PreferGo: false,
		},
		DNSCacheTTL: 10 * time.Minute,
	}

	regionResolver := &RegionResolver{
		Resolver: resolver,
	}

	if ok, _ := regexp.Match(`\((geoip|region|city) `, config.raw); ok {
		log.Info().Msg("try load maxmind geoip2 database")
		for _, filename := range []string{
			"GeoIP2-Enterprise.mmdb",
			"GeoIP2-City.mmdb",
			"GeoIP2-City-Africa.mmdb",
			"GeoIP2-City-Asia-Pacific.mmdb",
			"GeoIP2-City-Europe.mmdb",
			"GeoIP2-City-North-America.mmdb",
			"GeoIP2-City-South-America.mmdb",
			"GeoIP2-Precision-City.mmdb",
			"GeoLite2-City.mmdb",
			"GeoIP2-Country.mmdb",
			"GeoLite2-Country.mmdb",
		} {
			reader, err := maxminddb.Open(filename)
			switch {
			case os.IsNotExist(err):
				continue
			case err != nil:
				log.Fatal().Err(err).Str("geoip2_database", filename).Msg("load geoip2_city_database error")
			}
			regionResolver.MaxmindReader = reader
			break
		}
	}

	dialer := &Dialer{
		Resolver:              resolver,
		ParallelLevel:         2,
		DenyIntranet:          config.Global.DenyIntranet,
		Timeout:               30 * time.Second,
		TCPKeepAlive:          30 * time.Second,
		TLSClientSessionCache: tls.NewLRUClientSessionCache(2048),
	}

	if config.Global.DialTimeout > 0 {
		dialer.Timeout = time.Duration(config.Global.DialTimeout) * time.Second
	}

	if config.Global.PreferIpv6 {
		dialer.PreferIPv6 = true
		dialer.ParallelLevel = 1
	}

	if config.Global.DnsTtl > 0 {
		dialer.Resolver.DNSCacheTTL = time.Duration(config.Global.DnsTtl) * time.Second
	}

	if dnsServer := config.Global.DnsServer; dnsServer != "" {
		if !strings.Contains(dnsServer, "://") {
			dnsServer = "udp://" + dnsServer
		}
		u, err := url.Parse(dnsServer)
		if err != nil {
			log.Fatal().Err(err).Str("dns_server", config.Global.DnsServer).Msg("parse dns_server error")
		}
		if u.Scheme == "" || u.Host == "" {
			log.Fatal().Err(errors.New("no scheme or host")).Str("dns_server", config.Global.DnsServer).Msg("parse dns_server error")
		}

		switch u.Scheme {
		case "udp", "tcp":
			var addr = u.Host
			if _, _, err := net.SplitHostPort(u.Host); err != nil {
				addr = net.JoinHostPort(addr, "53")
			}
			dialer.Resolver.Resolver.Dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, u.Scheme, addr)
			}
		case "tls":
			var addr = u.Host
			if _, _, err := net.SplitHostPort(u.Host); err != nil {
				addr = net.JoinHostPort(addr, "853")
			}
			tlsConfig := &tls.Config{
				ServerName:         u.Hostname(),
				ClientSessionCache: tls.NewLRUClientSessionCache(128),
			}
			dialer.Resolver.Resolver.Dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
				return tls.Dial("tcp", addr, tlsConfig)
			}
		case "https":
			dialer.Resolver.Resolver.Dial = (&DoHDialer{
				EndPoint:  config.Global.DnsServer,
				UserAgent: DefaultHTTPDialerUserAgent,
				Transport: &http2.Transport{
					TLSClientConfig: &tls.Config{
						ServerName:         u.Hostname(),
						ClientSessionCache: tls.NewLRUClientSessionCache(128),
					},
				},
			}).DialContext
		}
	}

	// see http.DefaultTransport
	transport := &http.Transport{
		DialContext: dialer.DialContext,
		DialTLS: func(network, address string) (net.Conn, error) {
			return dialer.DialTLS(network, address, &tls.Config{
				InsecureSkipVerify: true,
				ClientSessionCache: dialer.TLSClientSessionCache,
			})
		},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    false,
	}

	if config.Global.MaxIdleConns > 0 {
		transport.MaxIdleConns = config.Global.MaxIdleConns
	}

	if config.Global.IdleConnTimeout > 0 {
		transport.IdleConnTimeout = time.Duration(config.Global.IdleConnTimeout) * time.Second
	}

	upstreams := make(map[string]*http.Transport)
	for name, upstream := range config.Upstream {
		var dialContext func(context.Context, string, string) (net.Conn, error)
		switch upstream.Scheme {
		case "http", "http1":
			dialContext = (&HTTPDialer{
				Username:  upstream.Username,
				Password:  upstream.Password,
				Host:      upstream.Host,
				Port:      strconv.Itoa(upstream.Port),
				UserAgent: upstream.UserAgent,
				Dialer:    dialer,
			}).DialContext
		case "https", "http2":
			dialContext = (&HTTP2Dialer{
				Username:  upstream.Username,
				Password:  upstream.Password,
				Host:      upstream.Host,
				Port:      strconv.Itoa(upstream.Port),
				UserAgent: upstream.UserAgent,
			}).DialContext
		case "quic":
			dialContext = (&QuicDialer{
				Username: upstream.Username,
				Password: upstream.Password,
				Host:     upstream.Host,
				Port:     strconv.Itoa(upstream.Port),
			}).DialContext
		case "socks", "socks5", "socks5h":
			dialContext = (&Socks5Dialer{
				Username: upstream.Username,
				Password: upstream.Password,
				Host:     upstream.Host,
				Port:     strconv.Itoa(upstream.Port),
				Socsk5H:  upstream.Scheme == "socks5h",
				Dialer:   dialer,
			}).DialContext
		case "socks4", "socks4a":
			dialContext = (&Socks4Dialer{
				Username: upstream.Username,
				Password: upstream.Password,
				Host:     upstream.Host,
				Port:     strconv.Itoa(upstream.Port),
				Socks4A:  upstream.Scheme == "socks4a",
				Dialer:   dialer,
			}).DialContext
		default:
			log.Fatal().Str("upstream_scheme", upstream.Scheme).Msgf("unsupported upstream=%+v", upstream)
		}
		upstreams[name] = &http.Transport{
			DialContext:         dialContext,
			TLSClientConfig:     transport.TLSClientConfig,
			TLSHandshakeTimeout: transport.TLSHandshakeTimeout,
			IdleConnTimeout:     transport.IdleConnTimeout,
			DisableCompression:  transport.DisableCompression,
			MaxIdleConns:        32,
		}
	}

	functions := (&Functions{
		RegionResolver: regionResolver,
	}).FuncMap()

	lc := ListenConfig{
		FastOpen:    config.Global.TcpFastopen,
		ReusePort:   true,
		DeferAccept: true,
	}

	servers := make([]*http.Server, 0)

	// listen and serve https
	tlsConfigurator := &TLSConfigurator{}
	h2handlers := map[string]map[string]http.Handler{}
	for _, server := range config.Https {
		// requestinfo -> forward -> pac -> pprof -> proxy
		handler := &HTTPHandler{
			Next: &HTTPForwardHandler{
				Next: &HTTPPacHandler{
					Next: &HTTPPprofHandler{
						Next: &HTTPProxyHandler{
							Config:    server,
							Transport: transport,
						},
						Config: server,
					},
					Config: server,
				},
				Config:         server,
				ForwardLogger:  forwardLogger,
				ServerNames:    NewStringSet(server.ServerName),
				RegionResolver: regionResolver,
				Dialer:         dialer,
				Transport:      transport,
				Upstreams:      upstreams,
				Functions:      functions,
			},
			TLSConfigurator: tlsConfigurator,
		}

		var h http.Handler = handler
		for {
			if loadable, ok := h.(interface {
				Load() error
			}); ok {
				err = loadable.Load()
				if err != nil {
					log.Fatal().Err(err).Strs("server_name", server.ServerName).Msgf("%T.Load() return error: %+v", h, err)
				}
				log.Info().Strs("server_name", server.ServerName).Msgf("%T.Load() ok", h)
			}

			v := reflect.Indirect(reflect.ValueOf(h)).FieldByName("Next")
			if !v.IsValid() {
				break
			}
			h = v.Interface().(http.Handler)
		}

		// add support for ip tls certificate
		if len(server.ServerName) > 0 && net.ParseIP(server.ServerName[0]) != nil {
			server.ServerName = append(server.ServerName, "")
		}

		for _, listen := range server.Listen {
			for _, name := range server.ServerName {
				tlsConfigurator.AddCertEntry(TLSConfiguratorEntry{
					ServerName:   name,
					KeyFile:      server.Keyfile,
					CertFile:     server.Certfile,
					DisableHTTP2: server.DisableHttp2,
				})
				if tlsConfigurator.DefaultServername == "" {
					tlsConfigurator.DefaultServername = name
				}
				hs, ok := h2handlers[listen]
				if !ok {
					hs = make(map[string]http.Handler)
					h2handlers[listen] = hs
				}
				hs[name] = handler
			}
		}
	}

	for addr, handlers := range h2handlers {
		addr, handlers := addr, handlers

		var ln net.Listener

		if ln, err = lc.Listen(context.Background(), "tcp", addr); err != nil {
			log.Fatal().Err(err).Str("address", addr).Msg("net.Listen error")
		}

		log.Info().Str("version", version).Str("address", ln.Addr().String()).Msg("liner listen and serve tls")

		server := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if s, _, err := net.SplitHostPort(r.TLS.ServerName); err == nil {
					r.TLS.ServerName = s
				}
				var serverName = r.TLS.ServerName
				if serverName == "" {
					serverName = tlsConfigurator.DefaultServername
				}

				h, _ := handlers[serverName]
				if h == nil {
					http.NotFound(w, r)
					return
				}
				h.ServeHTTP(w, r)
			}),
			TLSConfig: &tls.Config{
				GetConfigForClient: tlsConfigurator.GetConfigForClient,
			},
			ConnState: tlsConfigurator.ConnState,
			ErrorLog:  stdLog.New(LevelWriter{log.DefaultLogger, log.ErrorLevel}, "", stdLog.Lshortfile),
		}

		http2.ConfigureServer(server, &http2.Server{})

		go server.Serve(tls.NewListener(TCPListener{
			TCPListener:     ln.(*net.TCPListener),
			KeepAlivePeriod: 3 * time.Minute,
			ReadBufferSize:  32 * 1024,
			WriteBufferSize: 32 * 1024,
		}, server.TLSConfig))

		servers = append(servers, server)
	}

	// listen and serve http
	h1handlers := map[string]http.Handler{}
	for _, httpConfig := range config.Http {
		// requestinfo -> forward -> pac -> pprof -> proxy
		httpConfig.ServerName = append(httpConfig.ServerName, "", "localhost", "127.0.0.1")
		if name, err := os.Hostname(); err == nil {
			httpConfig.ServerName = append(httpConfig.ServerName, name)
		}
		if ip, err := GetPreferedLocalIP(); err == nil {
			httpConfig.ServerName = append(httpConfig.ServerName, ip.String())
		}
		handler := &HTTPHandler{
			Next: &HTTPForwardHandler{
				Next: &HTTPPacHandler{
					Next: &HTTPPprofHandler{
						Next: &HTTPProxyHandler{
							Config:    httpConfig,
							Transport: transport,
						},
						Config: httpConfig,
					},
					Config: httpConfig,
				},
				Config:         httpConfig,
				ForwardLogger:  forwardLogger,
				ServerNames:    NewStringSet(httpConfig.ServerName),
				RegionResolver: regionResolver,
				Dialer:         dialer,
				Transport:      transport,
				Upstreams:      upstreams,
				Functions:      functions,
			},
		}

		var h http.Handler = handler
		for {
			if loadable, ok := h.(interface {
				Load() error
			}); ok {
				err = loadable.Load()
				if err != nil {
					log.Fatal().Err(err).Strs("server_name", httpConfig.ServerName).Msgf("%T.Load() return error: %+v", h, err)
				}
				log.Info().Strs("server_name", httpConfig.ServerName).Msgf("%T.Load() ok", h)
			}

			v := reflect.Indirect(reflect.ValueOf(h)).FieldByName("Next")
			if !v.IsValid() {
				break
			}
			h = v.Interface().(http.Handler)
		}

		for _, listen := range httpConfig.Listen {
			h1handlers[listen] = handler
		}
	}

	for addr, handler := range h1handlers {
		addr, handler := addr, handler

		var ln net.Listener

		if ln, err = lc.Listen(context.Background(), "tcp", addr); err != nil {
			log.Fatal().Err(err).Str("address", addr).Msg("net.Listen error")
		}

		log.Info().Str("version", version).Str("address", ln.Addr().String()).Msg("liner listen and serve")

		server := &http.Server{
			Handler:  handler,
			ErrorLog: stdLog.New(LevelWriter{log.DefaultLogger, log.ErrorLevel}, "", stdLog.Lshortfile),
		}

		go server.Serve(TCPListener{
			TCPListener:     ln.(*net.TCPListener),
			KeepAlivePeriod: 3 * time.Minute,
			ReadBufferSize:  32 * 1024,
			WriteBufferSize: 32 * 1024,
		})

		servers = append(servers, server)
	}

	// quic handler
	for _, quicConfig := range config.Quic {
		for _, addr := range quicConfig.Listen {
			tlsConfig, err := GenerateTLSConfig()
			if err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("generate tls config error")
			}

			laddr, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("quic.Listen error")
			}

			conn, err := net.ListenUDP("udp", laddr)
			if err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("quic.Listen error")
			}

			ln, err := quic.Listen(conn, tlsConfig, nil)
			if err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("quic.Listen error")
			}

			log.Info().Str("version", version).Str("address", ln.Addr().String()).Msg("liner listen and serve quic")

			h := &QuicHandler{
				Config:         quicConfig,
				ForwardLogger:  forwardLogger,
				RegionResolver: regionResolver,
				Dialer:         dialer,
				Upstreams:      upstreams,
				Functions:      functions,
			}

			if err = h.Load(); err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("quic hanlder load error")
			}

			go func(ln quic.Listener, h *QuicHandler) {
				for {
					session, err := ln.Accept(context.Background())
					if err != nil {
						log.Error().Err(err).Str("version", version).Str("address", ln.Addr().String()).Msg("liner accept quic connection error")
						time.Sleep(10 * time.Millisecond)
					}

					go h.ServeSession(session)
				}
			}(ln, h)
		}
	}

	// socks handler
	for _, socksConfig := range config.Socks {
		for _, addr := range socksConfig.Listen {
			var ln net.Listener

			if ln, err = lc.Listen(context.Background(), "tcp", addr); err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("net.Listen error")
			}

			log.Info().Str("version", version).Str("address", ln.Addr().String()).Msg("liner listen and serve socks")

			h := &SocksHandler{
				Config:         socksConfig,
				ForwardLogger:  forwardLogger,
				RegionResolver: regionResolver,
				Dialer:         dialer,
				Upstreams:      upstreams,
				Functions:      functions,
			}

			if err = h.Load(); err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("socks hanlder load error")
			}

			go func(ln net.Listener, h *SocksHandler) {
				for {
					conn, err := ln.Accept()
					if err != nil {
						log.Error().Err(err).Str("version", version).Str("address", ln.Addr().String()).Msg("liner accept socks connection error")
						time.Sleep(10 * time.Millisecond)
					}
					go h.ServeConn(conn)
				}
			}(ln, h)
		}
	}

	// relay handler
	for _, relayConfig := range config.Relay {
		for _, addr := range relayConfig.Listen {
			var ln net.Listener

			if ln, err = lc.Listen(context.Background(), "tcp", addr); err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("net.Listen error")
			}

			log.Info().Str("version", version).Str("address", ln.Addr().String()).Msg("liner listen and forward port")

			h := &RelayHandler{
				Config:         relayConfig,
				ForwardLogger:  forwardLogger,
				RegionResolver: regionResolver,
				Dialer:         dialer,
				Upstreams:      upstreams,
			}

			if err = h.Load(); err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("socks hanlder load error")
			}

			go func(ln net.Listener, h *RelayHandler) {
				for {
					conn, err := ln.Accept()
					if err != nil {
						log.Error().Err(err).Str("version", version).Str("address", ln.Addr().String()).Msg("liner accept socks connection error")
						time.Sleep(10 * time.Millisecond)
					}
					go h.ServeConn(conn)
				}
			}(ln, h)
		}
	}

	// dns handler
	for _, dnsConfig := range config.Dns {
		for _, addr := range dnsConfig.Listen {
			var conn net.PacketConn

			if conn, err = lc.ListenPacket(context.Background(), "udp", addr); err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("net.ListenPacket error")
			}

			log.Info().Str("version", version).Str("address", conn.LocalAddr().String()).Msg("liner listen and serve dns")

			h := &DNSHandler{
				Config: dnsConfig,
			}

			if err = h.Load(); err != nil {
				log.Fatal().Err(err).Str("address", addr).Msg("socks hanlder load error")
			}

			go func(conn net.PacketConn, h *DNSHandler) {
				for {
					buf := make([]byte, 1024)
					n, addr, err := conn.ReadFrom(buf)
					if err != nil {
						log.Debug().Err(err).Str("version", version).Str("address", conn.LocalAddr().String()).Msg("liner accept socks connection error")
						continue
					}
					go h.ServePacketConn(conn, addr, buf[:n])
				}
			}(conn, h)
		}
	}

	if validate {
		os.Exit(0)
	}

	var cronOptions = []cron.Option{
		cron.WithSeconds(),
		cron.WithLogger(cron.PrintfLogger(log.DefaultLogger)),
	}
	if !config.Log.Localtime {
		cronOptions = append(cronOptions, cron.WithLocation(time.UTC))
	}
	runner := cron.New(cronOptions...)
	if !log.IsTerminal(os.Stderr.Fd()) {
		runner.AddFunc("0 0 0 * * *", func() { log.DefaultLogger.Writer.(*log.Writer).Rotate() })
		runner.AddFunc("0 0 0 * * *", func() { forwardLogger.Writer.(*log.Writer).Rotate() })
	}
	for _, job := range config.Cron {
		spec, command := job.Spec, job.Command
		runner.AddFunc(spec, func() {
			cmd := exec.CommandContext(context.Background(), "/bin/bash", "-c", command)
			err = cmd.Run()
			if err != nil {
				log.Warn().Strs("cmd_args", cmd.Args).Err(err).Msg("exec cron_command error")
				return
			}
			log.Info().Str("cron_command", command).Msg("exec cron_command OK")
		})
	}
	go runner.Run()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM)
	signal.Notify(c, syscall.SIGINT)
	signal.Notify(c, syscall.SIGHUP)

	switch <-c {
	case syscall.SIGTERM, syscall.SIGINT:
		log.Info().Msg("liner flush logs and exit.")
		os.Exit(0)
	}

	log.Warn().Msg("liner start graceful shutdown...")

	SetProcessName("liner: (graceful shutdown)")

	timeout := 5 * time.Minute
	if config.Global.GracefulTimeout > 0 {
		timeout = time.Duration(config.Global.GracefulTimeout) * time.Second
	}

	var wg sync.WaitGroup
	for _, server := range servers {
		wg.Add(1)
		go func(server *http.Server) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			if err := server.Shutdown(ctx); err != nil {
				log.Error().Err(err).Msgf("%T.Shutdown() error", server)
			}
		}(server)
	}
	wg.Wait()

	log.Info().Msg("liner server shutdown")
}
