package main

import (
	"context"
	"fmt"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"ln-fileserver/api"
	"ln-fileserver/filestore"
	"ln-fileserver/lnd"
	"ln-fileserver/lndutils"
	"ln-fileserver/server"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func init() {
	pflag.String("lndconnect", "", "londconnect uri to lnd node")
	pflag.Uint64("grpc_port", 9090, "port to listen for incoming grpc connections")
	pflag.String("data_dir", "", "location of data directory")
	// Bind environmental variables to flags. Will be overwritten by flags
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Panicf("could not bind pflags: %v", err)
	}
	viper.SetEnvPrefix("ln-fs")
	viper.AutomaticEnv()

	if ok := viper.IsSet("lndconnect"); !ok {
		log.Panicf("--lndconnect is not set, must be provided to connect to lnd node")
	}
	if ok := viper.IsSet("data_dir"); !ok {
		log.Panicf("--data_dir is not set, must be provided to store files")
	}
}
func main() {
	var (
		lndconnect string = viper.GetString("lndconnect")
		grpcPort   uint64 = viper.GetUint64("grpc_port")
		dataDir    string = viper.GetString("data_dir")
	)

	// Global context
	_, closeFunc := context.WithCancel(context.Background())
	defer closeFunc()

	// file store
	configStore := filestore.NewYmlUserConfigStore(dataDir)
	fileService, err := filestore.NewService(configStore, dataDir)
	if err != nil {
		log.Panicf("\t [Main] unable to create maindir %v", err)
	}

	// Connect to lnd node and create utils
	log.Println("\t [MAIN] > connecting to lnd")
	lndClient, lnConn, err := lndutils.NewLndConnectClient(context.Background(), lndconnect)
	if err != nil {
		log.Panicf("\t [LND] > unable not connect: %v", err)
	}
	defer lnConn.Close()
	invoicesClient := invoicesrpc.NewInvoicesClient(lnConn)
	lndUtils := lndutils.New(lndClient)
	_, err = lndClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		log.Panicf("\t [LND] > can not get info: %v", err)
	}

	lndService := lnd.NewService(lndClient, invoicesClient)
	// Start up grpc services
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", grpcPort))
	if err != nil {
		log.Panicf("\t [GRPC] > can not listen: %v", err)
	}
	defer lis.Close()

	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(
			grpc_middleware.ChainUnaryServer(
				lndUtils.UnaryServerPublicMethodsInterceptor(
					"/api.PrivateFileStore/GetInfo",
				),
				lndUtils.UnaryServerAuthenticationInterceptor,
			)), grpc.StreamInterceptor(
			grpc_middleware.ChainStreamServer(
				lndUtils.StreamServerAuthenticationInterceptor,
			)))
	fileserver := server.NewFileServer(fileService, lndService, &api.FeeReport{
		MsatBaseCost:        0,
		MsatPerDownloadedKB: 0,
		MsatPerHourPerKB:    0,
	})
	api.RegisterPrivateFileStoreServer(grpcSrv, fileserver)
	go func() {
		log.Println("\t [MAIN] > serving grpc")
		grpcSrv.Serve(lis)
	}()
	defer grpcSrv.GracefulStop()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	log.Println("\t [MAIN] > await signal")
	<-sigs
	log.Println("\t [MAIN] > exiting")
}
