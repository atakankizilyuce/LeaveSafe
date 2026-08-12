package network

import (
	"net"
	"strings"
)

// Placement says where the router this program asked for a port mapping sits,
// relative to the internet.
//
// It is the difference between a port mapping that means something and one that
// is a hole in the wrong wall. Forwarding a port on a router that is itself
// behind another router moves a connection one hop closer and no further; the
// address the internet sees belongs to whatever is in front, and nothing this
// program or this user can do to the inner router changes that.
type Placement string

const (
	// PlacementUnknown is what an unanswered question looks like: one of the
	// two addresses was not available, so nothing follows.
	PlacementUnknown Placement = ""

	// PlacementEdge is the good case. The router holding the mapping is the one
	// the internet talks to, so the mapping is on the path.
	PlacementEdge Placement = "edge"

	// PlacementCarrier is carrier-grade NAT: the router's own external address
	// is inside RFC 6598 space, which the ISP hands out while keeping the
	// routable address for itself and shares among its subscribers.
	PlacementCarrier Placement = "carrier"

	// PlacementDouble is another router in front of this one — a modem in
	// router mode, a landlord's box, a phone's hotspot. Unlike carrier NAT this
	// one is the user's to fix, if they can reach the outer router.
	PlacementDouble Placement = "double"
)

// RouterPlacement compares what the router says its own external address is
// with what the internet says this network's address is.
//
// This is the check that was missing, and its absence is why the remote URL
// could be advertised while nothing could reach it. Carrier-grade NAT was
// looked for in the wrong place: in the address the internet reports. But under
// carrier NAT the internet reports the ISP's routable address, which is an
// ordinary public address and passes every test — the 100.64 address is the one
// the *router* holds, on the inside, and nobody was asking the router. So a
// machine behind carrier NAT mapped a port successfully, was handed a public
// URL, printed it as the QR code to scan, and could not be reached from mobile
// data by anyone. The symptom was a phone on a spinner with nothing said.
//
// Two addresses that agree are the proof that there is nothing in between.
func RouterPlacement(routerWAN, publicIP string) Placement {
	router := net.ParseIP(strings.TrimSpace(routerWAN))
	public := net.ParseIP(strings.TrimSpace(publicIP))
	if router == nil || public == nil {
		return PlacementUnknown
	}

	if router.Equal(public) {
		return PlacementEdge
	}
	if IsCarrierGradeNAT(router.String()) {
		return PlacementCarrier
	}
	if !router.IsGlobalUnicast() || router.IsPrivate() {
		return PlacementDouble
	}

	// Two different public addresses. A router with more than one uplink can
	// produce this honestly, and so can a lookup that went out by a different
	// path than the mapping would come in by. It is not evidence of anything,
	// and guessing here would mean telling somebody their network is broken on
	// the strength of a coincidence.
	return PlacementUnknown
}
