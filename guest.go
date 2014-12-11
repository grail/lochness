package lochness

import (
	"encoding/json"
	"net"
	"path/filepath"

	"code.google.com/p/go-uuid/uuid"
	"github.com/coreos/go-etcd/etcd"
)

var (
	GuestPath = "lochness/guests/"
)

type (
	// Guest is a virtual machine
	Guest struct {
		context       *Context
		modifiedIndex uint64
		ID            string            `json:"id"`
		Metadata      map[string]string `json:"metadata"`
		Type          string            `json:"type"`       // type of guest. currently just kvm
		FlavorID      string            `json:"flavor"`     // resource flavor
		HypervisorID  string            `json:"hypervisor"` // hypervisor. may be blank if not assigned yet
		NetworkID     string            `json:"network"`
		SubnetID      string            `json:"subnet"`
		FWGroupID     string            `json:"fwgroup"`
		MAC           net.HardwareAddr  `json:"mac"`
		IP            net.IP            `json:"ip"`
	}

	Guests []*Guest

	guestJSON struct {
		ID           string            `json:"id"`
		Metadata     map[string]string `json:"metadata"`
		Type         string            `json:"type"`       // type of guest. currently just kvm
		FlavorID     string            `json:"flavor"`     // resource flavor
		HypervisorID string            `json:"hypervisor"` // hypervisor. may be blank if not assigned yet
		NetworkID    string            `json:"network"`
		SubnetID     string            `json:"subnet"`
		FWGroupID    string            `json:"fwgroup"`
		MAC          string            `json:"mac"`
		IP           net.IP            `json:"ip"`
	}
)

func (t *Guest) MarshalJSON() ([]byte, error) {
	data := guestJSON{
		ID:           t.ID,
		Metadata:     t.Metadata,
		Type:         t.Type,
		FlavorID:     t.FlavorID,
		NetworkID:    t.NetworkID,
		SubnetID:     t.SubnetID,
		FWGroupID:    t.FWGroupID,
		HypervisorID: t.HypervisorID,
		IP:           t.IP,
		MAC:          t.MAC.String(),
	}

	return json.Marshal(data)
}

func (t *Guest) UnmarshalJSON(input []byte) error {
	data := guestJSON{}

	if err := json.Unmarshal(input, &data); err != nil {
		return err
	}

	t.ID = data.ID
	t.Metadata = data.Metadata
	t.Type = data.Type
	t.FlavorID = data.FlavorID
	t.NetworkID = data.NetworkID
	t.SubnetID = data.SubnetID
	t.FWGroupID = data.FWGroupID
	t.HypervisorID = data.HypervisorID
	t.IP = data.IP

	a, err := net.ParseMAC(data.MAC)
	if err != nil {
		return err
	}

	t.MAC = a
	return nil

}

func (c *Context) NewGuest() *Guest {
	t := &Guest{
		context:  c,
		ID:       uuid.New(),
		Metadata: make(map[string]string),
	}

	return t
}

func (c *Context) Guest(id string) (*Guest, error) {
	t := &Guest{
		context: c,
		ID:      id,
	}

	err := t.Refresh()
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (t *Guest) key() string {
	return filepath.Join(GuestPath, t.ID, "metadata")
}

func (t *Guest) fromResponse(resp *etcd.Response) error {
	t.modifiedIndex = resp.Node.ModifiedIndex
	return json.Unmarshal([]byte(resp.Node.Value), &t)
}

// Refresh reloads from the data store
func (t *Guest) Refresh() error {
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

func (t *Guest) Validate() error {
	// do validation stuff...
	return nil
}

func (t *Guest) Save() error {

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
