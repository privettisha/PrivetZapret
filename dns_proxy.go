package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const dnsMessageType = "application/dns-message"

// validateDoHURL accepts a standard RFC 8484 wire-format endpoint, including
// a private query token in its path or query string.
func validateDoHURL(raw string) (string, error) {
	if raw == "" || len(raw) > 2048 || raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "\r\n\t ") {
		return "", errors.New("вставьте полную DoH-ссылку без пробелов")
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("нужна ссылка DoH с https://")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast()) {
		return "", errors.New("локальный адрес не подходит для DoH")
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return "", errors.New("локальный адрес не подходит для DoH")
	}
	if u.Port() != "" {
		return "", errors.New("используйте DoH на стандартном HTTPS-порту 443")
	}
	return u.String(), nil
}

func validDNSMessage(query, response []byte) bool {
	return len(query) >= 12 && len(response) >= 12 &&
		bytes.Equal(query[:2], response[:2]) && response[2]&0x80 != 0 &&
		response[2]&0x78 == 0 && query[2]&0x80 == 0
}

type dnsProxy struct {
	url      string
	fallback []string
	client   *http.Client
	udp      []net.PacketConn
	tcp      []net.Listener
	slots    chan struct{}
}

func newDNSProxy(rawURL string, original []string) (*dnsProxy, error) {
	endpoint, err := validateDoHURL(rawURL)
	if err != nil {
		return nil, err
	}
	servers := make([]string, 0, len(original))
	for _, value := range original {
		ip := net.ParseIP(value)
		if ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsMulticast() {
			servers = append(servers, net.JoinHostPort(ip.String(), "53"))
		}
	}
	if len(servers) == 0 {
		return nil, errors.New("у подключения нет исходных DNS-серверов для резервного режима")
	}
	proxy := &dnsProxy{url: endpoint, fallback: servers, slots: make(chan struct{}, 128)}
	transport := &http.Transport{
		TLSHandshakeTimeout: 3 * time.Second,
		IdleConnTimeout:     30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if net.ParseIP(host) != nil {
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, address)
			}
			for _, server := range servers {
				resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext(ctx, network, server)
				}}
				ips, err := resolver.LookupIPAddr(ctx, host)
				if err != nil {
					continue
				}
				for _, ip := range ips {
					conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
					if err == nil {
						return conn, nil
					}
				}
			}
			return nil, fmt.Errorf("не удалось соединиться с DoH-сервером %s", host)
		},
	}
	proxy.client = &http.Client{Transport: transport, Timeout: 4 * time.Second}
	return proxy, nil
}

func (p *dnsProxy) doh(ctx context.Context, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", dnsMessageType)
	req.Header.Set("Accept", dnsMessageType)
	response, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || contentType != dnsMessageType {
		return nil, fmt.Errorf("DoH ответил HTTP %d", response.StatusCode)
	}
	message, err := io.ReadAll(io.LimitReader(response.Body, 65536))
	if err != nil || !validDNSMessage(query, message) || len(message) > 65535 {
		return nil, errors.New("DoH вернул некорректный ответ")
	}
	return message, nil
}

func (p *dnsProxy) ordinary(ctx context.Context, query []byte) ([]byte, error) {
	for _, server := range p.fallback {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "udp", server)
		if err != nil {
			continue
		}
		_ = conn.SetDeadline(dnsDeadline(ctx))
		_, err = conn.Write(query)
		answer := make([]byte, 65535)
		var n int
		if err == nil {
			n, err = conn.Read(answer)
		}
		_ = conn.Close()
		if err == nil && validDNSMessage(query, answer[:n]) {
			if answer[2]&0x02 == 0 {
				return answer[:n], nil
			}
		}
		// Retry over TCP when UDP is blocked or the DNS reply is truncated.
		if tcpAnswer, err := ordinaryTCP(ctx, server, query); err == nil && validDNSMessage(query, tcpAnswer) {
			return tcpAnswer, nil
		}
	}
	return nil, errors.New("DNS-серверы подключения не ответили")
}

func ordinaryTCP(ctx context.Context, server string, query []byte) ([]byte, error) {
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(dnsDeadline(ctx))
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(query)))
	if _, err := conn.Write(append(size[:], query...)); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return nil, err
	}
	answer := make([]byte, int(binary.BigEndian.Uint16(size[:])))
	_, err = io.ReadFull(conn, answer)
	return answer, err
}

func dnsDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(1500 * time.Millisecond)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

func (p *dnsProxy) resolve(query []byte) []byte {
	if len(query) < 12 || query[2]&0x80 != 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	answer, err := p.doh(ctx, query)
	cancel()
	if err == nil {
		return answer
	}
	ctx, cancel = context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	answer, _ = p.ordinary(ctx, query)
	return answer
}

func (p *dnsProxy) listen(ipv6 bool) error {
	addresses := []string{"127.0.0.1:53"}
	if ipv6 {
		addresses = append(addresses, "[::1]:53")
	}
	for _, address := range addresses {
		udp, err := net.ListenPacket("udp", address)
		if err != nil {
			p.Close()
			return fmt.Errorf("порт DNS %s занят: %w", address, err)
		}
		p.udp = append(p.udp, udp)
		tcp, err := net.Listen("tcp", address)
		if err != nil {
			p.Close()
			return fmt.Errorf("порт DNS %s занят: %w", address, err)
		}
		p.tcp = append(p.tcp, tcp)
	}
	for _, conn := range p.udp {
		go p.serveUDP(conn)
	}
	for _, listener := range p.tcp {
		go p.serveTCP(listener)
	}
	return nil
}

func (p *dnsProxy) serveUDP(conn net.PacketConn) {
	buffer := make([]byte, 65535)
	for {
		n, address, err := conn.ReadFrom(buffer)
		if err != nil {
			return
		}
		query := append([]byte(nil), buffer[:n]...)
		if !p.acquire() {
			continue
		}
		go func() {
			defer p.release()
			if answer := p.resolve(query); len(answer) != 0 {
				_, _ = conn.WriteTo(answer, address)
			}
		}()
	}
}

func (p *dnsProxy) serveTCP(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if !p.acquire() {
			_ = conn.Close()
			continue
		}
		go func() {
			defer p.release()
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			var size [2]byte
			for {
				if _, err := io.ReadFull(conn, size[:]); err != nil {
					return
				}
				query := make([]byte, int(binary.BigEndian.Uint16(size[:])))
				if _, err := io.ReadFull(conn, query); err != nil {
					return
				}
				answer := p.resolve(query)
				if len(answer) == 0 {
					return
				}
				binary.BigEndian.PutUint16(size[:], uint16(len(answer)))
				if _, err := conn.Write(append(size[:], answer...)); err != nil {
					return
				}
			}
		}()
	}
}

func (p *dnsProxy) acquire() bool {
	if p.slots == nil {
		return true
	}
	select {
	case p.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (p *dnsProxy) release() {
	if p.slots != nil {
		<-p.slots
	}
}

func (p *dnsProxy) Close() {
	for _, conn := range p.udp {
		_ = conn.Close()
	}
	for _, listener := range p.tcp {
		_ = listener.Close()
	}
	if transport, ok := p.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}
