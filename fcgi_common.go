package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phuslu/log"
)

// Refer:
// 	https://fast-cgi.github.io/spec

const (
	// Listening socket file number
	FCGI_LISTENSOCK_FILENO = 0

	// Number of bytes in a FCGI_Header
	FCGI_HEADER_LEN = 8

	// Value for version component of FCGI_Header
	FCGI_VERSION_1 = 1

	// Values for type component of FCGI_Header
	FCGI_BEGIN_REQUEST     byte = 1
	FCGI_ABORT_REQUEST     byte = 2
	FCGI_END_REQUEST       byte = 3
	FCGI_PARAMS            byte = 4
	FCGI_STDIN             byte = 5
	FCGI_STDOUT            byte = 6
	FCGI_STDERR            byte = 7
	FCGI_DATA              byte = 8
	FCGI_GET_VALUES        byte = 9
	FCGI_GET_VALUES_RESULT byte = 10
	FCGI_UNKNOWN_TYPE      byte = 11
	FCGI_MAXTYPE                = FCGI_UNKNOWN_TYPE

	// Value for requestId component of FCGI_Header
	FCGI_NULL_REQUEST_ID = 0

	// Mask for flags component of FCGI_BeginRequestBody
	FCGI_KEEP_CONN byte = 1

	// Values for role component of FCGI_BeginRequestBody
	FCGI_RESPONDER  byte = 1
	FCGI_AUTHORIZER byte = 2
	FCGI_FILTER     byte = 3

	// Values for protocolStatus component of FCGI_EndRequestBody
	FCGI_REQUEST_COMPLETE byte = 0
	FCGI_CANT_MPX_CONN    byte = 1
	FCGI_OVERLOADED       byte = 2
	FCGI_UNKNOWN_ROLE     byte = 3

	// Variable names for FCGI_GET_VALUES / FCGI_GET_VALUES_RESULT records
	FCGI_MAX_CONNS  = "FCGI_MAX_CONNS"
	FCGI_MAX_REQS   = "FCGI_MAX_REQS"
	FCGI_MPXS_CONNS = "FCGI_MPXS_CONNS"
)

const (
	fcgiMaxWrite = 65500 // 65530 may work, but for compatibility
	fcgiMaxPad   = 255
)

var PAD = [255]byte{}

type FcgiHeader struct {
	Version       byte
	Type          byte
	RequestId     uint16
	ContentLength uint16
	PaddingLength byte
	Reserved      byte
	//ContentData []byte
	//PaddingData []byte
}

type FcgiNameValuePair struct {
	NameLength  uint16
	ValueLength uint16
	//NameData    []byte
	//ValueData   []byte
}

type FcgiUnknownTypeBody struct {
	recordType byte
	reserved   [7]byte
}

type FcgiBeginRequestBody struct {
	roleB1   byte
	roleB0   byte
	flags    byte
	reserved [5]byte
}

type FcgiEndRequestBody struct {
	appStatusB3    byte
	appStatusB2    byte
	appStatusB1    byte
	appStatusB0    byte
	protocolStatus byte
	reserved       [3]byte
}

var fcgiCurrentRequestId = uint16(0)
var fcgiRequestIdLocker = sync.Mutex{}

var contentLengthRegexp = regexp.MustCompile(`^\d+$`)

// FcgiRequest Referer:
//   - FastCGI Specification: http://www.mit.edu/~yandros/doc/specs/fcgi-spec.html
type FcgiRequest struct {
	id         uint16
	keepAlive  bool
	timeout    time.Duration
	params     map[string]string
	body       io.Reader
	bodyLength uint32
}

var fcgiReqPool = sync.Pool{
	New: func() any {
		return &FcgiRequest{}
	},
}

func NewFcgiRequest() *FcgiRequest {
	// req := fcgiReqPool.Get().(*FcgiRequest)
	// req.Reset()
	// defer fcgiReqPool.Put(req)

	req := &FcgiRequest{}
	req.id = req.nextId()
	req.keepAlive = false
	return req
}

func (r *FcgiRequest) Reset() {
	r.id = 0
	r.keepAlive = false
	r.timeout = 0
	r.params = make(map[string]string)
	r.body = nil
	r.bodyLength = 0
}

func (r *FcgiRequest) KeepAlive() {
	r.keepAlive = true
}

func (r *FcgiRequest) SetParams(params map[string]string) {
	r.params = params
}

func (r *FcgiRequest) SetParam(name, value string) {
	r.params[name] = value
}

func (r *FcgiRequest) SetBody(body io.Reader, length uint32) {
	r.body = body
	r.bodyLength = length
}

func (r *FcgiRequest) SetTimeout(timeout time.Duration) {
	r.timeout = timeout
}

func (r *FcgiRequest) CallOn(conn io.ReadWriter) (resp *http.Response, stderr []byte, err error) {
	err = r.writeBeginRequest(conn)
	if err != nil {
		return nil, nil, err
	}

	err = r.writeParams(conn)
	if err != nil {
		return nil, nil, err
	}

	err = r.writeStdin(conn)
	if err != nil {
		return nil, nil, err
	}

	return r.readStdout(conn)
}

func (r *FcgiRequest) writeBeginRequest(conn io.Writer) error {
	flags := byte(0)
	if r.keepAlive {
		flags = FCGI_KEEP_CONN
	}
	role := FCGI_RESPONDER
	data := [8]byte{byte(role >> 8), byte(role), flags}
	return r.writeRecord(conn, FCGI_BEGIN_REQUEST, data[:])
}

