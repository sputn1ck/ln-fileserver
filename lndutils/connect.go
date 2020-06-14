package lndutils

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/macaroon.v2"
	"net/url"
)

// NewLndConnectClient uses a lndconnect uri string, containing
// host, macaroon and optional credentials to connect to a lnd
// node.
func NewLndConnectClient(ctx context.Context, lndconnect string) (lnrpc.LightningClient, *grpc.ClientConn, error) {
	uri := &url.URL{}
	uri, err := uri.Parse(lndconnect)
	if err != nil {
		return nil, nil, err
	}

	address, mac, tlsCreds, err := LndConnect(uri)
	if err != nil {
		return nil, nil, err
	}

	macCred := NewMacaroonCredential(mac)

	dialOpts := []grpc.DialOption{
		grpc.WithPerRPCCredentials(macCred),
		grpc.WithBlock(),
	}

	if tlsCreds != nil {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(*tlsCreds))
	}

	conn, err := grpc.DialContext(ctx, address, dialOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to connect to RPC server: %v", err)
	}

	return lnrpc.NewLightningClient(conn), conn, nil
}

// MacaroonCredential wraps a macaroon to implement the
// credentials.PerRPCCredentials interface.
type MacaroonCredential struct {
	*macaroon.Macaroon
}

// RequireTransportSecurity implements the PerRPCCredentials interface.
func (m MacaroonCredential) RequireTransportSecurity() bool {
	return true
}

// GetRequestMetadata implements the PerRPCCredentials interface. This method
// is required in order to pass the wrapped macaroon into the gRPC context.
// With this, the macaroon will be available within the request handling scope
// of the ultimate gRPC server implementation.
func (m MacaroonCredential) GetRequestMetadata(ctx context.Context,
	uri ...string) (map[string]string, error) {

	macBytes, err := m.MarshalBinary()
	if err != nil {
		return nil, err
	}

	md := make(map[string]string)
	md["macaroon"] = hex.EncodeToString(macBytes)
	return md, nil
}

// NewMacaroonCredential returns a copy of the passed macaroon wrapped in a
// MacaroonCredential struct which implements PerRPCCredentials.
func NewMacaroonCredential(m *macaroon.Macaroon) MacaroonCredential {
	ms := MacaroonCredential{}
	ms.Macaroon = m.Clone()
	return ms
}

// LndConnect takes a lndconnect uri
// (https://github.com/LN-Zap/lndconnect/blob/master/lnd_connect_uri.md)
// and parses it into the address the macaroon and optionally
// the credentials, if provided.
func LndConnect(uri *url.URL) (string, *macaroon.Macaroon, *credentials.TransportCredentials, error) {
	var address string
	address = uri.Host

	macar := &macaroon.Macaroon{}
	if mac, ok := uri.Query()["macaroon"]; ok {
		if len(mac) != 1 {
			return "", nil, nil, fmt.Errorf("unable to get macaroon from uri")
		}
		macBytes, err := base64.RawURLEncoding.DecodeString(mac[0])
		if err != nil {
			return "", nil, nil, fmt.Errorf("unable to decode base64 macaroon: %v", err)
		}
		err = macar.UnmarshalBinary(macBytes)
		if err != nil {
			return "", nil, nil, fmt.Errorf("unable to unmarshal binary macaroon: %v", err)
		}
	}

	var creds credentials.TransportCredentials
	if cert, ok := uri.Query()["cert"]; ok {
		switch len(cert) {
		case 0:
			break
		case 1:
			certStr, err := reconstructCertFromUrlBase(cert[0])
			if err != nil {
				return "", nil, nil, fmt.Errorf("unable to decode url base: %v", err)
			}
			certBytes := []byte(certStr)
			pool := x509.NewCertPool()
			if ok := pool.AppendCertsFromPEM(certBytes); !ok {
				return "", nil, nil, fmt.Errorf("unable to append pem cert: %v", ok)
			}
			creds = credentials.NewClientTLSFromCert(pool, "")
		default:
			return "", nil, nil, fmt.Errorf("expected len 1 or 0")
		}
	}
	return address, macar, &creds, nil
}

func reconstructCertFromUrlBase(str string) (string, error) {
	out, err := base64UrlToBase64(str)
	if err != nil {
		return "", nil
	}

	lines := int(len(out) / 64)
	for i := 1; i <= lines; i++ {
		out = out[:(i*64)+(i-1)] + "\n" + out[(i*64)+(i-1):]
	}

	return fmt.Sprintf("\n-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----\n", out), nil
}

func base64UrlToBase64(str string) (string, error) {
	urlBase, err := base64.RawURLEncoding.DecodeString(str)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(urlBase), nil

}
