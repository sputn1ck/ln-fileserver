package main

import (
	"bufio"
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/sputn1ck/ln-fileserver/api"
	"github.com/sputn1ck/ln-fileserver/utils"
	"github.com/urfave/cli"
	"golang.org/x/net/context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func printRespJSON(resp proto.Message) {
	jsonMarshaler := &jsonpb.Marshaler{
		EmitDefaults: true,
		OrigName:     true,
		Indent:       "    ",
	}

	jsonStr, err := jsonMarshaler.MarshalToString(resp)
	if err != nil {
		fmt.Println("unable to decode response: ", err)
		return
	}

	fmt.Println(jsonStr)
}

var getInfoCommand = cli.Command{
	Name:   "getinfo",
	Usage:  "returns information of ln-fileserver",
	Action: getInfo,
}

func getInfo(ctx *cli.Context) error {
	ctxb := context.Background()
	lnfsClient, _, cleanUp := getClients(ctx)
	defer cleanUp()
	res, err := lnfsClient.GetInfo(ctxb, &api.GetInfoRequest{})
	if err != nil {
		return err
	}
	printRespJSON(res)
	return nil
}

var listFilesCommnad = cli.Command{
	Name:   "listfiles",
	Usage:  "returns all user owned files",
	Action: listFiles,
}

func listFiles(ctx *cli.Context) error {
	ctxb := context.Background()
	lnfsClient, _, cleanUp := getClients(ctx)
	defer cleanUp()
	res, err := lnfsClient.ListFiles(ctxb, &api.ListFilesRequest{})
	if err != nil {
		return err
	}
	printRespJSON(res)
	return nil
}
var estimateUploadFeeCommand = cli.Command{
	Name:      "uploadfee",
	Usage:     "",
	ArgsUsage: "",
	Flags:     []cli.Flag{
		cli.StringFlag{
			Name:      "file",
			Usage:     "path to file to upload",
			TakesFile: true,
			Required: true,
		},
		cli.Int64Flag{
			Name:  "store_time",
			Usage: "storage time in seconds",
			Required: true,
		},},
	Action:    estimateUploadFee,
}
func estimateUploadFee(ctx *cli.Context) error {
	ctxb := context.Background()
	lnfs, _, cleanUp := getClients(ctx)
	defer cleanUp()
	// open file
	file, err := os.Open(ctx.String("file"))
	if err != nil {
		return err
	}
	getinfo, err := lnfs.GetInfo(ctxb, &api.GetInfoRequest{})
	if err != nil {
		return err
	}
	fee, err := estimateUploadFileFee(file, ctx.Int64("store_time"), getinfo.FeeReport)
	if err != nil {
		return err
	}
	fmt.Printf("\n File: %s, fee: %v", file.Name(), fee)
	return nil
}

func estimateUploadFileFee(file *os.File, storetime int64, fees *api.FeeReport) (int64, error) {
	fi, err := file.Stat()
	if err != nil {
		return 0, err
	}
	totalCost := fees.MsatBaseCost
	totalCost+=utils.GetTotalUploadFee(fi.Size(), storetime, fees)
	return totalCost, nil
}
var uploadFileCommand = cli.Command{
	Name:      "upload",
	Usage:     "uploads a file to the ln-fileserver",
	ArgsUsage: "file",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:      "file",
			Usage:     "path to file to upload",
			TakesFile: true,
			Required: true,
		},
		cli.Int64Flag{
			Name:  "store_duration",
			Usage: "duration of storage in seconds",
			Required: true,
		},
		cli.IntFlag{
			Name:  "chunk_size",
			Usage: "bytesize of chunks that gets uploaded (default 1mb)",
			Value: 1024*1024,
		},
		cli.StringFlag{
			Name:  "description",
			Usage: "description of file",
			Value: "",
		},
		cli.BoolFlag{
			Name:  "force",
			Usage: "if set doesnt wait for fee confirmation",
		},
	},
	Action: uploadFile,
}

