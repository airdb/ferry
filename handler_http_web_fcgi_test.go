package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/phuslu/log"
)

var addr = fmt.Sprintf("/tmp/ferry-%d.sock", rand.Uint64())
var fcgiPass = "unix:/run/php/php8.2-fpm.sock"

// func TestMain(m *testing.M) {
// 	// ln, err := net.Listen("unix", addr)
// 	// if err != nil {
// 	// 	panic(err)
// 	// }

// 	// go fcgi.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 	// 	switch r.URL.Path {
// 	// 	case "/time.php":
// 	// 		fmt.Fprintf(w, "%d", time.Now().Unix())
// 	// 	default:
// 	// 		w.Write([]byte("Hello, world!"))
// 	// 	}
// 	// }))

// 	exitVal := m.Run()
// 	os.Exit(exitVal)
// }

func TestHTTPWebFcgiHandler_Load(t *testing.T) {
	if !fcgiAddrExist() {
		return
	}
	type fields struct {
		Root       string
		DefaultApp string
		Pass       string
		Param      string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{"", fields{Root: "./testdata", Pass: fcgiPass}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &HTTPWebFcgiHandler{
				Root:       tt.fields.Root,
				DefaultApp: tt.fields.DefaultApp,
				Pass:       tt.fields.Pass,
				Param:      tt.fields.Param,
			}
			if err := h.Load(); (err != nil) != tt.wantErr {
				t.Errorf("HTTPWebFcgiHandler.Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHTTPWebFcgiHandler_ServeHTTP(t *testing.T) {
	if !fcgiAddrExist() {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	type fields struct {
		Root       string
		DefaultApp string
		Pass       string
		Param      string
	}
	type args struct {
		rw   *httptest.ResponseRecorder
		req  *http.Request
		body func() string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name:   "serve time.php 01",
			fields: fields{Root: path.Join(cwd, "./testdata"), Pass: fcgiPass},
			args: args{
				rw:  httptest.NewRecorder(),
				req: httptest.NewRequest("GET", "/time.php", nil),
				body: func() string {
					return strconv.FormatInt(time.Now().Unix(), 10)
				},
			},
		},
		{
			name:   "serve time.php 02",
			fields: fields{Root: path.Join(cwd, "./testdata"), Pass: fcgiPass},
			args: args{
				rw:  httptest.NewRecorder(),
				req: httptest.NewRequest("GET", "/time.php", nil),
				body: func() string {
					return strconv.FormatInt(time.Now().Unix(), 10)
				},
			},
		},
		{
			name:   "serve time.php 03",
			fields: fields{Root: path.Join(cwd, "./testdata"), Pass: fcgiPass},
			args: args{
				rw:  httptest.NewRecorder(),
				req: httptest.NewRequest("GET", "/time.php", nil),
				body: func() string {
					return strconv.FormatInt(time.Now().Unix(), 10)
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &HTTPWebFcgiHandler{
				Root:       tt.fields.Root,
				DefaultApp: tt.fields.DefaultApp,
				Pass:       tt.fields.Pass,
				Param:      tt.fields.Param,
			}
			h.Load()
			h.ServeHTTP(tt.args.rw, tt.args.req.WithContext(
				context.WithValue(tt.args.req.Context(), RequestInfoContextKey, &RequestInfo{}),
			))
			resp := tt.args.rw.Result()
			body, _ := io.ReadAll(resp.Body)
			wantBody := tt.args.body()
			log.Debug().Msg(string(body))
			if string(body) != wantBody {
				t.Errorf("HTTPWebFcgiHandler.ServeHTTP() body = %v, wantBody %v", string(body), wantBody)
			}
		})
	}
}

func BenchmarkHTTPWebFcgiHandler_ServeHTTP_With_KeppAlive(b *testing.B) {
	cwd, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}

	h := &HTTPWebFcgiHandler{
		Root:      path.Join(cwd, "testdata"),
		Pass:      fcgiPass,
		KeepAlive: true,
	}
	h.Load()
	defer h.Close()

	rw := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/time.php", nil)
	req = req.WithContext(
		context.WithValue(req.Context(), RequestInfoContextKey, &RequestInfo{}),
	)

	log.DefaultLogger.SetLevel(log.WarnLevel)

	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rw, req)
		if rw.Result().StatusCode != http.StatusOK {
			b.Fatal("cig response status code error")
		}
	}
}

func BenchmarkHTTPWebFcgiHandler_ServeHTTP_Without_KeppAlive(b *testing.B) {
	cwd, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}

	h := &HTTPWebFcgiHandler{
		Root:      path.Join(cwd, "testdata"),
		Pass:      fcgiPass,
		KeepAlive: false,
	}
	h.Load()
	defer h.Close()

	rw := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/time.php", nil)
	req = req.WithContext(
		context.WithValue(req.Context(), RequestInfoContextKey, &RequestInfo{}),
	)

	log.DefaultLogger.SetLevel(log.WarnLevel)

	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rw, req)
		if rw.Result().StatusCode != http.StatusOK {
			b.Fatal("cig response status code error")
		}
	}
}
