package main

import (
	"context"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/sputn1ck/ln-fileserver/api"
	"github.com/sputn1ck/ln-fileserver/lndutils"
	"github.com/urfave/cli"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"log"
	"os"
)

func main() {
	app := cli.NewApp()
	app.Name = "lnfscli"
	app.Usage = "cli for lightning network fileserver"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:     "lndconnect",
			Usage:    "lndconnect string",
			Required: true,
		},
		cli.StringFlag{
			Name:  "target",
			Usage: "target fileserver host",
			Value: "localhost:9090",
		},
	}
	app.Commands = []cli.Command{
		getInfoCommand,
		listFilesCommnad,
		uploadFileCommand,
		downloadFileCommand,
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
// old
//func init() {
//	pflag.String("lndconnect", "", "londconnect uri to lnd node")
//	pflag.String("target", "localhost:9090", "target fileserver")
//
//	pflag.Parse()
//
//	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
//		log.Panicf("could not bind pflags: %v", err)
//	}
//	viper.SetEnvPrefix("ln-fs")
//	viper.AutomaticEnv()
//
//	if ok := viper.IsSet("lndconnect"); !ok {
//		log.Panicf("--lndconnect is not set, must be provided to connect to lnd node")
//	}
//}
//
//var (
//	lnConn *grpc.ClientConn
//	lndClient lnrpc.LightningClient
//	lnfsConn *grpc.ClientConn
//	lnfsClient api.PrivateFileStoreClient
//)
//func main() {
//	var (
//		lndconnect string = viper.GetString("lndconnect")
//		target     string = viper.GetString("target")
//		err error
//	)
//
//	// Global context
//	ctx, closeFunc := context.WithCancel(context.Background())
//	defer closeFunc()
//
//	// Connect to lnd node and create utils
//	log.Println("\n [MAIN] > connecting to lnd")
//	lndClient, lnConn, err = lndutils.NewLndConnectClient(context.Background(), lndconnect)
//	if err != nil {
//		log.Panicf("\n [LND] > unable not connect: %v", err)
//	}
//	defer lnConn.Close()
//	_, err = lndClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
//	if err != nil {
//		log.Panicf("\n [LND] > can not get info: %v", err)
//	}
//	msg := lndutils.AuthMsg
//	opts := []grpc.DialOption{
//		grpc.WithUnaryInterceptor(UnaryAuthenticationInterceptor(lndClient, &msg)),
//		grpc.WithStreamInterceptor(StreamAuthenticationIntercetpor(lndClient, &msg)),
//		grpc.WithInsecure(),
//	}
//	lnfsConn, err = grpc.DialContext(ctx, target, opts...)
//	if err != nil {
//		log.Panicf("\n[Grpc] > can not connect: %v", err)
//	}
//	defer lnfsConn.Close()
//	lnfsClient = api.NewPrivateFileStoreClient(lnfsConn)
//	feereport, err := lnfsClient.GetInfo(ctx, &api.GetInfoRequest{})
//	fmt.Printf("\t [FS] %v", feereport)
//	files, err := lnfsClient.ListFiles(ctx, &api.ListFilesRequest{})
//	fmt.Printf("\t [FS] Files: %v", files)
//	fileSlot, err := UploadFile(ctx, lnfsClient, lndClient)
//	if err != nil {
//		fmt.Printf("\n [FS] ERROR: %v", err)
//		return
//	}
//	err = DownloadFile(ctx, lnfsClient, lndClient, fileSlot.FileId)
//	if err != nil {
//		fmt.Printf("\n [FS] ERROR: %v", err)
//		return
//	}
//}

func getClients(ctx *cli.Context) (api.PrivateFileStoreClient, lnrpc.LightningClient, func()) {
	lndConn := getLndConn(ctx)
	lndClient := lnrpc.NewLightningClient(lndConn)
	lnfsConn := getLnfsConn(ctx, lndClient)
	lnfsClient := api.NewPrivateFileStoreClient(lnfsConn)

	cleanUp := func() {
		lndConn.Close()
		lnfsConn.Close()
	}
	return lnfsClient, lndClient, cleanUp
}
func getLndClient(ctx *cli.Context) (lnrpc.LightningClient, func()) {
	conn := getLndConn(ctx)
	cleanUp := func() {
		conn.Close()
	}
	return lnrpc.NewLightningClient(conn), cleanUp
}

func getLndConn(ctx *cli.Context ) *grpc.ClientConn {
	lndconnect := ctx.GlobalString("lndconnect")
	_, lnConn, err := lndutils.NewLndConnectClient(context.Background(), lndconnect)
	if err != nil {
		log.Panicf("\n[LND] > can not connect: %v", err)
	}
	return lnConn
}

func getLnfsClient(ctx *cli.Context, client lnrpc.LightningClient) (api.PrivateFileStoreClient, func()) {
	conn := getLnfsConn(ctx, client)
	cleanUp := func() {
		conn.Close()
	}
	return api.NewPrivateFileStoreClient(conn), cleanUp
}

func getLnfsConn(ctx *cli.Context, client lnrpc.LightningClient) *grpc.ClientConn {
	target := ctx.GlobalString("target")
	msg := lndutils.AuthMsg
	opts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(UnaryAuthenticationInterceptor(client, &msg)),
		grpc.WithStreamInterceptor(StreamAuthenticationIntercetpor(client, &msg)),
		grpc.WithInsecure(),
	}
	lnfsConn, err := grpc.DialContext(context.Background(), target, opts...)
	if err != nil {
		log.Panicf("\n[LNFS] > can not connect: %v", err)
	}
	return lnfsConn
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
