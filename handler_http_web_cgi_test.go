package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/phuslu/log"
)

func TestHTTPWebCgiHandler_Load(t *testing.T) {
	type fields struct {
		Location   string
		Root       string
		DefaultApp string
		phpcgi     string
	}

	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{}

	phpcgi, err := exec.LookPath("php-cgi")
	if err != nil {
		t.Log("can not find php-cgi")
	} else {
		tests = append(tests, struct {
			name    string
			fields  fields
			wantErr bool
		}{name: "load php", fields: fields{Location: "*.php", Root: "./testdata", phpcgi: phpcgi}})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &HTTPWebCgiHandler{
				Location:   tt.fields.Location,
				Root:       tt.fields.Root,
				DefaultApp: tt.fields.DefaultApp,
				phpcgi:     tt.fields.phpcgi,
			}
			if err := h.Load(); (err != nil) != tt.wantErr {
				t.Errorf("HTTPWebCgiHandler.Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHTTPWebCgiHandler_ServeHTTP(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	type fields struct {
		Location   string
		Root       string
		DefaultApp string
		phpcgi     string
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
			name:   "serve time.cgi",
			fields: fields{Location: "*.cgi", Root: path.Join(cwd, "testdata"), phpcgi: ""},
			args: args{
				rw:  httptest.NewRecorder(),
				req: httptest.NewRequest("GET", "/time.cgi", nil),
				body: func() string {
					return strconv.FormatInt(time.Now().Unix(), 10)
				},
			},
		},
	}

	phpcgi, err := exec.LookPath("php-cgi")
	if err != nil {
		t.Log("can not find php-cgi")
	} else {
		tests = append(tests, struct {
			name   string
			fields fields
			args   args
		}{
			name:   "serve time.php",
			fields: fields{Location: "*.php", Root: path.Join(cwd, "testdata"), phpcgi: phpcgi},
			args: args{
				rw:  httptest.NewRecorder(),
				req: httptest.NewRequest("GET", "/time.php", nil),
				body: func() string {
					return fmt.Sprintf("%d", time.Now().Unix())
				},
			},
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &HTTPWebCgiHandler{
				Runtime:    []string{"cgi", "php-cgi"},
				Location:   tt.fields.Location,
				Root:       tt.fields.Root,
				DefaultApp: tt.fields.DefaultApp,
				phpcgi:     tt.fields.phpcgi,
			}
			h.ServeHTTP(tt.args.rw, tt.args.req.WithContext(
				context.WithValue(tt.args.req.Context(), RequestInfoContextKey, &RequestInfo{}),
			))
			resp := tt.args.rw.Result()
			body, _ := io.ReadAll(resp.Body)
			wantBody := tt.args.body()
			if string(body) != wantBody {
				t.Errorf("HTTPWebCgiHandler.ServeHTTP() body = %v, wantBody %v", string(body), wantBody)
			}
		})
	}
}

func BenchmarkHTTPWebCgiHandler_ServeHTTP(b *testing.B) {
	cwd, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}

	phpcgi, err := exec.LookPath("php-cgi")
	if err != nil {
		b.Fatal("can not find php-cgi")
	}
	h := &HTTPWebCgiHandler{
		Runtime:  []string{"cgi", "php-cgi"},
		Location: "*.php",
		Root:     path.Join(cwd, "testdata"),
		phpcgi:   phpcgi,
	}
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
