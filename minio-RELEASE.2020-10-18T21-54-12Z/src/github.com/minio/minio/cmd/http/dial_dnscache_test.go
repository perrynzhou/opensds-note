/*
 * MinIO Cloud Storage, (C) 2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package http

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"testing"
	"time"
)

var (
	testFreq                 = 1 * time.Second
	testDefaultLookupTimeout = 1 * time.Second
)

func testDNSCache(t *testing.T) *DNSCache {
	t.Helper() // skip printing file and line information from this function
	return NewDNSCache(testFreq, testDefaultLookupTimeout)
}

func TestDialContextWithDNSCache(t *testing.T) {
	resolver := &DNSCache{
		cache: map[string][]string{
			"play.min.io": {
				"127.0.0.1",
				"127.0.0.2",
				"127.0.0.3",
			},
		},
	}

	cases := []struct {
		permF func(n int) []int
		dialF DialContext
	}{
		{
			permF: func(n int) []int {
				return []int{0}
			},
			dialF: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if got, want := addr, net.JoinHostPort("127.0.0.1", "443"); got != want {
					t.Fatalf("got addr %q, want %q", got, want)
				}
				return nil, nil
			},
		},
		{
			permF: func(n int) []int {
				return []int{1}
			},
			dialF: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if got, want := addr, net.JoinHostPort("127.0.0.2", "443"); got != want {
					t.Fatalf("got addr %q, want %q", got, want)
				}
				return nil, nil
			},
		},
		{
			permF: func(n int) []int {
				return []int{2}
			},
			dialF: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if got, want := addr, net.JoinHostPort("127.0.0.3", "443"); got != want {
					t.Fatalf("got addr %q, want %q", got, want)
				}
				return nil, nil
			},
		},
	}

	origFunc := randPerm
	defer func() {
		randPerm = origFunc
	}()

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			randPerm = tc.permF
			if _, err := DialContextWithDNSCache(resolver, tc.dialF)(context.Background(), "tcp", "play.min.io:443"); err != nil {
				t.Fatalf("err: %s", err)
			}
		})
	}

}

func TestDialContextWithDNSCacheRand(t *testing.T) {
	rand.Seed(time.Now().UTC().UnixNano())
	defer func() {
		rand.Seed(1)
	}()

	resolver := &DNSCache{
		cache: map[string][]string{
			"play.min.io": {
				"127.0.0.1",
				"127.0.0.2",
				"127.0.0.3",
			},
		},
	}

	count := make(map[string]int)
	dialF := func(ctx context.Context, network, addr string) (net.Conn, error) {
		count[addr]++
		return nil, nil
	}

	for i := 0; i < 100; i++ {
		if _, err := DialContextWithDNSCache(resolver, dialF)(context.Background(), "tcp", "play.min.io:443"); err != nil {
			t.Fatalf("err: %s", err)
		}
	}

	for _, c := range count {
		got := float32(c) / float32(100)
		if got < float32(0.2) {
			t.Fatalf("expected 0.2 rate got %f", got)
		}
	}
}

// Verify without port Dial fails, Go stdlib net.Dial expects port
func TestDialContextWithDNSCacheScenario1(t *testing.T) {
	resolver := testDNSCache(t)
	if _, err := DialContextWithDNSCache(resolver, nil)(context.Background(), "tcp", "play.min.io"); err == nil {
		t.Fatalf("expect to fail") // expected port
	}
}

// Verify if the host lookup function failed to return addresses
func TestDialContextWithDNSCacheScenario2(t *testing.T) {
	res := testDNSCache(t)
	originalFunc := res.lookupHostFn
	defer func() {
		res.lookupHostFn = originalFunc
	}()

	res.lookupHostFn = func(ctx context.Context, host string) ([]string, error) {
		return nil, fmt.Errorf("err")
	}

	if _, err := DialContextWithDNSCache(res, nil)(context.Background(), "tcp", "min.io:443"); err == nil {
		t.Fatalf("exect to fail")
	}
}

// Verify we always return the first error from net.Dial failure
func TestDialContextWithDNSCacheScenario3(t *testing.T) {
	resolver := &DNSCache{
		cache: map[string][]string{
			"min.io": {
				"1.1.1.1",
				"2.2.2.2",
				"3.3.3.3",
			},
		},
	}

	origFunc := randPerm
	randPerm = func(n int) []int {
		return []int{0, 1, 2}
	}
	defer func() {
		randPerm = origFunc
	}()

	want := errors.New("error1")
	dialF := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == net.JoinHostPort("1.1.1.1", "443") {
			return nil, want // first error should be returned
		}
		if addr == net.JoinHostPort("2.2.2.2", "443") {
			return nil, fmt.Errorf("error2")
		}
		if addr == net.JoinHostPort("3.3.3.3", "443") {
			return nil, fmt.Errorf("error3")
		}
		return nil, nil
	}

	_, got := DialContextWithDNSCache(resolver, dialF)(context.Background(), "tcp", "min.io:443")
	if got != want {
		t.Fatalf("got error %v, want %v", got, want)
	}
}
