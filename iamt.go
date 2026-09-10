// Package iamt manages Intel AMT devices over WS-Management.
//
// WS-Man messages are built and parsed by
// github.com/device-management-toolkit/go-wsman-messages, Intel's own message
// library, which covers the full AMT, CIM and IPS class surface. Transport and
// authentication are this package's own: see internal/digest for why.
package iamt

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/device-management-toolkit/go-wsman-messages/v2/pkg/wsman"
	wsmanclient "github.com/device-management-toolkit/go-wsman-messages/v2/pkg/wsman/client"
	"github.com/go-logr/logr"
	"github.com/jacobweinstock/iamt/internal/digest"
)

// Default AMT WS-Man ports.
const (
	// PortPlaintext is the plaintext WS-Man port.
	PortPlaintext uint32 = 16992
	// PortTLS is the TLS WS-Man port.
	PortTLS uint32 = 16993
)

// DefaultTimeout bounds each operation. AMT firmware is slow to wake, so this
// is deliberately generous.
const DefaultTimeout = 30 * time.Second

// Client used to perform actions on the machine.
type Client struct {
	Host   string
	Logger logr.Logger
	Pass   string
	Path   string
	Port   uint32
	Scheme string
	User   string

	// Timeout bounds each operation. Zero means DefaultTimeout.
	Timeout time.Duration

	// PinnedCert, when set, is the hex-encoded SHA-256 of the device's DER
	// leaf certificate. A device presenting anything else is refused.
	//
	// AMT ships a self-signed certificate that no chain can validate, so
	// pinning is what makes a TLS connection trustworthy rather than merely
	// encrypted.
	PinnedCert string

	transport *digest.Transport
	msg       wsman.Messages
}

// Option for setting optional Client values.
type Option func(*Client)

// WithScheme sets the URL scheme, "http" or "https".
func WithScheme(scheme string) Option {
	return func(c *Client) {
		c.Scheme = scheme
	}
}

// WithLogger sets the logger.
func WithLogger(logger logr.Logger) Option {
	return func(c *Client) {
		c.Logger = logger
	}
}

// WithPort sets the port used to reach the device.
func WithPort(port uint32) Option {
	return func(c *Client) {
		c.Port = port
	}
}

// WithPath sets the WS-Man request path.
func WithPath(path string) Option {
	return func(c *Client) {
		c.Path = path
	}
}

// WithTimeout bounds each operation.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.Timeout = timeout
	}
}

// WithPinnedCert pins the device's TLS certificate by its hex-encoded SHA-256
// fingerprint. See Client.PinnedCert.
func WithPinnedCert(fingerprint string) Option {
	return func(c *Client) {
		c.PinnedCert = fingerprint
	}
}

// NewClient creates an amt client to use.
func NewClient(host, user, passwd string, opts ...Option) *Client {
	defaultClient := &Client{
		Logger: logr.Discard(),
		Path:   "/wsman",
		Port:   PortPlaintext,
		Scheme: "http",
	}

	for _, opt := range opts {
		opt(defaultClient)
	}

	defaultClient.Host = host
	defaultClient.User = user
	defaultClient.Pass = passwd

	useTLS := strings.EqualFold(defaultClient.Scheme, "https")

	defaultClient.transport = &digest.Transport{
		Base:     baseTransport(useTLS, defaultClient.PinnedCert),
		Username: user,
		Password: passwd,
		Port:     defaultClient.Port,
		Path:     defaultClient.Path,
	}

	// UseDigest is false on purpose: authentication is handled by the
	// transport above, whose challenge parser copes with the malformed
	// WWW-Authenticate headers some Intel NUCs emit. go-wsman-messages' own
	// parser silently mis-reads them.
	defaultClient.msg = wsman.NewMessages(wsmanclient.Parameters{
		Target:            host,
		Username:          user,
		Password:          passwd,
		UseDigest:         false,
		UseTLS:            useTLS,
		SelfSignedAllowed: true,
		Transport:         defaultClient.transport,
		Timeout:           defaultClient.timeout(),
	})

	return defaultClient
}

func (c *Client) timeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

// endpoint is the WS-Man URL for this client.
func (c *Client) endpoint() string {
	scheme := c.Scheme
	if scheme == "" {
		scheme = "http"
	}
	path := c.Path
	if path == "" {
		path = "/wsman"
	}
	return scheme + "://" + net.JoinHostPort(c.Host, strconv.FormatUint(uint64(c.Port), 10)) + path
}

// Open the client.
//
// It confirms the endpoint is an AMT WS-Man service by requiring a digest
// challenge, and caches that challenge so subsequent calls skip a round trip.
// It does not verify credentials; a wrong password surfaces on the first real
// operation.
func (c *Client) Open(ctx context.Context) error {
	return c.transport.Prime(ctx, c.endpoint())
}

// Close the client.
func (c *Client) Close(_ context.Context) error {
	c.transport.Reset()
	return nil
}

// baseTransport builds the underlying HTTP transport.
//
// Supplying a transport to go-wsman-messages bypasses the TLS configuration it
// would otherwise build, so the TLS settings have to be established here.
func baseTransport(useTLS bool, pinnedCert string) http.RoundTripper {
	if !useTLS {
		return &http.Transport{
			MaxIdleConns:    2,
			IdleConnTimeout: 90 * time.Second,
		}
	}

	cfg := &tls.Config{
		// AMT's factory certificate is self-signed, so chain verification can
		// never pass. Trust comes from the pin below when one is configured.
		InsecureSkipVerify: true, //nolint:gosec // see PinnedCert
	}
	if pinnedCert != "" {
		cfg.VerifyPeerCertificate = digest.PinnedCertVerifier(pinnedCert)
	}

	return &http.Transport{
		MaxIdleConns:    2,
		IdleConnTimeout: 90 * time.Second,
		TLSClientConfig: cfg,
	}
}

// Fingerprint dials the device and returns the hex SHA-256 of its leaf
// certificate, without authenticating.
//
// It is how a fingerprint is learned on first contact so it can be pinned for
// every subsequent connection.
func (c *Client) Fingerprint(ctx context.Context) (string, error) {
	if !strings.EqualFold(c.Scheme, "https") {
		return "", fmt.Errorf("iamt: a plaintext endpoint presents no certificate")
	}

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: c.timeout()},
		Config:    &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // reading the cert is the point
	}
	addr := net.JoinHostPort(c.Host, strconv.FormatUint(uint64(c.Port), 10))

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("iamt: dialing %s: %w", addr, err)
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return "", fmt.Errorf("iamt: dialer did not return a TLS connection")
	}
	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", fmt.Errorf("iamt: device presented no certificate")
	}
	return digest.Fingerprint(certs[0].Raw), nil
}
