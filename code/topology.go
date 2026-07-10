package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// TopoNode / TopoEdge / Topology mirror the shapes SPR expects from plugin
// topology providers (same contract as spr-tailscale). The SPR host merges
// this graph into the router topology at the "root" anchor node.
type TopoNode struct {
	ID       string
	Kind     string
	Name     string
	IP       string `json:",omitempty"`
	ConnType string `json:",omitempty"`
	Online   bool
}

type TopoEdge struct {
	From  string
	To    string
	Layer string
	Kind  string
}

type Topology struct {
	Nodes []TopoNode
	Edges []TopoEdge
}

const topoEdgeNodeID = "cloudflare-edge"

// buildTopology builds the plugin's contribution to SPR's topology view from
// honest live state only: the proxy supervisor state and the parsed
// Cloudflare trace fetched *through* the tunnel. When the tunnel is up it
// emits a single "vpn-exit" node for the Cloudflare edge (Name = colo code,
// IP = WARP exit IP) hanging off the root anchor over a "vpn" layer edge.
// Not registered / proxy down / trace failed -> root anchor only.
func buildTopology(running bool, trace map[string]string) Topology {
	topo := Topology{
		Nodes: []TopoNode{{ID: "root", ConnType: "masque", Online: true}},
		Edges: []TopoEdge{},
	}

	// trace["colo"] on a nil map is safely "".
	if !running || trace["colo"] == "" {
		return topo
	}

	warpOn := trace["warp"] == "on" || trace["warp"] == "plus"
	topo.Nodes = append(topo.Nodes, TopoNode{
		ID:       topoEdgeNodeID,
		Kind:     "vpn-exit",
		Name:     trace["colo"],
		IP:       trace["ip"],
		ConnType: "masque",
		Online:   running && warpOn,
	})
	topo.Edges = append(topo.Edges, TopoEdge{
		From:  "root",
		To:    topoEdgeNodeID,
		Layer: "vpn",
		Kind:  "masque",
	})
	return topo
}

// GET /topology
func handleTopology(w http.ResponseWriter, r *http.Request) {
	running, _, _ := proxy.Status()
	running = running && warpRegistered()

	var trace map[string]string
	if running {
		settings := loadSettings()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		raw, err := fetchTrace(ctx, net.JoinHostPort(getContainerIP(), fmt.Sprintf("%d", settings.SocksPort)))
		if err == nil {
			trace = parseTrace(raw)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildTopology(running, trace))
}
