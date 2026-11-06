package main

import (
	"context"
	"fmt"
	"sync"
)

// demoShipping is application code, not a Gent service. Production applications need a durable
// operation ledger and an approval check at their own dispatch boundary.
type demoShipping struct {
	mu        sync.Mutex
	replies   map[string]*ShipmentReply
	cancelled map[string]bool
}

func newDemoShipping() *demoShipping {
	return &demoShipping{replies: map[string]*ShipmentReply{}, cancelled: map[string]bool{}}
}

func (s *demoShipping) Create(ctx context.Context, callID string, input *ShipmentInput) (
	*ShipmentReply, error,
) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if reply := s.replies[callID]; reply != nil {
		return reply, nil
	}
	if s.cancelled[callID] {
		return nil, fmt.Errorf("operation %s was cancelled before dispatch", callID)
	}
	reply := &ShipmentReply{Shipment: &Shipment{OrderID: input.OrderID}}
	switch input.OrderID {
	case "order-1001":
		// This fixed demo order has prior application approval. Policy text alone is not approval.
		reply.Shipment.ID, reply.Shipment.Status = "shipment-1001", ShipmentCreated
		reply.LabelPDF = []byte(sampleLabelPDF)
	case "approval-required":
		reply.Shipment.Status = ShipmentNeedsApproval
		reply.ApprovalRequestID = "approval-1002"
	default:
		reply.Shipment.Status = ShipmentDeclined
	}
	s.replies[callID] = reply
	return reply, nil
}

// CancelPlanned supplies positive evidence that an operation was stopped before dispatch.
// It cannot replace a completed or started operation. Create checks the same ledger under lock.
func (s *demoShipping) CancelPlanned(callID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replies[callID] != nil {
		return fmt.Errorf("operation %s already has a response", callID)
	}
	s.cancelled[callID] = true
	return nil
}

func (s *demoShipping) Inspect(ctx context.Context, callID, orderID string) (
	*ShipmentCheck, error,
) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if reply := s.replies[callID]; reply != nil {
		if reply.Shipment.OrderID != orderID {
			return nil, fmt.Errorf("operation order does not match")
		}
		return &ShipmentCheck{Status: CheckCompleted, Reply: reply}, nil
	}
	if s.cancelled[callID] {
		return &ShipmentCheck{Status: CheckNotStarted}, nil
	}
	// Absence in this in-memory fixture cannot prove that a previous server had no effect.
	return &ShipmentCheck{
		Status: CheckUnresolved, OperationID: callID, ProviderState: "not_confirmed",
	}, nil
}

// A valid one-page PDF, kept as source so the downloaded example needs no external assets.
const sampleLabelPDF = `%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /MediaBox [0 0 360 240] 
/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>
endobj
4 0 obj
<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>
endobj
5 0 obj
<< /Length 182 >>
stream
BT /F1 18 Tf 36 180 Td (DEMO SHIPPING LABEL) Tj
0 -32 Td /F1 12 Tf (Shipment: shipment-1001) Tj
0 -22 Td (Order: order-1001) Tj
0 -32 Td (Sample only - not valid for shipping) Tj ET
endstream
endobj
xref
0 6
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000242 00000 n 
0000000312 00000 n 
trailer
<< /Size 6 /Root 1 0 R >>
startxref
544
%%EOF
`
