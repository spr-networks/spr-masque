package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Realistic cloudflare.com/cdn-cgi/trace fixtures as fetched through the
// SOCKS5 proxy.
const traceWarpOn = `fl=470f126
h=www.cloudflare.com
ip=104.28.212.141
ts=1751970123.456
visit_scheme=https
uag=Go-http-client/2.0
colo=AMS
sliver=none
http=http/2
loc=NL
tls=TLSv1.3
sni=plaintext
warp=on
gateway=off
rbi=off
kex=X25519
`

const traceWarpPlus = `ip=2a09:bac1:14a0:50::2b:1f
colo=LAX
warp=plus
gateway=off
`

const traceWarpOff = `fl=18f52
h=www.cloudflare.com
ip=203.0.113.7
colo=FRA
warp=off
gateway=off
`

func rootOnly(t *testing.T, topo Topology) {
	t.Helper()
	if len(topo.Nodes) != 1 || len(topo.Edges) != 0 {
		t.Fatalf("expected root-only graph, got %d nodes %d edges", len(topo.Nodes), len(topo.Edges))
	}
	root := topo.Nodes[0]
	if root.ID != "root" || root.ConnType != "masque" || !root.Online {
		t.Errorf("bad root anchor: %+v", root)
	}
	if root.IP != "" || root.Name != "" || root.Kind != "" {
		t.Errorf("root anchor should carry no identity fields: %+v", root)
	}
}

func TestBuildTopologyConnected(t *testing.T) {
	topo := buildTopology(true, parseTrace(traceWarpOn))

	if len(topo.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d: %+v", len(topo.Nodes), topo.Nodes)
	}
	root := topo.Nodes[0]
	if root.ID != "root" || root.ConnType != "masque" || !root.Online {
		t.Errorf("bad root anchor: %+v", root)
	}

	edgeNode := topo.Nodes[1]
	if edgeNode.ID != topoEdgeNodeID {
		t.Errorf("edge node ID = %q, want %q", edgeNode.ID, topoEdgeNodeID)
	}
	if edgeNode.Kind != "vpn-exit" {
		t.Errorf("edge node Kind = %q, want vpn-exit", edgeNode.Kind)
	}
	if edgeNode.Name != "AMS" {
		t.Errorf("edge node Name = %q, want colo AMS", edgeNode.Name)
	}
	if edgeNode.IP != "104.28.212.141" {
		t.Errorf("edge node IP = %q, want exit IP from trace", edgeNode.IP)
	}
	if edgeNode.ConnType != "masque" {
		t.Errorf("edge node ConnType = %q, want masque", edgeNode.ConnType)
	}
	if !edgeNode.Online {
		t.Errorf("edge node should be online when proxy runs and warp=on")
	}

	if len(topo.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(topo.Edges))
	}
	e := topo.Edges[0]
	if e.From != "root" || e.To != topoEdgeNodeID || e.Layer != "vpn" || e.Kind != "masque" {
		t.Errorf("bad edge: %+v", e)
	}
}

func TestBuildTopologyWarpPlus(t *testing.T) {
	topo := buildTopology(true, parseTrace(traceWarpPlus))
	if len(topo.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(topo.Nodes))
	}
	n := topo.Nodes[1]
	if n.Name != "LAX" || n.IP != "2a09:bac1:14a0:50::2b:1f" {
		t.Errorf("edge node identity wrong: %+v", n)
	}
	if !n.Online {
		t.Errorf("warp=plus should count as online")
	}
}

func TestBuildTopologyWarpOff(t *testing.T) {
	// trace succeeded but the exit says warp=off: node present, not online
	topo := buildTopology(true, parseTrace(traceWarpOff))
	if len(topo.Nodes) != 2 || len(topo.Edges) != 1 {
		t.Fatalf("expected node+edge, got %d nodes %d edges", len(topo.Nodes), len(topo.Edges))
	}
	if topo.Nodes[1].Online {
		t.Errorf("edge node must not be online with warp=off")
	}
	if topo.Nodes[1].Name != "FRA" {
		t.Errorf("edge node Name = %q, want FRA", topo.Nodes[1].Name)
	}
}

func TestBuildTopologyProxyDown(t *testing.T) {
	// even with a stale successful trace, a stopped proxy contributes root only
	rootOnly(t, buildTopology(false, parseTrace(traceWarpOn)))
}

func TestBuildTopologyNoTrace(t *testing.T) {
	rootOnly(t, buildTopology(true, nil))
}

func TestBuildTopologyTraceMissingColo(t *testing.T) {
	rootOnly(t, buildTopology(true, parseTrace("warp=on\nip=1.2.3.4\n")))
}

func TestTopologyJSONShape(t *testing.T) {
	// root-only graph: Edges must encode as [], not null; empty IP/ConnType
	// are omitted on nodes that don't set them
	data, err := json.Marshal(buildTopology(false, nil))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"Edges":[]`) {
		t.Errorf("Edges should encode as empty array: %s", s)
	}
	if strings.Contains(s, `"IP"`) {
		t.Errorf("empty IP should be omitted: %s", s)
	}
	if !strings.Contains(s, `"ConnType":"masque"`) {
		t.Errorf("root anchor must declare the masque transport: %s", s)
	}
}
