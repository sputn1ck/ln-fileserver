package main

import (
	"context"
	"fmt"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"io"
	"lndprivatefileserver/api"
	"lndprivatefileserver/lndutils"
	"log"
	"os"
	"path/filepath"
	"time"
)

func init() {
	pflag.String("lndconnect", "", "londconnect uri to lnd node")
	pflag.String("target","localhost:9090","target fileserver")
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Panicf("could not bind pflags: %v", err)
	}
	viper.SetEnvPrefix("ln-fs")
	viper.AutomaticEnv()

	if ok := viper.IsSet("lndconnect"); !ok {
		log.Panicf("--lndconnect is not set, must be provided to connect to lnd node")
	}
}
func main() {
	var (
		lndconnect       string = viper.GetString("lndconnect")
		target string = viper.GetString("target")
	)

	// Global context
	ctx, closeFunc := context.WithCancel(context.Background())
	defer closeFunc()

	// Connect to lnd node and create utils
	log.Println("\t [MAIN] > connecting to lnd")
	lndClient, lnConn, err := lndutils.NewLndConnectClient(context.Background(), lndconnect)
	if err != nil {
		log.Panicf("\t [LND] > unable not connect: %v", err)
	}
	defer lnConn.Close()
	_, err = lndClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		log.Panicf("\t [LND] > can not get info: %v", err)
	}
	msg := lndutils.AuthMsg
	opts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(UnaryAuthenticationInterceptor(lndClient, &msg)),
		grpc.WithStreamInterceptor(StreamAuthenticationIntercetpor(lndClient, &msg)),
		grpc.WithInsecure(),
	}
	conn, err := grpc.DialContext(ctx, target, opts...)
	if err != nil {
		log.Panicf("\t [Grpc] > can not connect: %v", err)
	}
	defer conn.Close()
	lnfsclient := api.NewPrivateFileStoreClient(conn)
	res, err := lnfsclient.GetInfo(ctx, &api.GetInfoRequest{})
	fmt.Printf("\t [FS] %v", res)
	err = UploadFile(ctx, lnfsclient, lndClient)
	if err != nil {
		fmt.Printf("\t[FS] ERROR: %v",err )
	}
}

func UploadFile(ctx context.Context, lnfs api.PrivateFileStoreClient, lnd lnrpc.LightningClient) error{
	// open file
	file, err := os.Open("./testdata/test.txt")
	if err != nil {
		return fmt.Errorf("[FS] > Error opening file %v", err)
	}
	stream, err := lnfs.UploadFile(ctx)
	if err != nil {
		return fmt.Errorf("[FS] > Error opening stream %v", err)
	}
	// create chunk buffer
	buf := make([]byte, 1024)

	// send opening request
	err = stream.Send(&api.UploadFileRequest{Event:&api.UploadFileRequest_Slot{Slot:&api.NewFileSlot{
		DeletionDate: time.Now().UTC().Unix() + 86400,
		Filename:     filepath.Base(file.Name()),
		Description:  "foo",
	}}})
	if err != nil {
		return fmt.Errorf("[FS] > Error sending opening req %v", err)
	}
	res, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("[FS] > Error receiving %v", err)
	}
	invoice := res.GetInvoice()
	fmt.Printf("[FS] > received invoice %s", invoice)
	// pay invoice
	writing := true
	for writing {
		n, err := file.Read(buf)
		if err == io.EOF{
			writing = false
			break
		}
		err = stream.Send(&api.UploadFileRequest{Event:&api.UploadFileRequest_Chunk{Chunk:&api.FileChunk{
			Content:buf[:n],
		}}})
		if err != nil {
			return fmt.Errorf("\t [FS] > Error sending chunk req %v", err)
		}
		res, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("\t [FS] > Error receiving %v", err)
		}
		invoice = res.GetInvoice()

		fmt.Printf("\t [FS] > received invoice %s", invoice)
		// pay invoice
	}
	err = stream.Send(&api.UploadFileRequest{Event:&api.UploadFileRequest_Finished{Finished: &api.Empty{}}})
	if err != nil {
		return fmt.Errorf("\t[FS] > Error sending finished event %v", err)
	}
	res, err = stream.Recv()
	if err != nil {
		return fmt.Errorf("\t[FS] > Error receiving finished %v", err)
	}
	finished := res.GetFinishedFile()
	fmt.Printf("\t[FS] > finished filseslot %v", finished)
	return nil

}
func UnaryAuthenticationInterceptor(lnd lnrpc.LightningClient, msg *string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx, err := GetPfContext(ctx, lnd, msg)
		if err != nil {
			return err
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func StreamAuthenticationIntercetpor(lnd lnrpc.LightningClient, msg *string) grpc.StreamClientInterceptor {
	return func(parentCtx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx, err := GetPfContext(parentCtx, lnd, msg)
		if err != nil {
			return nil, err
		}
		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			return nil, err
		}
		return clientStream, nil
	}
}

func GetPfContext(ctx context.Context, lnd lnrpc.LightningClient, msg *string) (context.Context, error) {
	gi, err := lnd.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "pubkey", gi.IdentityPubkey)


	sig, err := lnd.SignMessage(ctx, &lnrpc.SignMessageRequest{Msg:[]byte(*msg)})
	if err != nil {
		return nil, err
	}

	ctx = metadata.AppendToOutgoingContext(ctx, "sig", sig.Signature)
	return ctx, nil
}