func (r *FcgiRequest) writeParams(conn io.Writer) error {
	// check CONTENT_LENGTH
	if r.body != nil {
		contentLength, found := r.params["CONTENT_LENGTH"]
		if !found || !contentLengthRegexp.MatchString(contentLength) {
			if r.bodyLength > 0 {
				r.params["CONTENT_LENGTH"] = fmt.Sprintf("%d", r.bodyLength)
			} else {
				return errors.New("[fcgi]'CONTENT_LENGTH' should be specified")
			}
		}
	}

	buf := fcgiBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer fcgiBufPool.Put(buf)

	b := make([]byte, 8)
	nn := 0

	// init headers
	buf.Write(b)
	for name, value := range r.params {
		m := 8 + len(name) + len(value)
		if m > fcgiMaxWrite {
			// param data size exceed 65535 bytes"
			vl := fcgiMaxWrite - 8 - len(name)
			value = value[:vl]
		}

		n := encodeSize(b, uint32(len(name)))
		n += encodeSize(b[n:], uint32(len(value)))
		m = n + len(name) + len(value)
		if (nn + m) > fcgiMaxWrite {
			if err := r.writeRecord(conn, FCGI_PARAMS, buf.Bytes()); err != nil {
				return err
			}
			// reset headers
			buf.Write(b)
			nn = 0
		}
		nn += m
		buf.Write(b[:n])
		buf.WriteString(name)
		buf.WriteString(value)
	}

	err := r.writeRecord(conn, FCGI_PARAMS, buf.Bytes())
	if err != nil {
		return err
	}

	// write end
	return r.writeRecord(conn, FCGI_PARAMS, []byte{})
}

func (r *FcgiRequest) writeStdin(conn io.Writer) error {
	if r.body != nil {
		// read body with buffer
		buf := make([]byte, 1024*8)
		for {
			n, err := r.body.Read(buf)

			if n > 0 {
				err := r.writeRecord(conn, FCGI_STDIN, buf[:n])
				if err != nil {
					return err
				}
			}

			if err != nil {
				break
			}
		}
	}

	return r.writeRecord(conn, FCGI_STDIN, []byte{})
}

var fcgiBufPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(nil)
	},
}

func (r *FcgiRequest) writeRecord(conn io.Writer, recordType byte, contentData []byte) (err error) {
	contentLength := len(contentData)

	// write header
	header := &FcgiHeader{
		Version:       FCGI_VERSION_1,
		Type:          recordType,
		RequestId:     r.id,
		ContentLength: uint16(contentLength),
		PaddingLength: byte(-contentLength & 7),
	}

	buf := fcgiBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer fcgiBufPool.Put(buf)

	if err = binary.Write(buf, binary.BigEndian, header); err != nil {
		return err
	}

	if _, err = buf.Write(contentData); err != nil {
		return err
	}

	if _, err = buf.Write(PAD[:header.PaddingLength]); err != nil {
		return err
	}

	if _, err = buf.WriteTo(conn); err != nil {
		return ErrClientDisconnect
	}

	return nil
}

var fcgiSrPool = sync.Pool{
	New: func() any {
		return new(fcgiStreamReader)
	},
}

func (r *FcgiRequest) readStdout(conn io.Reader) (resp *http.Response, stderr []byte, err error) {
	stdout := bytes.NewBuffer(nil)

	for {
		respHeader := FcgiHeader{}
		if err = binary.Read(conn, binary.BigEndian, &respHeader); err != nil {
			return nil, nil, ErrClientDisconnect
		}

		// check request id
		if respHeader.RequestId != r.id {
			continue
		}

		b := make([]byte, respHeader.ContentLength+uint16(respHeader.PaddingLength))
		if _, err = conn.Read(b); err != nil {
			log.Error().Err(err).Msg("read stdout")
			return nil, nil, ErrClientDisconnect
		}

		if respHeader.Type == FCGI_STDOUT {
			stdout.Write(b[:respHeader.ContentLength])
			continue
		}

		if respHeader.Type == FCGI_STDERR {
			stderr = append(stderr, b[:respHeader.ContentLength]...)
			continue
		}

		if respHeader.Type == FCGI_END_REQUEST {
			break
		}
	}

	if len(stderr) > 0 {
		return nil, stderr, errors.New("fcgi:" + string(stderr))
	}

	sr := fcgiSrPool.Get().(*fcgiStreamReader)
	sr.Reset(stdout)
	tp := textproto.NewReader(&sr.Reader)

	// Parse the response headers.
	mimeHeader, err := tp.ReadMIMEHeader()
	if err != nil && err != io.EOF {
		return
	}

	resp = new(http.Response)
	resp.Header = http.Header(mimeHeader)

	if resp.Header.Get("Status") != "" {
		statusNumber, statusInfo, statusIsCut := strings.Cut(resp.Header.Get("Status"), " ")
		if resp.StatusCode, err = strconv.Atoi(statusNumber); err != nil {
			return
		}
		if statusIsCut {
			resp.Status = statusInfo
		}
	} else {
		resp.StatusCode = http.StatusOK
	}

	// TODO: fixTransferEncoding ?
	resp.TransferEncoding = resp.Header["Transfer-Encoding"]
	resp.ContentLength, _ = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)

	resp.Body = sr

	return
}

func (r *FcgiRequest) nextId() uint16 {
	fcgiRequestIdLocker.Lock()
	defer fcgiRequestIdLocker.Unlock()

	fcgiCurrentRequestId++

	if fcgiCurrentRequestId == math.MaxUint16 {
		fcgiCurrentRequestId = 0
	}

	return fcgiCurrentRequestId
}

type fcgiStreamReader struct {
	bufio.Reader
}

func (sr *fcgiStreamReader) Close() error {
	fcgiSrPool.Put(sr)
	return nil
}

func encodeSize(b []byte, size uint32) int {
	if size > 127 {
		size |= 1 << 31
		binary.BigEndian.PutUint32(b, size)
		return 4
	}
	b[0] = byte(size)
	return 1
}
