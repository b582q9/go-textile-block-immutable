package ipfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"sort"
	"strings"
	"time"

	"gx/ipfs/QmPDEJTb3WBHmvubsLXCaqRPC8dRgvFz7A4p96dxZbJuWL/go-ipfs/core"
	"gx/ipfs/QmPDEJTb3WBHmvubsLXCaqRPC8dRgvFz7A4p96dxZbJuWL/go-ipfs/pin"
	"gx/ipfs/QmQmhotPUzVrMEWNK3x1R5jQ5ZHWyL7tVUrmRPjrBrvyCb/go-ipfs-files"
	"gx/ipfs/QmTbxNB1NwDesLmKTscr4udL2tVP7MaxvXnD1D9yX7g3PN/go-cid"
	"gx/ipfs/QmXLwxifxwfc2bAwq6rdjbYqAsGzWsDE9RM5TWMGtykyj6/interface-go-ipfs-core"
	"gx/ipfs/QmXLwxifxwfc2bAwq6rdjbYqAsGzWsDE9RM5TWMGtykyj6/interface-go-ipfs-core/options"
	"gx/ipfs/QmXLwxifxwfc2bAwq6rdjbYqAsGzWsDE9RM5TWMGtykyj6/interface-go-ipfs-core/options/namesys"
	inet "gx/ipfs/QmY3ArotKMKaL7YGfbQfyDrib6RVraLqZYWXZvVgZktBxp/go-libp2p-net"
	"gx/ipfs/QmYVXrKrKHDC9FobgmcmshCDyWwdrfwfanNQN4oxJ9Fk3h/go-libp2p-peer"
	ipld "gx/ipfs/QmZ6nzCLwGLVfRzYLpD7pW6UNuBDKEcA2imJtVpbEx2rxy/go-ipld-format"
	logging "gx/ipfs/QmbkT7eMTyXfpeyB3ZMxxcxg7XH8t6uXp49jqzz4HB7BGF/go-log"
	uio "gx/ipfs/QmcYUTQ7tBZeH1CLsZM2S3xhMEZdvUgXvbjhpMsLDpk3oJ/go-unixfs/io"
)

var log = logging.Logger("tex-ipfs")

const pinTimeout = time.Minute
const catTimeout = time.Minute
const ipnsTimeout = time.Second * 30
const connectTimeout = time.Second * 10
const publishTimeout = time.Second * 5

// DataAtPath return bytes under an ipfs path
func DataAtPath(pctx context.Context, api iface.CoreAPI, pth string) ([]byte, error) {
	ip, err := iface.ParsePath(pth)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(pctx, catTimeout)
	defer cancel()

	f, err := api.Unixfs().Get(ctx, ip)
	if err != nil {
		log.Errorf("failed to get data: %s", pth)
		return nil, err
	}
	defer f.Close()

	var file files.File
	switch f := f.(type) {
	case files.File:
		file = f
	case files.Directory:
		return nil, iface.ErrIsDir
	default:
		return nil, iface.ErrNotSupported
	}

	return ioutil.ReadAll(file)
}

// LinksAtPath return ipld links under a path
func LinksAtPath(pctx context.Context, api iface.CoreAPI, pth string) ([]*ipld.Link, error) {
	ip, err := iface.ParsePath(pth)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(pctx, catTimeout)
	defer cancel()

	res, err := api.Unixfs().Ls(ctx, ip)
	if err != nil {
		return nil, err
	}

	links := make([]*ipld.Link, 0)
	for link := range res {
		links = append(links, link.Link)
	}

	return links, nil
}

// AddDataToDirectory adds reader bytes to a virtual dir
func AddDataToDirectory(pctx context.Context, api iface.CoreAPI, dir uio.Directory, fname string, reader io.Reader) (*cid.Cid, error) {
	id, err := AddData(pctx, api, reader, false)
	if err != nil {
		return nil, err
	}

	n, err := api.Dag().Get(pctx, *id)
	if err != nil {
		return nil, err
	}

	if err := dir.AddChild(pctx, fname, n); err != nil {
		return nil, err
	}

	return id, nil
}

// AddLinkToDirectory adds a link to a virtual dir
func AddLinkToDirectory(pctx context.Context, api iface.CoreAPI, dir uio.Directory, fname string, pth string) error {
	id, err := cid.Decode(pth)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(pctx, catTimeout)
	defer cancel()

	nd, err := api.Dag().Get(ctx, id)
	if err != nil {
		return err
	}

	ctx2, cancel2 := context.WithTimeout(pctx, catTimeout)
	defer cancel2()

	return dir.AddChild(ctx2, fname, nd)
}

