package sphinx

import (
	"math"

	"github.com/btcsuite/btcd/btcec"
)

// NumMaxHops is the maximum path length. This should be set to an estimate of
// the upper limit of the diameter of the node graph.
const NumMaxHops = 20

// PaymentPath represents a series of hops within the Lightning Network
// starting at a sender and terminating at a receiver. Each hop contains a set
// of mandatory data which contains forwarding instructions for that hop.
// Additionally, we can also transmit additional data to each hop by utilizing
// the un-used hops (see TrueRouteLength()) to pack in additional data. In
// order to do this, we encrypt the several hops with the same node public key,
// and unroll the extra data into the space used for route forwarding
// information.
type PaymentPath [NumMaxHops]OnionHop

// HopPayload is a slice of bytes and associated payload-type that are destined
// for a specific hop in the PaymentPath. The payload itself is treated as an
// opaque datafield by the onion router, while the Realm is modified to
// indicate how many hops are to be read by the processing node. The 4 MSB in
// the realm indicate how many additional hops are to be processed to collect
// the entire payload.
type HopPayload struct {
	Realm [1]byte

	Payload []byte

	HMAC [HMACSize]byte
}

// NumFrames returns the total number of frames it'll take to pack the target
// HopPayload into a Sphinx packet.
func (hp *HopPayload) NumFrames() int {
	// If it all fits in the legacy payload size, don't use any additional
	// frames.
	if len(hp.Payload) <= 32 {
		return 1
	}

	// Otherwise we'll need at least one additional frame: subtract the 64
	// bytes we can stuff into payload and hmac of the first, and the 33
	// bytes we can pack into the payload of the second, then divide the
	// remainder by 65.
	remainder := len(hp.Payload) - 64 - 33
	return 2 + int(math.Ceil(float64(remainder)/65))
}

// OnionHop represents an abstract hop (a link between two nodes) within the
// Lightning Network. A hop is composed of the incoming node (able to decrypt
// the encrypted routing information), and the routing information itself.
// Optionally, the crafter of a route can indicate that additional data aside
// from the routing information is be delivered, which will manifest as
// additional hops to pack the data.
type OnionHop struct {
	// NodePub is the target node for this hop. The payload will enter this
	// hop, it'll decrypt the routing information, and hand off the
	// internal packet to the next hop.
	NodePub btcec.PublicKey

	// HopData are the plaintext routing instructions that should be
	// delivered to this hop.
	HopData HopData

	// HopPayload is the opaque payload provided to this node. If the
	// HopData above is specified, then it'll be packed into this payload.
	HopPayload HopPayload
}

// IsEmpty returns true if the hop isn't populated.
func (o OnionHop) IsEmpty() bool {
	return o.NodePub.X == nil || o.NodePub.Y == nil
}

// NodeKeys returns a slice pointing to node keys that this route comprises of.
// The size of the returned slice will be TrueRouteLength().
func (p *PaymentPath) NodeKeys() []*btcec.PublicKey {
	var nodeKeys [NumMaxHops]*btcec.PublicKey

	routeLen := p.TrueRouteLength()
	for i := 0; i < routeLen; i++ {
		nodeKeys[i] = &p[i].NodePub
	}

	return nodeKeys[:routeLen]
}

// TrueRouteLength returns the "true" length of the PaymentPath. The max
// payment path is NumMaxHops size, but in practice routes are much smaller.
// This method will return the number of actual hops (nodes) involved in this
// route. For references, a direct path has a length of 1, path through an
// intermediate node has a length of 2 (3 nodes involved).
func (p *PaymentPath) TrueRouteLength() int {
	var routeLength int
	for _, hop := range p {
		// When we hit the first empty hop, we know we're now in the
		// zero'd out portion of the array.
		if hop.IsEmpty() {
			return routeLength
		}

		routeLength++
	}

	return routeLength
}

// TotalFrames returns the total numebr of frames that it'll take to create a
// Sphinx packet from the target PaymentPath.
func (p *PaymentPath) TotalFrames() int {
	var frameCount int
	for _, hop := range p {
		if hop.IsEmpty() {
			break
		}

		frameCount += hop.HopPayload.NumFrames()
	}

	return frameCount
}

const (
	// RealmMaskBytes is the mask to apply the realm in order to pack or
	// decode the 4 LSB of the realm field.
	RealmMaskBytes = 0x0f

	// NumFramesShift is the number of bytes to shift the encoding of the
	// number of frames by in order to pack/unpack them into the 4 MSB bits
	// of the realm field.
	NumFramesShift = 4
)

// CalculateRealm computes the proper realm encoding in place. The final
// encoding uses the first 4 bits of the realm to encode the number of frames
// used, and the latter 4 bits to encode the real realm type.
func (hp *HopPayload) CalculateRealm() {
	maskedRealm := hp.Realm[0] & 0x0F
	numFrames := hp.NumFrames()

	hp.Realm[0] = maskedRealm | (byte(numFrames-1) << NumFramesShift)
}
