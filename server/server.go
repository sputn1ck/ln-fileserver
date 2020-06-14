package server

import (
	"context"
	"fmt"
	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"io"
	"lndprivatefileserver/api"
	"lndprivatefileserver/filestore"
)

type FileServer struct{
	fs *filestore.Service
	lnd lnrpc.LightningClient
}

func NewFileServer(fs *filestore.Service, lnd lnrpc.LightningClient) *FileServer {
	return &FileServer{fs: fs, lnd: lnd}
}


func (f *FileServer) GetInfo(ctx context.Context, req *api.GetInfoRequest) (*api.GetInfoResponse, error) {
	return &api.GetInfoResponse{
		FeeReport: &api.FeeReport{
			MsatBaseCost:        10000,
			MsatPerUploadedByte: 1,
			MsatPerDay:          1,
			MsatPerDownloadByte: 1,
		},
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

	//paymentChan := make(chan *lnrpc.Payment)
	// Get Initial Request
	req, err := srv.Recv()
	if err != nil {
		return err
	}
	newFileSlot := req.GetSlot()
	if newFileSlot == nil {
		return fmt.Errorf("Expected NewFileSlot")
	}
	// Return CreationInvoice
	err = srv.Send(&api.UploadFileResponse{Event:&api.UploadFileResponse_Invoice{Invoice:&api.InvoiceResponse{Invoice:"opening invoice"}}})
	if err != nil {
		return err
	}
	// Wait for invoice paid

	//payment := <-paymentChan
	//if payment.Status != lnrpc.Payment_SUCCEEDED {
	//	return fmt.Errorf("payment failed: %v",payment.FailureReason)
	//}
	// Create FileSlot
	fileSlot, err := f.fs.NewFile(srv.Context(), pubkey[0], newFileSlot.Filename, newFileSlot.Description, newFileSlot.DeletionDate)
	if err != nil {
		return err
	}
	// Get FileWriter
	fileWriter, err := f.fs.GetFileWriter(srv.Context(), pubkey[0], fileSlot.FileName)
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
			break Loop
		case *api.UploadFileRequest_Chunk:
			// Add Bytes
			chunk := req.GetChunk()
			_, err := fileWriter.Write(chunk.Content)
			// Get Invoice
			// Send Bytes Invoice
			err = srv.Send(&api.UploadFileResponse{Event:&api.UploadFileResponse_Invoice{Invoice:&api.InvoiceResponse{Invoice:"chunk invoice"}}})
			if err != nil {
				return err
			}
			// Wait for invoice paid
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