// AddData takes a reader and adds it, optionally pins it
func AddData(pctx context.Context, api iface.CoreAPI, reader io.Reader, pin bool) (*cid.Cid, error) {
	ctx, cancel := context.WithTimeout(pctx, pinTimeout)
	defer cancel()

	pth, err := api.Unixfs().Add(ctx, files.NewReaderFile(reader))
	if err != nil {
		return nil, err
	}

	if pin {
		if err := api.Pin().Add(ctx, pth, options.Pin.Recursive(false)); err != nil {
			return nil, err
		}
	}
	id := pth.Cid()

	return &id, nil
}

// AddObject takes a reader and adds it as a DAG node, optionally pins it
func AddObject(pctx context.Context, api iface.CoreAPI, reader io.Reader, pin bool) (*cid.Cid, error) {
	ctx, cancel := context.WithTimeout(pctx, pinTimeout)
	defer cancel()

	pth, err := api.Object().Put(ctx, reader)
	if err != nil {
		return nil, err
	}

	if pin {
		if err := api.Pin().Add(ctx, pth, options.Pin.Recursive(false)); err != nil {
			return nil, err
		}
	}
	id := pth.Cid()

	return &id, nil
}

// NodeAtLink returns the node behind an ipld link
func NodeAtLink(pctx context.Context, api iface.CoreAPI, link *ipld.Link) (ipld.Node, error) {
	ctx, cancel := context.WithTimeout(pctx, catTimeout)
	defer cancel()
	return link.GetNode(ctx, api.Dag())
}

// NodeAtCid returns the node behind a cid
func NodeAtCid(pctx context.Context, api iface.CoreAPI, id cid.Cid) (ipld.Node, error) {
	ctx, cancel := context.WithTimeout(pctx, catTimeout)
	defer cancel()
	return api.Dag().Get(ctx, id)
}

// NodeAtPath returns the last node under path
func NodeAtPath(pctx context.Context, api iface.CoreAPI, pth string) (ipld.Node, error) {
	p, err := iface.ParsePath(pth)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(pctx, catTimeout)
	defer cancel()

	return api.ResolveNode(ctx, p)
}

type Node struct {
	Links []Link
	Data  string
}

type Link struct {
	Name, Hash string
	Size       uint64
}

// GetObjectAtPath returns the DAG object at the given path
func GetObjectAtPath(pctx context.Context, api iface.CoreAPI, pth string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(pctx, catTimeout)
	defer cancel()

	ipth, err := iface.ParsePath(pth)
	if err != nil {
		return nil, err
	}
	nd, err := api.Object().Get(ctx, ipth)
	if err != nil {
		return nil, err
	}

	r, err := api.Object().Data(ctx, ipth)
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	out := &Node{
		Links: make([]Link, len(nd.Links())),
		Data:  string(data),
	}

	for i, link := range nd.Links() {
		out.Links[i] = Link{
			Hash: link.Cid.String(),
			Name: link.Name,
			Size: link.Size,
		}
	}

	return json.Marshal(out)
}

// PinNode pins an ipld node
func PinNode(node *core.IpfsNode, nd ipld.Node, recursive bool) error {
	ctx, cancel := context.WithTimeout(node.Context(), pinTimeout)
	defer cancel()

	defer node.Blockstore.PinLock().Unlock()

	if err := node.Pinning.Pin(ctx, nd, recursive); err != nil {
		if strings.Contains(err.Error(), "already pinned recursively") {
			return nil
		}
		return err
	}

	return node.Pinning.Flush()
}

// UnpinNode unpins an ipld node
func UnpinNode(node *core.IpfsNode, nd ipld.Node, recursive bool) error {
	ctx, cancel := context.WithTimeout(node.Context(), pinTimeout)
	defer cancel()

	err := node.Pinning.Unpin(ctx, nd.Cid(), recursive)
	if err != nil && err != pin.ErrNotPinned {
		return err
	}

	return node.Pinning.Flush()
}

// Publish publishes data to a topic
func Publish(pctx context.Context, api iface.CoreAPI, topic string, data []byte) error {
	ctx, cancel := context.WithTimeout(pctx, publishTimeout)
	defer cancel()

	return api.PubSub().Publish(ctx, topic, data)
}

