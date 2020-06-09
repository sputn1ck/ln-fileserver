package server

import (
	"context"
	"fmt"
	"github.com/lightningnetwork/lnd/lnrpc"
	"io"
	"lndprivatefileserver/api"
	"lndprivatefileserver/filestore"
)

type FileServer struct{
	fs *filestore.Service
}

func (f *FileServer) GetInfo(ctx context.Context, req *api.GetInfoRequest) (*api.GetInfoResponse, error) {
	panic("implement me")
}

func (f *FileServer) ListFiles(ctx context.Context, req *api.ListFilesRequest) (*api.ListFilesResponse, error) {
	panic("implement me")
}

func (f *FileServer) UploadFile(srv api.PrivateFileStore_UploadFileServer) error {
	paymentChan := make(chan *lnrpc.Payment)
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
	err = srv.Send(&api.UploadFileResponse{Event:&api.UploadFileResponse_Invoice{Invoice:&api.InvoiceResponse{Invoice:""}}})
	if err != nil {
		return err
	}
	// Wait for invoice paid
	payment := <-paymentChan
	if payment.Status != lnrpc.Payment_SUCCEEDED {
		return fmt.Errorf("payment failed: %v",payment.FailureReason)
	}
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
			// Send Bytes Invoice
			err = srv.Send(&api.UploadFileResponse{Event:&api.UploadFileResponse_Invoice{Invoice:&api.InvoiceResponse{Invoice:""}}})
			if err != nil {
				return err
			}
			// Wait for invoice paid
			break
		}
	}

}


func (f *FileServer) DownloadFile(req *api.DownloadFileRequest, res api.PrivateFileStore_DownloadFileServer) error {
	panic("implement me")
}

