package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/textileio/textile-go/util"

	mh "gx/ipfs/QmPnFwZ2JXKnXgMw8CdBPxn7FWh6LLdjUjxV1fKHuJnkr8/go-multihash"
	libp2pc "gx/ipfs/QmPvyPwuCgJ7pDmrKDxRtsScJgBaM5h4EpRL2qQJsmXf4n/go-libp2p-crypto"
	"gx/ipfs/QmTRhk7cgjUf2gfQ3p2M9KPECNZEW9XUrmHcFCgog4cPgB/go-libp2p-peer"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/textileio/textile-go/crypto"
	"github.com/textileio/textile-go/ipfs"
	"github.com/textileio/textile-go/keypair"
	"github.com/textileio/textile-go/mill"
	"github.com/textileio/textile-go/pb"
	"github.com/textileio/textile-go/repo"
	"github.com/textileio/textile-go/schema/textile"
)

// ErrThreadNotFound indicates thread is not found in the loaded list
var ErrThreadNotFound = errors.New("thread not found")

// ErrThreadInviteNotFound indicates thread invite is not found
var ErrThreadInviteNotFound = errors.New("thread invite not found")

// ErrThreadLoaded indicates the thread is already loaded from the datastore
var ErrThreadLoaded = errors.New("thread is loaded")

// internalThreadKeys lists keys used by internal threads
var internalThreadKeys = []string{"avatars"}

// AddThread adds a thread with a given name and secret key
func (t *Textile) AddThread(conf pb.AddThreadConfig, sk libp2pc.PrivKey, initiator string, join bool) (*Thread, error) {
	id, err := peer.IDFromPrivateKey(sk)
	if err != nil {
		return nil, err
	}
	skb, err := sk.Bytes()
	if err != nil {
		return nil, err
	}

	var schema string
	if conf.Schema != nil {
		var sjson string

		if conf.Schema.Id != "" {
			// ensure schema id is a multi hash
			if _, err := mh.FromB58String(conf.Schema.Id); err != nil {
				return nil, err
			}
			schema = conf.Schema.Id
		} else if conf.Schema.Json != "" {
			sjson = conf.Schema.Json
		} else {
			switch conf.Schema.Preset {
			case pb.AddThreadConfig_Schema_CAMERA_ROLL:
				sjson = textile.CameraRoll
			case pb.AddThreadConfig_Schema_MEDIA:
				sjson = textile.Media
			}
		}

		if sjson != "" {
			sfile, err := t.AddFileIndex(&mill.Schema{}, AddFileConfig{
				Input: []byte(sjson),
				Media: "application/json",
			})
			if err != nil {
				return nil, err
			}
			schema = sfile.Hash
		}

		if schema != "" {
			if err := t.cafeOutbox.Add(schema, repo.CafeStoreRequest); err != nil {
				return nil, err
			}
		}
	}

	// ensure members is unique
	set := make(map[string]struct{})
	var members []string
	for _, m := range conf.Members {
		if _, ok := set[m]; !ok {
			kp, err := keypair.Parse(m)
			if err != nil {
				return nil, fmt.Errorf("error parsing member: %s", err)
			}
			if _, err := kp.Sign([]byte{0x00}); err == nil {
				// we don't want to handle account seeds, just addresses
				return nil, fmt.Errorf("member is an account seed, not address")
			}
			members = append(members, m)
		}
		set[m] = struct{}{}
	}

	threadModel := &pb.Thread{
		Id:        id.Pretty(),
		Key:       conf.Key,
		Sk:        skb,
		Name:      strings.TrimSpace(conf.Name),
		Schema:    schema,
		Initiator: initiator,
		Type:      conf.Type,
		Sharing:   conf.Sharing,
		Members:   members,
		State:     pb.Thread_Loaded,
	}
	if err := t.datastore.Threads().Add(threadModel); err != nil {
		return nil, err
	}

	thrd, err := t.loadThread(threadModel)
	if err != nil {
		return nil, err
	}

	// we join here if we're the creator
	if join {
		if _, err := thrd.joinInitial(); err != nil {
			return nil, err
		}
	}

	if thrd.Schema != nil {
		go t.cafeOutbox.Flush()
	}

	t.sendUpdate(Update{Id: thrd.Id, Key: thrd.Key, Name: thrd.Name, Type: ThreadAdded})

	log.Debugf("added a new thread %s with name %s", thrd.Id, conf.Name)

	return thrd, nil
}

