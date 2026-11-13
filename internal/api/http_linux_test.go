// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// A full TCP accept backlog keeps the next connect in progress; the
// deadline must cover that phase too.
func TestPendingConnectTimesOut(t *testing.T) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Listen(fd, 0); err != nil {
		t.Fatal(err)
	}
	sa, err := syscall.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	addr := "127.0.0.1:" + strconv.Itoa(sa.(*syscall.SockaddrInet4).Port)

	// fill the backlog without accepting
	for {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			break
		}
		defer c.Close()
	}

	useClient(t, newHTTPClient(nil))
	start := time.Now()
	_, err = doRequest(context.Background(), http.MethodGet, "http://"+addr+"/", nil, "", 250*time.Millisecond)
	if !errors.Is(err, errTimeout) {
		t.Fatalf("a pending TCP connect must respect the deadline, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %v", elapsed)
	}
}
