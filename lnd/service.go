package lnd

import (
	"context"
	"fmt"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"io"
)


type Service struct {
	lnd lnrpc.LightningClient
	invoices invoicesrpc.InvoicesClient
}

func NewService(lnd lnrpc.LightningClient, invoices invoicesrpc.InvoicesClient) *Service {
	return &Service{lnd: lnd, invoices: invoices}
}
func (s *Service) CreateListenInvoice(ctx context.Context, paymentChan chan *lnrpc.Invoice, invoice *lnrpc.Invoice) (string, error) {
	invoiceRes, err := s.lnd.AddInvoice(ctx, invoice)
	if err != nil {
		return "",err
	}
	go func() {
		err := s.ListenPayment(ctx, paymentChan, invoiceRes.RHash)
		if err != nil {
			fmt.Printf("[LND] > Listen Payment error: %v", err)
			return
		}
	}()
	return invoiceRes.PaymentRequest, nil

}

func (s *Service) ListenPayment(ctx context.Context, paymentChan chan *lnrpc.Invoice, paymentHash []byte) error{
	stream, err := s.invoices.SubscribeSingleInvoice(ctx, &invoicesrpc.SubscribeSingleInvoiceRequest{
		RHash: paymentHash,
	})
	if err != nil {
		return err
	}
	for {
		select {
			case <-ctx.Done():
				return nil
		default:
			res, err := stream.Recv()
			if err == io.EOF{
				return nil
			}
			if err != nil {
				return err
			}
			if res.State == lnrpc.Invoice_SETTLED{
				paymentChan <- res
				return nil
			}
		}
	}
}

