package main

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/phuslu/log"
)

var ErrClientDisconnect = errors.New("lost connection to server")

type FcgiClient struct {
	isFree      bool
	isAvailable bool

	keepAlive bool

	network string
	address string
	conn    net.Conn

	locker sync.Mutex

	expireTime   time.Time
	expireLocker sync.Mutex

	mock bool
}

func NewFcgiClient(network string, address string) *FcgiClient {
	client := &FcgiClient{
		isFree:      true,
		isAvailable: false,
		network:     network,
		address:     address,
		expireTime:  time.Now().Add(86400 * time.Second),
	}

	// deal with expireTime
	go func() {
		for {
			time.Sleep(1 * time.Second)
			if time.Since(client.expireTime) > 0 {
				_ = client.conn.Close()

				client.expireLocker.Lock()
				client.expireTime = time.Now().Add(86400 * time.Second)
				client.expireLocker.Unlock()
			}
		}
	}()
	return client
}

func (c *FcgiClient) KeepAlive() {
	c.keepAlive = true
}

func (c *FcgiClient) Call(req *FcgiRequest) (resp *http.Response, stderr []byte, err error) {
	c.locker.Lock()
	c.isFree = false

	if c.keepAlive && c.conn == nil {
		err := c.Connect()
		if err != nil {
			c.locker.Unlock()
			return nil, nil, err
		}
	}

	if c.keepAlive {
		req.keepAlive = true
	}

	defer func() {
		if c.mock {
			time.Sleep(1 * time.Second)
		}
		c.isFree = true
		c.locker.Unlock()
	}()

	if c.conn == nil {
		return nil, nil, errors.New("no connection to server")
	}

	if req.timeout > 0 {
		c.beforeTime(req.timeout)
	}
	resp, stderr, err = req.CallOn(c.conn)
	c.endTime()

	// if lost connection, retry
	if err != nil {
		log.Error().Err(err).Msg("fcgi call")
		if errors.Is(err, ErrClientDisconnect) {
			// retry again
			c.Close()
			if err = c.Connect(); err == nil {
				if req.timeout > 0 {
					c.beforeTime(req.timeout)
				}
				resp, stderr, err = req.CallOn(c.conn)
				c.endTime()
			} else {
				log.Error().Err(err).Msg("fcgi call again")
				c.Close()
			}
		}
	}

	return resp, stderr, err
}

// Get issues a GET request to the fcgi responder.
func (c *FcgiClient) Get(req *FcgiRequest, body io.Reader, l int64) (resp *http.Response, stderr []byte, err error) {
	req.SetParam("REQUEST_METHOD", "GET")
	req.SetParam("CONTENT_LENGTH", strconv.FormatInt(l, 10))
	if l > 0 {
		req.SetBody(body, uint32(l))
	}

	return c.Call(req)
}

// Head issues a HEAD request to the fcgi responder.
func (c *FcgiClient) Head(req *FcgiRequest) (resp *http.Response, stderr []byte, err error) {
	req.SetParam("REQUEST_METHOD", "HEAD")
	req.SetParam("CONTENT_LENGTH", "0")

	return c.Call(req)
}

// Options issues an OPTIONS request to the fcgi responder.
func (c *FcgiClient) Options(req *FcgiRequest) (resp *http.Response, stderr []byte, err error) {
	req.SetParam("REQUEST_METHOD", "OPTIONS")
	req.SetParam("CONTENT_LENGTH", "0")

	return c.Call(req)
}

// Post issues a POST request to the fcgi responder. with request body
// in the format that bodyType specified
func (c *FcgiClient) Post(req *FcgiRequest, method string, bodyType string, body io.Reader, l int64) (resp *http.Response, stderr []byte, err error) {
	req.SetParam("REQUEST_METHOD", ToUpper(method))

	if len(req.params["REQUEST_METHOD"]) == 0 || req.params["REQUEST_METHOD"] == "GET" {
		req.SetParam("REQUEST_METHOD", "POST")
	}

	req.SetParam("CONTENT_LENGTH", strconv.FormatInt(l, 10))
	if len(bodyType) > 0 {
		req.SetParam("CONTENT_TYPE", bodyType)
	} else {
		req.SetParam("CONTENT_TYPE", "application/x-www-form-urlencoded")
	}

	req.SetBody(body, uint32(l))

	return c.Call(req)
}

// // PostForm issues a POST to the fcgi responder, with form
// // as a string key to a list values (url.Values)
// func (c *FcgiClient) PostForm(p map[string]string, data url.Values) (resp *http.Response, err error) {
// 	body := bytes.NewReader([]byte(data.Encode()))
// 	return c.Post(p, "POST", "application/x-www-form-urlencoded", body, int64(body.Len()))
// }

// // PostFile issues a POST to the fcgi responder in multipart(RFC 2046) standard,
// // with form as a string key to a list values (url.Values),
// // and/or with file as a string key to a list file path.
// func (c *FcgiClient) PostFile(p map[string]string, data url.Values, file map[string]string) (resp *http.Response, err error) {
// 	buf := &bytes.Buffer{}
// 	writer := multipart.NewWriter(buf)
// 	bodyType := writer.FormDataContentType()

// 	for key, val := range data {
// 		for _, v0 := range val {
// 			err = writer.WriteField(key, v0)
// 			if err != nil {
// 				return
// 			}
// 		}
// 	}

// 	for key, val := range file {
// 		fd, e := os.Open(val)
// 		if e != nil {
// 			return nil, e
// 		}
// 		defer fd.Close()

// 		part, e := writer.CreateFormFile(key, filepath.Base(val))
// 		if e != nil {
// 			return nil, e
// 		}
// 		_, err = io.Copy(part, fd)
// 		if err != nil {
// 			return
// 		}
// 	}

// 	err = writer.Close()
// 	if err != nil {
// 		return
// 	}

// 	return c.Post(p, "POST", bodyType, buf, int64(buf.Len()))
// }

// // SetReadTimeout sets the read timeout for future calls that read from the
// // fcgi responder. A zero value for t means no timeout will be set.
// func (c *FcgiClient) SetReadTimeout(t time.Duration) error {
// 	if t != 0 {
// 		return c.rwc.SetReadDeadline(time.Now().Add(t))
// 	}
// 	return nil
// }

// // SetWriteTimeout sets the write timeout for future calls that send data to
// // the fcgi responder. A zero value for t means no timeout will be set.
// func (c *FcgiClient) SetWriteTimeout(t time.Duration) error {
// 	if t != 0 {
// 		return c.rwc.SetWriteDeadline(time.Now().Add(t))
// 	}
// 	return nil
// }

func (c *FcgiClient) Close() {
	c.isAvailable = false
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
}

func (c *FcgiClient) Connect() error {
	c.isAvailable = false

	// @TODO set timeout
	conn, err := net.Dial(c.network, c.address)
	if err != nil {
		log.Error().Msg("[fcgi]" + err.Error())
		return err
	}

	c.conn = conn
	c.isAvailable = true

	return nil
}

func (c *FcgiClient) Mock() {
	c.mock = true
}

func (c *FcgiClient) beforeTime(timeout time.Duration) {
	c.expireLocker.Lock()
	defer c.expireLocker.Unlock()
	c.expireTime = time.Now().Add(timeout)
}

func (c *FcgiClient) endTime() {
	c.expireLocker.Lock()
	defer c.expireLocker.Unlock()
	c.expireTime = time.Now().Add(86400 * time.Second)
}