// Subscribe subscribes to a topic
func Subscribe(pctx context.Context, api iface.CoreAPI, ctx context.Context, topic string, discover bool, msgs chan iface.PubSubMessage) error {
	sub, err := api.PubSub().Subscribe(ctx, topic, options.PubSub.Discover(discover))
	if err != nil {
		return err
	}
	defer sub.Close()

	for {
		msg, err := sub.Next(pctx)
		if err == io.EOF || err == context.Canceled {
			return nil
		} else if err != nil {
			return err
		}
		msgs <- msg
	}
}

// PublishIPNS publishes a content id to ipns
func PublishIPNS(pctx context.Context, api iface.CoreAPI, id string) (iface.IpnsEntry, error) {
	opts := []options.NamePublishOption{
		options.Name.AllowOffline(true),
		options.Name.ValidTime(time.Hour * 24),
		options.Name.TTL(time.Hour),
	}

	pth, err := iface.ParsePath(id)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(pctx, ipnsTimeout)
	defer cancel()

	return api.Name().Publish(ctx, pth, opts...)
}

// ResolveIPNS resolves an ipns path to an ipfs path
func ResolveIPNS(pctx context.Context, api iface.CoreAPI, name peer.ID) (iface.Path, error) {
	key := fmt.Sprintf("/ipns/%s", name.Pretty())

	opts := []options.NameResolveOption{
		options.Name.ResolveOption(nsopts.Depth(1)),
		options.Name.ResolveOption(nsopts.DhtRecordCount(4)),
		options.Name.ResolveOption(nsopts.DhtTimeout(ipnsTimeout)),
	}

	ctx, cancel := context.WithTimeout(pctx, ipnsTimeout)
	defer cancel()

	return api.Name().Resolve(ctx, key, opts...)
}

// SwarmConnect opens a direct connection to a list of peer multi addresses
func SwarmConnect(pctx context.Context, api iface.CoreAPI, addrs []string) ([]string, error) {
	pis, err := peersWithAddresses(addrs)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(pctx, connectTimeout)
	defer cancel()

	output := make([]string, len(pis))
	for i, pi := range pis {
		output[i] = "connect " + pi.ID.Pretty()

		err := api.Swarm().Connect(ctx, pi)
		if err != nil {
			return nil, fmt.Errorf("%s failure: %s", output[i], err)
		}
		output[i] += " success"
	}

	return output, nil
}

type streamInfo struct {
	Protocol string
}

type connInfo struct {
	Addr      string         `json:"addr"`
	Peer      string         `json:"peer"`
	Latency   string         `json:"latency,omitempty"`
	Muxer     string         `json:"muxer,omitempty"`
	Direction inet.Direction `json:"direction,omitempty"`
	Streams   []streamInfo   `json:"streams,omitempty"`
}

func (ci *connInfo) Less(i, j int) bool {
	return ci.Streams[i].Protocol < ci.Streams[j].Protocol
}

func (ci *connInfo) Len() int {
	return len(ci.Streams)
}

func (ci *connInfo) Swap(i, j int) {
	ci.Streams[i], ci.Streams[j] = ci.Streams[j], ci.Streams[i]
}

type ConnInfos struct {
	Peers []connInfo
}

func (ci ConnInfos) Less(i, j int) bool {
	return ci.Peers[i].Addr < ci.Peers[j].Addr
}

func (ci ConnInfos) Len() int {
	return len(ci.Peers)
}

func (ci ConnInfos) Swap(i, j int) {
	ci.Peers[i], ci.Peers[j] = ci.Peers[j], ci.Peers[i]
}

// SwarmPeers lists the set of peers this node is connected to
func SwarmPeers(pctx context.Context, api iface.CoreAPI, verbose bool, latency bool, streams bool, direction bool) (*ConnInfos, error) {
	ctx, cancel := context.WithTimeout(pctx, connectTimeout)
	defer cancel()

	conns, err := api.Swarm().Peers(ctx)
	if err != nil {
		return nil, err
	}

	var out ConnInfos
	for _, c := range conns {
		ci := connInfo{
			Addr: c.Address().String(),
			Peer: c.ID().Pretty(),
		}

		if verbose || direction {
			// set direction
			ci.Direction = c.Direction()
		}

		if verbose || latency {
			lat, err := c.Latency()
			if err != nil {
				return nil, err
			}

			if lat == 0 {
				ci.Latency = "n/a"
			} else {
				ci.Latency = lat.String()
			}
		}
		if verbose || streams {
			strs, err := c.Streams()
			if err != nil {
				return nil, err
			}

			for _, s := range strs {
				ci.Streams = append(ci.Streams, streamInfo{Protocol: string(s)})
			}
		}
		sort.Sort(&ci)
		out.Peers = append(out.Peers, ci)
	}

	sort.Sort(&out)
	return &out, nil
}
