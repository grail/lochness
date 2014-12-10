package lochness

import (
	"encoding/json"
	"path/filepath"

	"code.google.com/p/go-uuid/uuid"
	"github.com/coreos/go-etcd/etcd"
)

var (
	NetworkPath = "lochness/networks/"
)

type (
	// Network is a logical collection of subnets
	Network struct {
		context       *Context
		modifiedIndex uint64
		ID            string            `json:"id"`
		Metadata      map[string]string `json:"metadata"`
	}

	Networks []*Network
)

func (c *Context) NewNetwork() *Network {
	t := &Network{
		context:  c,
		ID:       uuid.New(),
		Metadata: make(map[string]string),
	}

	return t
}

func (c *Context) Network(id string) (*Network, error) {
	t := &Network{
		context: c,
		ID:      id,
	}

	err := t.Refresh()
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (t *Network) key() string {
	return filepath.Join(NetworkPath, t.ID, "metadata")
}

func (t *Network) fromResponse(resp *etcd.Response) error {
	t.modifiedIndex = resp.Node.ModifiedIndex
	return json.Unmarshal([]byte(resp.Node.Value), &t)
}

// Refresh reloads from the data store
func (t *Network) Refresh() error {
	resp, err := t.context.etcd.Get(t.key(), false, false)

	if err != nil {
		return err
	}

	if resp == nil || resp.Node == nil {
		// should this be an error??
		return nil
	}

	return t.fromResponse(resp)
}

func (t *Network) Validate() error {
	// do validation stuff...
	return nil
}

func (t *Network) Save() error {

	if err := t.Validate(); err != nil {
		return err
	}

	v, err := json.Marshal(t)

	if err != nil {
		return err
	}

	// if we changed something, don't clobber
	var resp *etcd.Response
	if t.modifiedIndex != 0 {
		resp, err = t.context.etcd.CompareAndSwap(t.key(), string(v), 0, "", t.modifiedIndex)
	} else {
		resp, err = t.context.etcd.Create(t.key(), string(v), 0)
	}
	if err != nil {
		return err
	}

	t.modifiedIndex = resp.EtcdIndex
	return nil
}

func (t *Network) subnetKey(s *Subnet) string {
	var key string
	if s != nil {
		key = s.ID
	}
	return filepath.Join(NetworkPath, t.ID, "subnets", key)
}

// when we load one, should we make sure the networkid actually matches us?

func (t *Network) AddSubnet(s *Subnet) error {
	_, err := t.context.etcd.Set(filepath.Join(t.subnetKey(s)), "", 0)
	if err != nil {
		return err
	}

	// an instance where transactions would be cool...
	s.NetworkID = t.ID
	return s.Save()
}

func (t *Network) Subnets() ([]string, error) {
	resp, err := t.context.etcd.Get(t.subnetKey(nil), true, true)
	if err != nil {
		return nil, err
	}

	var subnets []string
	for _, n := range resp.Node.Nodes {
		subnets = append(subnets, filepath.Base(n.Key))
	}

	return subnets, nil
}
