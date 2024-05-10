package main

import (
	"bytes"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFcgiClientGet(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	client := &FcgiClient{
		network: "unix",
		address: "/run/php/php8.2-fpm.sock",
	}
	err = client.Connect()
	if err != nil {
		t.Fatal("connect err:", err.Error())
	}

	req := NewFcgiRequest()
	req.SetParams(map[string]string{
		"SCRIPT_FILENAME": path.Join(cwd, "./testdata/time.php"),
		"SERVER_SOFTWARE": "ferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgi/1.0.0",
		"REMOTE_ADDR":     "127.0.0.1",
		"QUERY_STRING":    "name=value&__ACTION__=/@wx",

		"SERVER_NAME":       "ferry.local",
		"SERVER_ADDR":       "127.0.0.1:80",
		"SERVER_PORT":       "80",
		"REQUEST_URI":       "/time.php?__ACTION__=/@wx",
		"DOCUMENT_ROOT":     path.Join(cwd, "./testdata"),
		"GATEWAY_INTERFACE": "CGI/1.1",
		"REDIRECT_STATUS":   "200",
		"HTTP_HOST":         "ferry.local",

		"REQUEST_METHOD": "GET",
	})

	resp, stderr, err := client.Call(req)
	if err != nil {
		t.Fatal("call error:", err.Error())
	}

	if len(stderr) > 0 {
		t.Fatal("stderr:", string(stderr))
	}

	t.Log("resp, status:", resp.StatusCode)
	t.Log("resp, status message:", resp.Status)
	t.Log("resp headers:", resp.Header)

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("resp body:", string(data))
}

func TestFcgiClientGetAlive(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	client := &FcgiClient{
		network: "unix",
		address: "/run/php/php8.2-fpm.sock",
	}
	client.KeepAlive()
	err = client.Connect()
	if err != nil {
		t.Fatal("connect err:", err.Error())
	}

	for i := 0; i < 10; i++ {
		req := NewFcgiRequest()
		req.SetParams(map[string]string{
			"SCRIPT_FILENAME": path.Join(cwd, "./testdata/time.php"),
			"SERVER_SOFTWARE": "ferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgi/1.0.0",
			"REMOTE_ADDR":     "127.0.0.1",
			"QUERY_STRING":    "name=value&__ACTION__=/@wx",

			"SERVER_NAME":       "ferry.local",
			"SERVER_ADDR":       "127.0.0.1:80",
			"SERVER_PORT":       "80",
			"REQUEST_URI":       "/time.php?__ACTION__=/@wx",
			"DOCUMENT_ROOT":     path.Join(cwd, "./testdata"),
			"GATEWAY_INTERFACE": "CGI/1.1",
			"REDIRECT_STATUS":   "200",
			"HTTP_HOST":         "ferry.local",

			"REQUEST_METHOD": "GET",
		})

		resp, _, err := client.Call(req)
		if err != nil {
			t.Fatal("do error:", err.Error())
		}

		t.Log("resp, status:", resp.StatusCode)
		t.Log("resp, status message:", resp.Status)
		t.Log("resp headers:", resp.Header)

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("resp body:", string(data))

		time.Sleep(1 * time.Second)
	}
}

func TestFcgiClientPost(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	client := &FcgiClient{
		network: "unix",
		address: "/run/php/php8.2-fpm.sock",
	}
	err = client.Connect()
	if err != nil {
		t.Fatal("connect err:", err.Error())
	}

	req := NewFcgiRequest()
	req.SetParams(map[string]string{
		"SCRIPT_FILENAME": path.Join(cwd, "./testdata/time.php"),
		"SERVER_SOFTWARE": "ferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgiferrycgi/1.0.0",
		"REMOTE_ADDR":     "127.0.0.1",
		"QUERY_STRING":    "name=value&__ACTION__=/@wx",

		"SERVER_NAME":       "ferry.local",
		"SERVER_ADDR":       "127.0.0.1:80",
		"SERVER_PORT":       "80",
		"REQUEST_URI":       "/time.php?__ACTION__=/@wx",
		"DOCUMENT_ROOT":     path.Join(cwd, "./testdata"),
		"GATEWAY_INTERFACE": "CGI/1.1",
		"REDIRECT_STATUS":   "200",
		"HTTP_HOST":         "ferry.local",

		"REQUEST_METHOD": "POST",
		"CONTENT_TYPE":   "application/x-www-form-urlencoded",
	})

	r := bytes.NewReader([]byte("name12=value&hello=world&name13=value&hello4=world"))
	//req.SetParam("CONTENT_LENGTH", fmt.Sprintf("%d", r.Len()))
	req.SetBody(r, uint32(r.Len()))

	resp, _, err := client.Call(req)
	if err != nil {
		t.Fatal("do error:", err.Error())
	}

	t.Log("resp, status:", resp.StatusCode)
	t.Log("resp, status message:", resp.Status)
	t.Log("resp headers:", resp.Header)

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("resp body:", string(data))
}

func TestFcgiClientPerformance(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	threads := 1500
	countRequests := 200
	countSuccess := 0
	countFail := 0
	locker := sync.Mutex{}
	beforeTime := time.Now()
	wg := sync.WaitGroup{}
	wg.Add(threads)

	pool := FcgiSharedPool("unix", "/run/php/php8.2-fpm.sock", 5)

	for i := 0; i < threads; i++ {
		go func(i int) {
			defer wg.Done()

			for j := 0; j < countRequests; j++ {
				client, err := pool.Client()
				if err != nil {
					t.Fatal("connect err:", err.Error())
				}

				req := NewFcgiRequest()
				req.SetTimeout(5 * time.Second)
				req.SetParams(map[string]string{
					"SCRIPT_FILENAME": path.Join(cwd, "./testdata/time.php"),
					"SERVER_SOFTWARE": "ferry/0.0.1",
					"REMOTE_ADDR":     "127.0.0.1",
					"QUERY_STRING":    "name=value&__ACTION__=/@wx",

					"SERVER_NAME":       "ferry.local",
					"SERVER_ADDR":       "127.0.0.1:80",
					"SERVER_PORT":       "80",
					"REQUEST_URI":       "/time.php?__ACTION__=/@wx",
					"DOCUMENT_ROOT":     path.Join(cwd, "./testdata"),
					"GATEWAY_INTERFACE": "CGI/1.1",
					"REDIRECT_STATUS":   "200",
					"HTTP_HOST":         "ferry.local",

					"REQUEST_METHOD": "GET",
				})

				resp, _, err := client.Call(req)
				if err != nil {
					locker.Lock()
					countFail++
					locker.Unlock()
					continue
				}
				defer resp.Body.Close()

				if resp.StatusCode == 200 {
					data, err := io.ReadAll(resp.Body)
					if err != nil || strings.Index(string(data), "Welcome") == -1 {
						locker.Lock()
						countFail++
						locker.Unlock()
					} else {
						locker.Lock()
						countSuccess++
						locker.Unlock()
					}
				} else {
					locker.Lock()
					countFail++
					locker.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	t.Log("success:", countSuccess, "fail:", countFail, "qps:", int(float64(countSuccess+countFail)/time.Since(beforeTime).Seconds()))
}
