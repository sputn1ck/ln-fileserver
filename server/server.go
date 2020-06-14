package server

import (
	"context"
	"fmt"
	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"io"
	"ln-fileserver/api"
	"ln-fileserver/filestore"
	lnd2 "ln-fileserver/lnd"
	"time"
)

type FileServer struct{
	fs *filestore.Service
	lnd *lnd2.Service

	fees *api.FeeReport
}

func NewFileServer(fs *filestore.Service, lnd *lnd2.Service, fees *api.FeeReport) *FileServer {
	return &FileServer{fs: fs, lnd: lnd, fees:fees}
}


func (f *FileServer) GetInfo(ctx context.Context, req *api.GetInfoRequest) (*api.GetInfoResponse, error) {
	return &api.GetInfoResponse{
		FeeReport: f.fees,
	},nil
}

func (f *FileServer) ListFiles(ctx context.Context, req *api.ListFilesRequest) (*api.ListFilesResponse, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Internal, fmt.Sprintf("unable to read metadata"))
	}

	pubkey := md.Get("pubkey")
	if len(pubkey) != 1 {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("unable to get pubkey from metadata"))
	}
	fileSlots, err := f.fs.ListFiles(ctx, pubkey[0])
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	var pbFileSlots []*api.FileSlot
	for k,v := range fileSlots {
		pbFileSlots = append(pbFileSlots, f.YmlFileSlotToProto(k, v))
	}
	return &api.ListFilesResponse{Files:pbFileSlots}, nil
}

func (f *FileServer) UploadFile(srv api.PrivateFileStore_UploadFileServer) error {
	// todo invoice stuff
	md, ok := metadata.FromIncomingContext(srv.Context())
	if !ok {
		return status.Error(codes.Internal, fmt.Sprintf("unable to read metadata"))
	}

	pubkey := md.Get("pubkey")
	if len(pubkey) != 1 {
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("unable to get pubkey from metadata"))
	}

	// Get Initial Request
	req, err := srv.Recv()
	if err != nil {
		return err
	}
	newFileSlot := req.GetSlot()
	if newFileSlot == nil {
		return fmt.Errorf("Expected NewFileSlot")
	}
	if newFileSlot.DeletionDate < time.Now().UTC().Unix() + 3600 {
		return fmt.Errorf("minimum store time is 1 hour")
	}
	cost := f.fees.MsatBaseCost
	paymentChan := make(chan *lnrpc.Invoice)
	fmt.Printf("\n \t [FS] new Fileslot Request Cost:%v;Fileslot request %v", cost, newFileSlot)
	defer close(paymentChan)
	if cost > 0 {
		// Return CreationInvoice
		invoice,err := f.lnd.CreateListenInvoice(srv.Context(), paymentChan, &lnrpc.Invoice{
			Memo:      "Create Fileslot",
			ValueMsat: f.fees.MsatBaseCost,
			Expiry:    60,
		})
		err = srv.Send(&api.UploadFileResponse{Event:&api.UploadFileResponse_Invoice{Invoice:&api.InvoiceResponse{Invoice:invoice}}})
		if err != nil {
			return err
		}

		// Wait for invoice paid
		_ = <-paymentChan
	} else {
		err = srv.Send(&api.UploadFileResponse{Event:&api.UploadFileResponse_Invoice{Invoice:&api.InvoiceResponse{Invoice:"free"}}})
		if err != nil {
			return err
		}
	}


	// Create FileSlot
	fileSlot, err := f.fs.NewFile(srv.Context(), pubkey[0], newFileSlot.Filename, newFileSlot.Description, newFileSlot.DeletionDate)
	if err != nil {
		return err
	}
	// Get FileWriter
	fileWriter, err := f.fs.GetFileWriter(srv.Context(), pubkey[0], fileSlot.Id)
	if err != nil {
		return err
	}
	defer fileWriter.Close()
	Loop:
	for  {
		req, err = srv.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch req.Event.(type){
		case *api.UploadFileRequest_Finished:

			fmt.Printf("\n \t [FS] Finished Upload")
			break Loop
		case *api.UploadFileRequest_Chunk:
			// Add Bytes
			chunk := req.GetChunk()
			_, err := fileWriter.Write(chunk.Content)
			// Get Invoice
			chunkKB := int64(float64(len(chunk.Content)) / float64(1024))
			msatCost := f.fees.MsatPerSecondPerKB * (newFileSlot.DeletionDate - time.Now().UTC().Unix()) * chunkKB

			fmt.Printf("\n \t [FS] New Chunk; size: %v; cost: %v;",len(chunk.Content), msatCost )
			if msatCost > 0 {
				invoice,err := f.lnd.CreateListenInvoice(srv.Context(), paymentChan, &lnrpc.Invoice{
					Memo:      "Uploading Chunk",
					ValueMsat: msatCost,
					Expiry:    60,
				})
				// Send Bytes Invoice
				err = srv.Send(&api.UploadFileResponse{Event:&api.UploadFileResponse_Invoice{Invoice:&api.InvoiceResponse{Invoice:invoice}}})
				if err != nil {
					return err
				}
				// Wait for invoice paid
				payment := <-paymentChan
				fmt.Printf("\n \t [FS] Invoice paid %v", payment)
			} else {
				// Send Bytes Invoice
				err = srv.Send(&api.UploadFileResponse{Event:&api.UploadFileResponse_Invoice{Invoice:&api.InvoiceResponse{Invoice:"free"}}})
				if err != nil {
					return err
				}
			}
			break
		}
	}
	fileSlot, err = f.fs.SaveFile(srv.Context(), pubkey[0], fileSlot, fileWriter)
	if err != nil {
		return err
	}
	err = srv.Send(&api.UploadFileResponse{Event:&api.UploadFileResponse_FinishedFile{FinishedFile:f.YmlFileSlotToProto(fileSlot.Id, fileSlot)}})
	if err != nil {
		return err
	}
	fmt.Printf("\n \t [FS] File saved %v", fileSlot)
	return nil

}


func (f *FileServer) DownloadFile(req *api.DownloadFileRequest, res api.PrivateFileStore_DownloadFileServer) error {
	// todo implement me
	md, ok := metadata.FromIncomingContext(res.Context())
	if !ok {
		return status.Error(codes.Internal, fmt.Sprintf("unable to read metadata"))
	}

	pubkey := md.Get("pubkey")
	if len(pubkey) != 1 {
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("unable to get pubkey from metadata"))
	}
	return nil

}

func (f *FileServer) YmlFileSlotToProto(id string, slot *filestore.FileSlot) *api.FileSlot {
	return &api.FileSlot{
		FileId: id,
		Filename:     slot.FileName,
		Description:  slot.Description,
		ShaChecksum:  slot.Sha256Checksum,
		Bytes:        slot.Bytes,
		CreationDate: slot.CreationDate,
		DeletionDate: slot.DeletionDate,
	}
}