// AddOrUpdateThread add or updates a thread directly, usually from a backup
func (t *Textile) AddOrUpdateThread(thrd *pb.Thread) error {
	// check if we're allowed to get an invite
	// Note: just using a dummy thread here because having these access+sharing
	// methods on Thread is very nice elsewhere.
	dummy := &Thread{
		initiator: thrd.Initiator,
		ttype:     thrd.Type,
		sharing:   thrd.Sharing,
		members:   thrd.Members,
	}
	if !dummy.shareable(t.config.Account.Address, t.config.Account.Address) {
		return ErrNotShareable
	}

	sk, err := libp2pc.UnmarshalPrivateKey(thrd.Sk)
	if err != nil {
		return err
	}

	id, err := peer.IDFromPrivateKey(sk)
	if err != nil {
		return err
	}

	nthrd := t.Thread(id.Pretty())
	if nthrd == nil {
		config := pb.AddThreadConfig{
			Key:  thrd.Key,
			Name: thrd.Name,
			Schema: &pb.AddThreadConfig_Schema{
				Id: thrd.Schema,
			},
			Type:    thrd.Type,
			Sharing: thrd.Sharing,
			Members: thrd.Members,
		}

		var err error
		nthrd, err = t.AddThread(config, sk, thrd.Initiator, false)
		if err != nil {
			return err
		}
	}

	if err := nthrd.followParents([]string{thrd.Head}); err != nil {
		return err
	}
	hash, err := mh.FromB58String(thrd.Head)
	if err != nil {
		return err
	}

	return nthrd.updateHead(hash)
}

// RemoveThread removes a thread
func (t *Textile) RemoveThread(id string) (mh.Multihash, error) {
	var thrd *Thread
	var index int
	for i, th := range t.loadedThreads {
		if th.Id == id {
			thrd = th
			index = i
			break
		}
	}
	if thrd == nil {
		return nil, ErrThreadNotFound
	}

	// notify peers
	addr, err := thrd.leave()
	if err != nil {
		return nil, err
	}

	if err := t.datastore.Threads().Delete(thrd.Id); err != nil {
		return nil, err
	}

	copy(t.loadedThreads[index:], t.loadedThreads[index+1:])
	t.loadedThreads[len(t.loadedThreads)-1] = nil
	t.loadedThreads = t.loadedThreads[:len(t.loadedThreads)-1]

	t.sendUpdate(Update{Id: thrd.Id, Key: thrd.Key, Name: thrd.Name, Type: ThreadRemoved})

	log.Infof("removed thread %s with name %s", thrd.Id, thrd.Name)

	return addr, nil
}

// Threads lists loaded threads
func (t *Textile) Threads() []*Thread {
	var threads []*Thread
loop:
	for _, i := range t.loadedThreads {
		if i == nil || i.Key == t.account.Address() {
			continue
		}
		for _, k := range internalThreadKeys {
			if i.Key == k {
				continue loop
			}
		}
		threads = append(threads, i)
	}
	return threads
}

// Thread get a thread by id from loaded threads
func (t *Textile) Thread(id string) *Thread {
	for _, thrd := range t.loadedThreads {
		if thrd.Id == id {
			return thrd
		}
	}
	return nil
}

// ThreadByKey get a thread by key from loaded threads
func (t *Textile) ThreadByKey(key string) *Thread {
	for _, thrd := range t.loadedThreads {
		if thrd.Key == key {
			return thrd
		}
	}
	return nil
}

// ThreadView returns a thread with expanded view properties
func (t *Textile) ThreadView(id string) (*pb.Thread, error) {
	thrd := t.Thread(id)
	if thrd == nil {
		return nil, ErrThreadNotFound
	}

	mod := t.datastore.Threads().Get(thrd.Id)
	if mod == nil {
		return nil, errThreadReload
	}

	// add extra view info
	mod.SchemaNode = thrd.Schema
	if mod.Head != "" {
		mod.HeadBlock = t.datastore.Blocks().Get(mod.Head)
		if mod.HeadBlock != nil {
			mod.HeadBlock.User = t.User(mod.HeadBlock.Author)
		}
	}
	mod.BlockCount = int32(t.datastore.Blocks().Count(fmt.Sprintf("threadId='%s'", thrd.Id)))
	mod.PeerCount = int32(len(thrd.Peers()) + 1)

	return mod, nil
}

// Invite gets a pending invite
func (t *Textile) Invite(invite *pb.Invite) *ThreadInviteInfo {
	if invite == nil {
		return nil
	}

	var username, avatar string
	contact := t.datastore.Contacts().Get(invite.Inviter.Id)
	if contact != nil && (invite.Inviter == nil || util.ProtoNanos(invite.Inviter.Updated) < util.ProtoNanos(contact.Updated)) {
		username = toName(contact)
		avatar = contact.Avatar
	} else if invite.Inviter != nil {
		//invite.Inviter = t.Us
		username, avatar = t.ContactDisplayInfo(invite.Contact.Id)
	}

	return &ThreadInviteInfo{
		Id:       invite.Id,
		Name:     invite.Name,
		Username: username,
		Avatar:   avatar,
		Date:     invite.Date,
	}
}

// Invites lists info on all pending invites
func (t *Textile) Invites() []ThreadInviteInfo {
	list := make([]ThreadInviteInfo, 0)

	for _, invite := range t.datastore.ThreadInvites().List() {
		info := t.Invite(&invite)
		list = append(list, *info)
	}

	return list
}

