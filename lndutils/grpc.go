package lndutils

import (
	"context"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"log"
	"strings"
)

var (
	errMissingMetadata  = status.Errorf(codes.InvalidArgument, "missing metadata")
	errInvalidSignature = status.Errorf(codes.Unauthenticated, "invalid signature")
	errMissingPubkey    = status.Errorf(codes.InvalidArgument, "missing pubkey in metadata")
	errMissingSig       = status.Errorf(codes.InvalidArgument, "missing sig in metadata")
)

type contextKey string

const (
	authKeyIsPublic = contextKey("auth-key")
	lndPubkey       = contextKey("pubkey")
	AuthMsg         = "lndprivatefileserver"
)

// VerificationClient is a minimalistic lnrpc.LigningClient that is able
// to perform the VerifyMessage api request.
type VerificationClient interface {
	//VerifyMessage verifies a signature over a msg. The signature must be
	//zbase32 encoded and signed by an active node in the resident node's
	//channel database. In addition to returning the validity of the signature,
	//VerifyMessage also returns the recovered pubkey from the signature.
	VerifyMessage(ctx context.Context, in *lnrpc.VerifyMessageRequest, opts ...grpc.CallOption) (*lnrpc.VerifyMessageResponse, error)
}

// GPRCUtils groups usefull lnd utility functions.
type GPRCUtils struct {
	vc VerificationClient
}

// New returns new lnd utils
func New(vc VerificationClient) *GPRCUtils {
	return &GPRCUtils{vc: vc}
}

// UnaryServerAuthenticationInterceptor checks if a signed message was
// signed by provided lnd node. To check if the provided pubkey is the
// identity of origin, the origin has to provide the pubkey as well as
// a signed message in the metadata of the request.
// {
//	"pubkey": the-nodes-62-chars-long-pubkey (string),
//	"sig": the-message-signed-by-node (string)
// }
func (u *GPRCUtils) UnaryServerAuthenticationInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Skip the authentication if the requested method is public
	isPublic, _ := ctx.Value(authKeyIsPublic).(bool)
	if isPublic {
		return handler(ctx, req)
	}

	// Authentication
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, errMissingMetadata
	}

	pubkey, ok := getPubkey(md)
	if !ok {
		return nil, errMissingPubkey
	}

	sig, ok := getSig(md)
	if !ok {
		return nil, errMissingSig
	}

	ok, err := u.valid(ctx, pubkey, sig)
	if err != nil {
		log.Printf("\t [LND UTILS] > unable to process signature: %v", err)
		return nil, err
	}
	if !ok {
		return nil, errInvalidSignature
	}

	return handler(context.WithValue(ctx, lndPubkey, pubkey), req)
}

// StreamServerAuthenticationInterceptor checks if a signed message was
// signed by provided lnd node. To check if the provided pubkey is the
// identity of origin, the origin has to provide the pubkey as well as
// a signed message in the metadata of the request.
// {
//	"pubkey": the-nodes-62-chars-long-pubkey (string),
//	"sig": the-message-signed-by-node (string)
// }
func (u *GPRCUtils) StreamServerAuthenticationInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	// Skip the authentication if the requested method is public
	isPublic, _ := ss.Context().Value(authKeyIsPublic).(bool)
	if isPublic {
		log.Printf("is public")
		return handler(srv, ss)
	}

	// Authentication
	md, ok := metadata.FromIncomingContext(ss.Context())
	if !ok {
		return errMissingMetadata
	}

	pubkey, ok := getPubkey(md)
	if !ok {
		return errMissingPubkey
	}

	sig, ok := getSig(md)
	if !ok {
		return errMissingSig
	}

	ok, err := u.valid(ss.Context(), pubkey, sig)
	if err != nil {
		log.Printf("\t [LND UTILS] > unable to process signature: %v", err)
		return err
	}
	if !ok {
		return errInvalidSignature
	}

	wrapped := grpc_middleware.WrapServerStream(ss)
	wrapped.WrappedContext = context.WithValue(ss.Context(), lndPubkey, pubkey)
	err = handler(srv, wrapped)
	if err != nil {
		log.Printf("\t [RPC] > failed with error: %v", err)
	}

	return err
}

// getPubkey retrieves the pubkey from metadata.
func getPubkey(md metadata.MD) (string, bool) {
	if len(md["pubkey"]) < 1 {
		return "", false
	}
	return strings.TrimSpace(md["pubkey"][0]), true
}

// getSig retrieves the signature from metadata.
func getSig(md metadata.MD) (string, bool) {
	if len(md["sig"]) < 1 {
		return "", false
	}
	return strings.TrimSpace(md["sig"][0]), true
}

// valid checks if the correct message was signed, and if it
// was signed by the given pubkey
func (u *GPRCUtils) valid(ctx context.Context, pubkey string, sig string) (bool, error) {
	r, err := u.vc.VerifyMessage(ctx, &lnrpc.VerifyMessageRequest{
		Msg:       []byte(AuthMsg),
		Signature: sig,
	})
	if err != nil {
		return false, err
	}

	// Leave checking on "r.Valid" of verify message response as
	// it will only be valid if there is an active channel to the
	// pubkey in th channel db.
	return r.Pubkey == pubkey, nil
}

// UnaryServerPublicMethodsInterceptor passes to the context if a
// grpc method is publicly visible or needs authentication, using
// the operands. It compares the requested grpc method with the
// given operands and sets the authKeyIsPublic context flag to true
// if a match was found. False otherwise.
// When the wildcard "*" is set, all methods are
// seen as publicly visible and dont neet authentication.
func (u *GPRCUtils) UnaryServerPublicMethodsInterceptor(publicMethods ...string) func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		for _, publicMethod := range publicMethods {
			if info.FullMethod == publicMethod || publicMethod == "*" {
				return handler(context.WithValue(ctx, authKeyIsPublic, true), req)
			}
		}
		return handler(context.WithValue(ctx, authKeyIsPublic, false), req)
	}
}
