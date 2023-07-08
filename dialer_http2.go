package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/net/http2"
)

var _ Dialer = (*HTTP2Dialer)(nil)

type HTTP2Dialer struct {
	Username  string
	Password  string
	Host      string
	Port      string
	UserAgent string
	Dialer    Dialer

	mu        sync.Mutex
	transport *http2.Transport
}

func (d *HTTP2Dialer) init() {
	if d.transport != nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.transport != nil {
		return
	}

	d.transport = &http2.Transport{
		DisableCompression: false,
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			dialer := d.Dialer
			if dialer == nil {
				dialer = &net.Dialer{}
			}
			conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(d.Host, d.Port))
			if err != nil {
				return nil, err
			}

			tlsConn := tls.Client(conn, &tls.Config{
				NextProtos:         []string{"h2"},
				InsecureSkipVerify: false,
				ServerName:         d.Host,
				ClientSessionCache: tls.NewLRUClientSessionCache(1024),
			})

			err = tlsConn.HandshakeContext(ctx)
			if err != nil {
				return nil, err
			}

			return tlsConn, nil
		},
	}

	if d.UserAgent == "" {
		d.UserAgent = DefaultHTTPUserAgent
	}
}

func (d *HTTP2Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.init()

	switch network {
	case "tcp", "tcp6", "tcp4":
	default:
		return nil, errors.New("proxy: no support for HTTP proxy connections of type " + network)
	}

	pr, pw := io.Pipe()
	req := &http.Request{
		ProtoMajor: 2,
		Method:     http.MethodConnect,
		URL: &url.URL{
			Scheme: "https",
			Host:   addr,
		},
		Host: addr,
		Header: http.Header{
			"content-type": []string{"application/octet-stream"},
			"user-agent":   []string{d.UserAgent},
		},
		Body:          pr,
		ContentLength: -1,
	}

	if d.Username != "" && d.Password != "" {
		req.Header.Set("proxy-authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(d.Username+":"+d.Password)))
	}

	resp, err := d.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var errmsg string
		if resp.Body != nil {
			data := make([]byte, 1024)
			if n, err := resp.Body.Read(data); err != nil {
				errmsg = err.Error()
			} else {
				errmsg = string(data[:n])
			}
		}
		return nil, errors.New("proxy: read from " + d.Host + " error: " + resp.Status + ": " + errmsg)
	}

	// see https://github.com/golang/net/blob/d8f9c0143e94e55c0e871e302e81cf982732df30/http2/transport.go#L2526
	type transportResponseBody struct {
		cs *struct {
			cc *http2.ClientConn
		}
	}

	conn := &http2Stream{
		r:      resp.Body,
		w:      pw,
		cc:     (*transportResponseBody)(unsafe.Pointer(&resp.Body)).cs.cc,
		closed: make(chan struct{}),
	}

	return conn, nil
}

type http2Stream struct {
	r  io.ReadCloser
	w  io.Writer
	cc *http2.ClientConn

	closed chan struct{}
}

func (c *http2Stream) Read(b []byte) (n int, err error) {
	return c.r.Read(b)
}

func (c *http2Stream) Write(b []byte) (n int, err error) {
	return c.w.Write(b)
}

func (c *http2Stream) Close() (err error) {
	select {
	case <-c.closed:
		return
	default:
		close(c.closed)
	}
	if rc, ok := c.r.(io.Closer); ok {
		err = rc.Close()
	}
	if w, ok := c.w.(io.Closer); ok {
		err = w.Close()
	}
	return
}

// see https://github.com/golang/net/blob/d8f9c0143e94e55c0e871e302e81cf982732df30/http2/transport.go#L291
type http2ClientConn struct {
	t     *http2.Transport
	tconn net.Conn
}

func (c *http2Stream) LocalAddr() net.Addr {
	return (*http2ClientConn)(unsafe.Pointer(c.cc)).tconn.LocalAddr()
}

func (c *http2Stream) RemoteAddr() net.Addr {
	return (*http2ClientConn)(unsafe.Pointer(c.cc)).tconn.RemoteAddr()
}

func (c *http2Stream) SetDeadline(t time.Time) error {
	return &net.OpError{Op: "set", Net: "http2", Source: nil, Addr: nil, Err: errors.New("deadline not supported")}
}

func (c *http2Stream) SetReadDeadline(t time.Time) error {
	return &net.OpError{Op: "set", Net: "http2", Source: nil, Addr: nil, Err: errors.New("deadline not supported")}
}

func (c *http2Stream) SetWriteDeadline(t time.Time) error {
	return &net.OpError{Op: "set", Net: "http2", Source: nil, Addr: nil, Err: errors.New("deadline not supported")}
}
