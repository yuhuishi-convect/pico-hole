package main

import (
	_ "embed"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/soypat/lneto/http/httpraw"
)

//go:embed index.html
var webPage []byte

const maxHeaderBytes = 2048

// tcp.Conn and ordinary host connections both satisfy this interface.
type httpConn interface {
	io.ReadWriteCloser
	SetDeadline(time.Time) error
}

// serveHTTP handles one request, then closes the connection to keep RAM bounded.
func serveHTTP(conn httpConn) error {
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	var storage [maxHeaderBytes]byte
	var hdr httpraw.HeaderV1
	hdr.Reset(storage[:0], 32)
	hdr.ConfigBufferGrowth(false)
	for {
		free := hdr.BufferFree()
		if free == 0 {
			return respond(conn, "431 Request Header Fields Too Large", "text/plain", []byte("Request headers too large\n"), false, "")
		}
		_, readErr := hdr.ReadFromLimited(conn, min(free, 256))
		needMore, parseErr := hdr.TryParse(false)
		if !needMore {
			if parseErr != nil {
				return respond(conn, "400 Bad Request", "text/plain", []byte("Bad request\n"), false, "")
			}
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	protocol := string(hdr.Protocol())
	if protocol != "HTTP/1.1" && protocol != "HTTP/1.0" {
		return respond(conn, "400 Bad Request", "text/plain", []byte("Bad request\n"), false, "")
	}
	method := string(hdr.Method())
	headOnly := method == "HEAD"
	if method != "GET" && !headOnly {
		return respond(conn, "405 Method Not Allowed", "text/plain", []byte("Use GET or HEAD\n"), false, "Allow: GET, HEAD\r\n")
	}
	path, _, _ := strings.Cut(string(hdr.RequestTarget()), "?")
	if path != "/api/status" {
		serviceStatus.recordHTTPRequest()
	}
	switch path {
	case "/", "/index.html":
		return respond(conn, "200 OK", "text/html; charset=utf-8", webPage, headOnly, "")
	case "/api/status":
		return respond(conn, "200 OK", "application/json; charset=utf-8", dashboardJSON(), headOnly, "Cache-Control: no-store\r\n")
	case "/favicon.ico":
		return respond(conn, "204 No Content", "", nil, true, "")
	default:
		return respond(conn, "404 Not Found", "text/plain; charset=utf-8", []byte("Not found\n"), headOnly, "")
	}
}

func respond(w io.Writer, status, contentType string, body []byte, headOnly bool, extra string) error {
	header := "HTTP/1.1 " + status + "\r\nConnection: close\r\n"
	// A 204 response must not include Content-Length.
	if status != "204 No Content" {
		header += "Content-Length: " + strconv.Itoa(len(body)) + "\r\n"
	}
	if contentType != "" {
		header += "Content-Type: " + contentType + "\r\n"
	}
	if err := writeAll(w, []byte(header+extra+"\r\n")); err != nil {
		return err
	}
	if !headOnly {
		return writeAll(w, body)
	}
	return nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