// AcceptInvite adds a new thread, and notifies the inviter of the join
func (t *Textile) AcceptInvite(inviteId string) (mh.Multihash, error) {
	invite := t.datastore.ThreadInvites().Get(inviteId)
	if invite == nil {
		return nil, ErrThreadInviteNotFound
	}

	hash, err := t.handleThreadInvite(invite.Block)
	if err != nil {
		return nil, err
	}

	if err := t.IgnoreInvite(inviteId); err != nil {
		return nil, err
	}

	return hash, nil
}

// AcceptExternalInvite attemps to download an encrypted thread key from an external invite,
// adds a new thread, and notifies the inviter of the join
func (t *Textile) AcceptExternalInvite(inviteId string, key []byte) (mh.Multihash, error) {
	ciphertext, err := ipfs.DataAtPath(t.node, fmt.Sprintf("%s", inviteId))
	if err != nil {
		return nil, err
	}

	// attempt decrypt w/ key
	plaintext, err := crypto.DecryptAES(ciphertext, key)
	if err != nil {
		return nil, ErrInvalidThreadBlock
	}
	return t.handleThreadInvite(plaintext)
}

// IgnoreInvite deletes the invite and removes the associated notification.
func (t *Textile) IgnoreInvite(inviteId string) error {
	if err := t.datastore.ThreadInvites().Delete(inviteId); err != nil {
		return err
	}
	return t.datastore.Notifications().DeleteByBlock(inviteId)
}

// handleThreadInvite uses an invite block to join a thread
func (t *Textile) handleThreadInvite(plaintext []byte) (mh.Multihash, error) {
	block := new(pb.ThreadBlock)
	if err := proto.Unmarshal(plaintext, block); err != nil {
		return nil, err
	}
	if block.Type != pb.Block_INVITE {
		return nil, ErrInvalidThreadBlock
	}
	msg := new(pb.ThreadInvite)
	if err := ptypes.UnmarshalAny(block.Payload, msg); err != nil {
		return nil, err
	}
	if msg.Thread == nil || msg.Inviter == nil {
		return nil, ErrInvalidThreadBlock
	}

	// check if we're allowed to get an invite
	// Note: just using a dummy thread here because having these access+sharing
	// methods on Thread is very nice elsewhere.
	dummy := &Thread{
		initiator: msg.Thread.Initiator,
		ttype:     msg.Thread.Type,
		sharing:   msg.Thread.Sharing,
		members:   msg.Thread.Members,
	}
	if !dummy.shareable(msg.Inviter.Address, t.config.Account.Address) {
		return nil, ErrNotShareable
	}

	sk, err := libp2pc.UnmarshalPrivateKey(msg.Thread.Sk)
	if err != nil {
		return nil, err
	}

	id, err := peer.IDFromPrivateKey(sk)
	if err != nil {
		return nil, err
	}
	if thrd := t.Thread(id.Pretty()); thrd != nil {
		// thread exists, aborting
		return nil, nil
	}

	config := pb.AddThreadConfig{
		Key:  msg.Thread.Key, // TODO: auto bump with _1,2,3 etc. if not unique
		Name: msg.Thread.Name,
		Schema: &pb.AddThreadConfig_Schema{
			Id: msg.Thread.Schema,
		},
		Type:    msg.Thread.Type,
		Sharing: msg.Thread.Sharing,
		Members: msg.Thread.Members,
	}
	thrd, err := t.AddThread(config, sk, msg.Thread.Initiator, false)
	if err != nil {
		return nil, err
	}

	if err := thrd.addOrUpdateContact(protoContactToRepo(msg.Inviter)); err != nil {
		return nil, err
	}

	// follow parents, update head
	if err := thrd.handleInviteMessage(block); err != nil {
		return nil, err
	}

	// mark any discovered peers as welcomed
	// there's no need to send a welcome because we're about to send a join message
	if err := t.datastore.ThreadPeers().WelcomeByThread(thrd.Id); err != nil {
		return nil, err
	}

	// join the thread
	author, err := peer.IDB58Decode(block.Header.Author)
	if err != nil {
		return nil, err
	}
	hash, err := thrd.join(author)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// addAccountThread adds a thread with seed representing the state of the account
func (t *Textile) addAccountThread() error {
	if t.ThreadByKey(t.config.Account.Address) != nil {
		return nil
	}
	sk, err := t.account.LibP2PPrivKey()
	if err != nil {
		return err
	}

	config := pb.AddThreadConfig{
		Key:     t.account.Address(),
		Name:    "account",
		Type:    pb.Thread_Private,
		Sharing: pb.Thread_NotShared,
	}
	if _, err := t.AddThread(config, sk, t.account.Address(), true); err != nil {
		return err
	}
	return nil
}
