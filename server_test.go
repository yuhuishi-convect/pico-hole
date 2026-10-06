package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Fragment reads and writes to exercise TCP stream boundaries and short writes.
type testConn struct {
	input  *strings.Reader
	output bytes.Buffer
	closed bool
}

func TestStatusEndpoint(t *testing.T) {
	c := &testConn{input: strings.NewReader("GET /api/status HTTP/1.1\r\nHost: pico\r\n\r\n")}
	if err := serveHTTP(c); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(&c.output), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var snapshot dashboardSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.HTTPStatus == "" || snapshot.DNSStatus == "" || snapshot.WiFiStatus == "" {
		t.Fatalf("status endpoint omitted service states: %+v", snapshot)
	}
}

func (c *testConn) Read(p []byte) (int, error)  { return c.input.Read(p[:min(7, len(p))]) }
func (c *testConn) Write(p []byte) (int, error) { return c.output.Write(p[:min(11, len(p))]) }
func (c *testConn) Close() error                { c.closed = true; return nil }
func (c *testConn) SetDeadline(time.Time) error { return nil }

func TestHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, method, target string
		status               int
		body                 string
	}{
		{"home", "GET", "/", 200, string(webPage)},
		{"index", "GET", "/index.html", 200, string(webPage)},
		{"query", "GET", "/?refresh=1", 200, string(webPage)},
		{"head", "HEAD", "/", 200, ""},
		{"missing", "GET", "/missing", 404, "Not found\n"},
		{"missing-head", "HEAD", "/missing", 404, ""},
		{"method", "POST", "/", 405, "Use GET or HEAD\n"},
		{"favicon", "GET", "/favicon.ico", 204, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &testConn{input: strings.NewReader(tc.method + " " + tc.target + " HTTP/1.1\r\nHost: pico\r\n\r\n")}
			if err := serveHTTP(c); err != nil {
				t.Fatal(err)
			}
			resp, err := http.ReadResponse(bufio.NewReader(&c.output), &http.Request{Method: tc.method})
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.status || string(body) != tc.body {
				t.Fatalf("status=%d body=%q", resp.StatusCode, body)
			}
			if !c.closed || !resp.Close {
				t.Fatal("connection was not closed")
			}
			if tc.method == "HEAD" && tc.status == 200 && resp.ContentLength != int64(len(webPage)) {
				t.Fatal("HEAD length does not match GET")
			}
			if tc.status == 405 && resp.Header.Get("Allow") != "GET, HEAD" {
				t.Fatal("missing Allow header")
			}
		})
	}
}

func TestBadHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, request string
		status        int
	}{
		{"malformed", "not a valid request\r\n\r\n", 400},
		{"oversized", "GET / HTTP/1.1\r\nX-Large: " + strings.Repeat("a", maxHeaderBytes) + "\r\n\r\n", 431},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &testConn{input: strings.NewReader(tc.request)}
			if err := serveHTTP(c); err != nil {
				t.Fatal(err)
			}
			resp, err := http.ReadResponse(bufio.NewReader(&c.output), nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status=%d", resp.StatusCode)
			}
		})
	}
}

func TestIncompleteHeaders(t *testing.T) {
	c := &testConn{input: strings.NewReader("GET / HTTP/1.1\r\nHost: pico\r\n")}
	if err := serveHTTP(c); err == nil {
		t.Fatal("incomplete request should fail")
	}
	if !c.closed || c.output.Len() != 0 {
		t.Fatal("incomplete request served a page or leaked connection")
	}
}