func uploadFile(ctx *cli.Context) error {

	ctxb := context.Background()
	lnfs, lnd, cleanUp := getClients(ctx)
	defer cleanUp()
	totalMsats := int64(0)
	// open file
	file, err := os.Open(ctx.String("file"))
	if err != nil {
		return fmt.Errorf("Error opening file %v", err)
	}
	if !ctx.Bool("force") {
		getinfo, err := lnfs.GetInfo(ctxb, &api.GetInfoRequest{})
		if err != nil {
			return err
		}
		fee, err := estimateUploadFileFee(file, ctx.Int64("store_duration"), getinfo.FeeReport)
		if err != nil {
			return err
		}
		fmt.Printf(fmt.Sprintf("\n Uploading file: %v, Estimated fee: %v", file.Name(), fee))
		do := promptForConfirmation("\n Confirm upload (yes/no): ")
		if !do {
			return fmt.Errorf("aborted upload")
		}
	}
	stream, err := lnfs.UploadFile(ctxb)
	if err != nil {
		return fmt.Errorf("Error opening stream %v", err)
	}
	// create chunk buffer with 1mb
	buf := make([]byte, ctx.Int("chunk_size"))

	// send opening request
	err = stream.Send(&api.UploadFileRequest{Event: &api.UploadFileRequest_Slot{Slot: &api.NewFileSlot{
		DeletionDate: time.Now().UTC().Unix() + ctx.Int64("store_duration"),
		Filename:     filepath.Base(file.Name()),
		Description:  ctx.String("description"),
	}}})
	if err != nil {
		return fmt.Errorf("Error sending opening req %v", err)
	}
	res, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("Error receiving %v", err)
	}
	invoice := res.GetInvoice()
	// pay invoice
	if invoice.Invoice != "free" {
		payment, err := lnd.SendPaymentSync(ctxb, &lnrpc.SendRequest{PaymentRequest: invoice.Invoice})
		if err != nil {
			return err
		}
		if payment.PaymentError != "" {
			return fmt.Errorf("Payment failed %s", payment.PaymentError)
		}
		totalMsats += payment.PaymentRoute.TotalAmtMsat
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
			return fmt.Errorf("\n [FS] > Error sending chunk req %v", err)
		}
		res, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("\n [FS] > Error receiving %v", err)
		}
		invoice = res.GetInvoice()

		// pay invoice
		// pay invoice
		if invoice.Invoice != "free" {
			payment, err := lnd.SendPaymentSync(ctxb, &lnrpc.SendRequest{PaymentRequest: invoice.Invoice})
			if err != nil {
				return err
			}
			if payment.PaymentError != "" {
				return fmt.Errorf("Payment failed %s", payment.PaymentError)
			}
			totalMsats += payment.PaymentRoute.TotalAmtMsat
		}
	}
	err = stream.Send(&api.UploadFileRequest{Event: &api.UploadFileRequest_Finished{Finished: &api.Empty{}}})
	if err != nil {
		return fmt.Errorf("\n[FS] > Error sending finished event %v", err)
	}
	res, err = stream.Recv()
	if err != nil {
		return fmt.Errorf("\n[FS] > Error receiving finished %v", err)
	}
	finished := res.GetFinishedFile()
	printRespJSON(finished)
	fmt.Printf("\n Paid a total of %v mSats", totalMsats)
	return nil
}

var downloadFileCommand = cli.Command{
	Name:      "download",
	Usage:     "downloads ln-fileserver",
	ArgsUsage: "id",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "id",
			Usage: "id of the file to download",
		},
		cli.StringFlag{
			Name:  "dir",
			Usage: "where to download to",
			Value: ".",
		},
	},
	Action: downloadFile,
}

func downloadFile(ctx *cli.Context) error {
	ctxb := context.Background()
	lnfs, lnd, cleanUp := getClients(ctx)
	defer cleanUp()
	// open file
	stream, err := lnfs.DownloadFile(ctxb, &api.DownloadFileRequest{FileId: ctx.String("id")})
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
	file, err := os.Create(filepath.Join(ctx.String("dir"), fileInfo.Filename))
	if err != nil {
		return err
	}
	defer file.Close()
Loop:
	for {
		select {
		case <-ctxb.Done():
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
				payment, err := lnd.SendPaymentSync(ctxb, &lnrpc.SendRequest{PaymentRequest: invoice})
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

func promptForConfirmation(msg string) bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(msg)

		answer, err := reader.ReadString('\n')
		if err != nil {
			return false
		}

		answer = strings.ToLower(strings.TrimSpace(answer))

		switch {
		case answer == "yes":
			return true
		case answer == "no":
			return false
		default:
			continue
		}
	}
}


