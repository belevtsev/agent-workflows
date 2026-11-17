package pilot

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain supplies the isolated runner's API tunnel without introducing a
// production execution path. TLS remains end to end; no prompts are inspected.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && strings.HasPrefix(os.Args[1], "pilot-bridge-") {
		var err error
		switch os.Args[1] {
		case "pilot-bridge-serve":
			err = serveAPIProxy(os.Args[2])
		case "pilot-bridge-run":
			err = runAPIProxy(os.Args[2], os.Args[3:])
		default:
			err = errors.New("unknown pilot bridge mode")
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			var exited *exec.ExitError
			if errors.As(err, &exited) {
				os.Exit(exited.ExitCode())
			}
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func providerAddress(address string) bool {
	switch strings.ToLower(address) {
	case "api.openai.com:443", "chatgpt.com:443", "auth.openai.com:443", "ab.chatgpt.com:443":
		return true
	}
	return false
}

func serveAPIProxy(socket string) error {
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(socket, 0o600); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "pilot API proxy ready")
	for {
		client, err := listener.Accept()
		if err != nil {
			return err
		}
		go forwardAPI(client)
	}
}

func forwardAPI(client net.Conn) {
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReader(io.LimitReader(client, 16<<10))
	request, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	address := strings.ToLower(request.RequestURI)
	if request.Method != "CONNECT" || !providerAddress(address) {
		_, _ = io.WriteString(client, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
		return
	}
	remote, err := dialProvider(address)
	if err != nil {
		// Neither headers nor upstream proxy credentials belong in evidence logs.
		_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer remote.Close()
	_ = client.SetReadDeadline(time.Time{})
	_, _ = io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n")
	if n := reader.Buffered(); n > 0 {
		initial := make([]byte, n)
		if _, err := io.ReadFull(reader, initial); err != nil {
			return
		}
		if _, err := remote.Write(initial); err != nil {
			return
		}
	}
	spliceAPI(client, remote)
}

func dialProvider(address string) (net.Conn, error) {
	request, err := http.NewRequest("CONNECT", "https://"+address, nil)
	if err != nil {
		return nil, err
	}
	proxy, err := http.ProxyFromEnvironment(request)
	if err != nil {
		return nil, err
	}
	if proxy == nil {
		return net.DialTimeout("tcp", address, 20*time.Second)
	}
	if proxy.Scheme != "http" {
		return nil, errors.New("pilot bridge supports HTTP upstream CONNECT proxies")
	}
	host := proxy.Host
	if proxy.Port() == "" {
		host = net.JoinHostPort(proxy.Hostname(), "80")
	}
	connection, err := net.DialTimeout("tcp", host, 20*time.Second)
	if err != nil {
		return nil, err
	}
	headers := "CONNECT " + address + " HTTP/1.1\r\nHost: " + address + "\r\n"
	if proxy.User != nil {
		password, _ := proxy.User.Password()
		headers += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(proxy.User.Username()+":"+password)) + "\r\n"
	}
	if _, err := io.WriteString(connection, headers+"\r\n"); err != nil {
		connection.Close()
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), request)
	if err != nil || response.StatusCode != http.StatusOK {
		connection.Close()
		return nil, errors.New("upstream proxy rejected provider CONNECT")
	}
	return connection, nil
}

func spliceAPI(a, b net.Conn) {
	var copies sync.WaitGroup
	copyOne := func(dst, src net.Conn) {
		defer copies.Done()
		_, _ = io.Copy(dst, src)
		if half, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = half.CloseWrite()
		}
	}
	copies.Add(2)
	go copyOne(a, b)
	go copyOne(b, a)
	copies.Wait()
}

func runAPIProxy(socket string, argv []string) error {
	if len(argv) == 0 {
		return errors.New("pilot bridge run requires a command")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	proxy := "http://" + listener.Addr().String()
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer client.Close()
				remote, err := net.Dial("unix", socket)
				if err != nil {
					return
				}
				defer remote.Close()
				spliceAPI(client, remote)
			}()
		}
	}()
	child := exec.CommandContext(context.Background(), argv[0], argv[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			continue
		}
		child.Env = append(child.Env, pair)
	}
	child.Env = append(child.Env, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy,
		"http_proxy="+proxy, "https_proxy="+proxy, "NO_PROXY=localhost,127.0.0.1", "no_proxy=localhost,127.0.0.1")
	return child.Run()
}

func TestAPIProxyBlocksSolutionFetches(t *testing.T) {
	for _, request := range []string{
		"CONNECT github.com:443 HTTP/1.1\r\nHost: github.com:443\r\n\r\n",
		"CONNECT api.openai.com.evil.invalid:443 HTTP/1.1\r\nHost: api.openai.com.evil.invalid\r\n\r\n",
		"GET https://api.openai.com/ HTTP/1.1\r\nHost: api.openai.com\r\n\r\n",
	} {
		client, server := net.Pipe()
		go forwardAPI(server)
		_ = client.SetDeadline(time.Now().Add(time.Second))
		if _, err := io.WriteString(client, request); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(bufio.NewReader(client), nil)
		client.Close()
		if err != nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("non-provider request escaped isolation: response=%v error=%v", response, err)
		}
	}
}
