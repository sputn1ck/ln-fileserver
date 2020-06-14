package main

import (
	"context"
	"fmt"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/sputn1ck/ln-fileserver/api"
	"github.com/sputn1ck/ln-fileserver/lndutils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

func init() {
	pflag.String("lndconnect", "", "londconnect uri to lnd node")
	pflag.String("target", "localhost:9090", "target fileserver")
	
	pflag.Parse()

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
		lndconnect string = viper.GetString("lndconnect")
		target     string = viper.GetString("target")
	)

	// Global context
	ctx, closeFunc := context.WithCancel(context.Background())
	defer closeFunc()

	// Connect to lnd node and create utils
	log.Println("\n [MAIN] > connecting to lnd")
	lndClient, lnConn, err := lndutils.NewLndConnectClient(context.Background(), lndconnect)
	if err != nil {
		log.Panicf("\n [LND] > unable not connect: %v", err)
	}
	defer lnConn.Close()
	_, err = lndClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		log.Panicf("\n [LND] > can not get info: %v", err)
	}
	msg := lndutils.AuthMsg
	opts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(UnaryAuthenticationInterceptor(lndClient, &msg)),
		grpc.WithStreamInterceptor(StreamAuthenticationIntercetpor(lndClient, &msg)),
		grpc.WithInsecure(),
	}
	conn, err := grpc.DialContext(ctx, target, opts...)
	if err != nil {
		log.Panicf("\n[Grpc] > can not connect: %v", err)
	}
	defer conn.Close()
	lnfsclient := api.NewPrivateFileStoreClient(conn)
	feereport, err := lnfsclient.GetInfo(ctx, &api.GetInfoRequest{})
	fmt.Printf("\t [FS] %v", feereport)
	files, err := lnfsclient.ListFiles(ctx, &api.ListFilesRequest{})
	fmt.Printf("\t [FS] Files: %v", files)
	fileSlot, err := UploadFile(ctx, lnfsclient, lndClient)
	if err != nil {
		fmt.Printf("\n [FS] ERROR: %v", err)
		return
	}
	err = DownloadFile(ctx, lnfsclient, lndClient, fileSlot.FileId)
	if err != nil {
		fmt.Printf("\n [FS] ERROR: %v", err)
		return
	}
}

func UploadFile(ctx context.Context, lnfs api.PrivateFileStoreClient, lnd lnrpc.LightningClient) (*api.FileSlot, error) {
	// open file
	file, err := os.Open("./testdata/upload/foo.exe")
	if err != nil {
		return nil, fmt.Errorf("Error opening file %v", err)
	}
	stream, err := lnfs.UploadFile(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error opening stream %v", err)
	}
	// create chunk buffer with 1mb
	buf := make([]byte, 1024*1024)

	// send opening request
	err = stream.Send(&api.UploadFileRequest{Event: &api.UploadFileRequest_Slot{Slot: &api.NewFileSlot{
		DeletionDate: time.Now().UTC().Unix() + 86400,
		Filename:     filepath.Base(file.Name()),
		Description:  "foo",
	}}})
	if err != nil {
		return nil, fmt.Errorf("Error sending opening req %v", err)
	}
	res, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("Error receiving %v", err)
	}
	invoice := res.GetInvoice()
	fmt.Printf("[FS] > received invoice %s", invoice)
	// pay invoice
	if invoice.Invoice != "free" {
		payment, err := lnd.SendPaymentSync(ctx, &lnrpc.SendRequest{PaymentRequest: invoice.Invoice})
		if err != nil {
			return nil, err
		}
		if payment.PaymentError != "" {
			return nil, fmt.Errorf("Payment failed %s", payment.PaymentError)
		}
	}
	writing := true
	for writing {
		n, err := file.Read(buf)
		if err == io.EOF {
			writing = false
			break
		}
		err = stream.Send(&api.UploadFileRequest{Event: &api.UploadFileRequest_Chunk{Chunk: &api.FileChunk{
			Content: buf[:n],
		}}})
		if err != nil {
			return nil, fmt.Errorf("\n [FS] > Error sending chunk req %v", err)
		}
		res, err := stream.Recv()
		if err != nil {
			return nil, fmt.Errorf("\n [FS] > Error receiving %v", err)
		}
		invoice = res.GetInvoice()

		fmt.Printf("\n [FS] > received invoice %s", invoice)
		// pay invoice
		// pay invoice
		if invoice.Invoice != "free" {
			payment, err := lnd.SendPaymentSync(ctx, &lnrpc.SendRequest{PaymentRequest: invoice.Invoice})
			if err != nil {
				return nil, err
			}
			if payment.PaymentError != "" {
				return nil, fmt.Errorf("Payment failed %s", payment.PaymentError)
			}
		}
	}
	err = stream.Send(&api.UploadFileRequest{Event: &api.UploadFileRequest_Finished{Finished: &api.Empty{}}})
	if err != nil {
		return nil, fmt.Errorf("\n[FS] > Error sending finished event %v", err)
	}
	res, err = stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("\n[FS] > Error receiving finished %v", err)
	}
	finished := res.GetFinishedFile()
	fmt.Printf("\n[FS] > finished filseslot %v", finished)
	return finished, nil

}

func DownloadFile(ctx context.Context, lnfs api.PrivateFileStoreClient, lnd lnrpc.LightningClient, fileid string) error {
	stream, err := lnfs.DownloadFile(ctx, &api.DownloadFileRequest{FileId: fileid})
	if err != nil {
		return err
	}
	res, err := stream.Recv()
	if err != nil {
		return err
	}
	fileInfo := res.GetFileInfo()
	if fileInfo == nil {
		return fmt.Errorf("fileinfo expected")
	}
	file, err := os.Create(filepath.Join("./testdata/download/", fileInfo.Filename))
	if err != nil {
		return err
	}
	defer file.Close()
Loop:
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			res, err = stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			switch res.Event.(type) {
			case *api.DownloadFileResponse_Finished:
				break Loop
			case *api.DownloadFileResponse_Chunk:
				_, err := file.Write(res.GetChunk().Content)
				if err != nil {
					return err
				}
			case *api.DownloadFileResponse_Invoice:
				invoice := res.GetInvoice().Invoice
				if invoice == "free" {
					continue
				}
				payment, err := lnd.SendPaymentSync(ctx, &lnrpc.SendRequest{PaymentRequest: invoice})
				if err != nil {
					return err
				}
				if payment.PaymentError != "" {
					return fmt.Errorf("Payment failed %s", payment.PaymentError)
				}
			}
		}
	}
	err = stream.CloseSend()
	if err != nil {
		return err
	}
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

	sig, err := lnd.SignMessage(ctx, &lnrpc.SignMessageRequest{Msg: []byte(*msg)})
	if err != nil {
		return nil, err
	}

	ctx = metadata.AppendToOutgoingContext(ctx, "sig", sig.Signature)
	return ctx, nil
}
