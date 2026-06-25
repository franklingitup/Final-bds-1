package cluster

import "context"

// TokenDelivery carries the plaintext registration token for out-of-band delivery.
type TokenDelivery struct {
	ClusterID   string
	Token       string
	DeliveryRef string
}

// Notifier delivers sensitive tokens out-of-band so they never appear in events.
type Notifier interface {
	DeliverToken(ctx context.Context, d TokenDelivery)
}

// noopNotifier is a no-op implementation for production where delivery
// is handled by an external Notification Service consumer.
type noopNotifier struct{}

func (noopNotifier) DeliverToken(context.Context, TokenDelivery) {}
