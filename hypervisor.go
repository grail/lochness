package lochness

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"code.google.com/p/go-uuid/uuid"
	"github.com/coreos/go-etcd/etcd"
)

var (
	// HypervisorPath is the path in the config store
	HypervisorPath = "lochness/hypervisors/"
)

type (
	// Hypervisor is a physical box on which guests run
	Hypervisor struct {
		context            *Context
		modifiedIndex      uint64
		ID                 string            `json:"id"`
		Metadata           map[string]string `json:"metadata"`
		IP                 net.IP            `json:"ip"`
		Netmask            net.IP            `json:"netmask"`
		Gateway            net.IP            `json:"gateway"`
		MAC                net.HardwareAddr  `json:"mac"`
		TotalResources     Resources         `json:"total_resources"`
		AvailableResources Resources         `json:"available_resources"`
		subnets            map[string]string
		guests             []string
		alive              bool
	}

	// Hypervisors is a convenience type for Hypervisor slices
	Hypervisors []*Hypervisor

	// hypervisorJSON is used to ease json marshal/unmarshal
	hypervisorJSON struct {
		ID                 string            `json:"id"`
		Metadata           map[string]string `json:"metadata"`
		IP                 net.IP            `json:"ip"`
		Netmask            net.IP            `json:"netmask"`
		Gateway            net.IP            `json:"gateway"`
		MAC                string            `json:"mac"`
		TotalResources     Resources         `json:"total_resources"`
		AvailableResources Resources         `json:"available_resources"`
	}
)

// MarshalJSON is a helper for marshalling a Hypervisor
func (h *Hypervisor) MarshalJSON() ([]byte, error) {
	data := hypervisorJSON{
		ID:                 h.ID,
		Metadata:           h.Metadata,
		IP:                 h.IP,
		Netmask:            h.Netmask,
		Gateway:            h.Gateway,
		MAC:                h.MAC.String(),
		TotalResources:     h.TotalResources,
		AvailableResources: h.AvailableResources,
	}

	return json.Marshal(data)
}

// UnmarshalJSON is a helper for unmarshalling a Hypervisor
func (h *Hypervisor) UnmarshalJSON(input []byte) error {
	data := hypervisorJSON{}

	if err := json.Unmarshal(input, &data); err != nil {
		return err
	}

	h.ID = data.ID
	h.Metadata = data.Metadata
	h.IP = data.IP
	h.Netmask = data.Netmask
	h.Gateway = data.Gateway
	h.TotalResources = data.TotalResources
	h.AvailableResources = data.AvailableResources

	if data.MAC != "" {
		a, err := net.ParseMAC(data.MAC)
		if err != nil {
			return err
		}

		h.MAC = a
	}

	return nil

}

// blankHypervisor is a helper for creating a blank Hypervisor.
func (c *Context) blankHypervisor(id string) *Hypervisor {
	h := &Hypervisor{
		context: c,
		ID:      id,
		subnets: make(map[string]string),
		guests:  make([]string, 0, 0),
	}

	if id == "" {
		h.ID = uuid.New()
	}

	return h
}

// NewHypervisor create a new blank Hypervisor.
func (c *Context) NewHypervisor() *Hypervisor {
	return c.blankHypervisor("")
}

// Hypervisor fetches a Hypervisor from the config store.
func (c *Context) Hypervisor(id string) (*Hypervisor, error) {
	h := c.blankHypervisor(id)

	err := h.Refresh()
	if err != nil {
		return nil, err
	}

	return h, nil
}

// key is a helper to generate the config store key.
func (h *Hypervisor) key() string {
	return filepath.Join(HypervisorPath, h.ID, "metadata")
}

// Refresh reloads a Hypervisor from the data store.
func (h *Hypervisor) Refresh() error {
	resp, err := h.context.etcd.Get(filepath.Join(HypervisorPath, h.ID), false, true)

	if err != nil {
		return err
	}

	for _, n := range resp.Node.Nodes {
		key := filepath.Base(n.Key)
		switch key {

		case "metadata":
			if err := json.Unmarshal([]byte(n.Value), &h); err != nil {
				return err
			}
			h.modifiedIndex = n.ModifiedIndex
		case "heartbeat":
			//if exists, then its alive
			h.alive = true

		case "subnets":
			for _, n := range n.Nodes {
				h.subnets[filepath.Base(n.Key)] = n.Value
			}
		case "guests":
			for _, n := range n.Nodes {
				h.guests = append(h.guests, filepath.Base(n.Key))
			}
		}
	}

	return nil
}

// TODO: figure out safe amount of memory to report and how to limit it (etcd?)
func memory() (uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(f)
	mem := 0
	for scanner.Scan() {
		if !strings.HasPrefix(scanner.Text(), "MemTotal:") {
			continue
		}
		vals := strings.Split(scanner.Text(), " ")
		mem, err = strconv.Atoi(vals[len(vals)-2])
		if err != nil {
			return 0, err
		}
	}
	return uint64(mem) * 80 / 100 / 1024, scanner.Err()
}

// TODO: figure out real zfs disk size
func disk() (uint64, error) {
	stat := &syscall.Statfs_t{}
	err := syscall.Statfs("/", stat)
	return uint64(stat.Bsize) * stat.Bavail * 80 / 100 / 1024 / 1024, err
}

// cpu gets number of CPU's.
func cpu() (uint32, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "processor") {
			count++
		}
	}
	return uint32(count - 1), scanner.Err()
}

// verifyOnHV verifies that it is being ran on hypervisor with same hostname as id.
func (h *Hypervisor) verifyOnHV() error {
	hostname := os.Getenv("TEST_HOSTNAME")
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != err {
			return err
		}
	}
	if hostname != h.ID {
		return errors.New("Hypervisor ID does not match hostname")
	}
	return nil
}

// calcGuestsUsage calculates gutes usage
func (h *Hypervisor) calcGuestsUsage() (Resources, error) {
	usage := Resources{}
	for _, guestID := range h.Guests() {
		guest, err := h.context.Guest(guestID)
		if err != nil {
			return Resources{}, err
		}

		// cache?
		flavor, err := h.context.Flavor(guest.FlavorID)
		if err != nil {
			return Resources{}, err
		}
		usage.Memory += flavor.Memory
		usage.Disk += flavor.Disk
	}
	return usage, nil
}

// UpdateResources syncs Hypervisor resource usage to the data store. It should only be ran on
// the actual hypervisor.
func (h *Hypervisor) UpdateResources() error {
	if err := h.verifyOnHV(); err != nil {
		return err
	}

	m, err := memory()
	if err != nil {
		return err
	}
	d, err := disk()
	if err != nil {
		return err
	}
	c, err := cpu()
	if err != nil {
		return err
	}

	h.TotalResources = Resources{Memory: m, Disk: d, CPU: c}

	usage, err := h.calcGuestsUsage()
	if err != nil {
		return err
	}

	h.AvailableResources = Resources{
		Memory: h.TotalResources.Memory - usage.Memory,
		Disk:   h.TotalResources.Disk - usage.Disk,
		CPU:    h.TotalResources.CPU - usage.CPU,
	}

	return h.Save()
}

// Validate ensures a Hypervisor has reasonable data. It currently does nothing.
func (h *Hypervisor) Validate() error {
	// do validation stuff...
	return nil
}

// Save persists a FWGroup.  It will call Validate.
func (h *Hypervisor) Save() error {

	if err := h.Validate(); err != nil {
		return err
	}

	v, err := json.Marshal(h)

	if err != nil {
		return err
	}

	// if we changed something, don't clobber
	var resp *etcd.Response
	if h.modifiedIndex != 0 {
		resp, err = h.context.etcd.CompareAndSwap(h.key(), string(v), 0, "", h.modifiedIndex)
	} else {
		resp, err = h.context.etcd.Create(h.key(), string(v), 0)
	}
	if err != nil {
		return err
	}

	h.modifiedIndex = resp.EtcdIndex

	return nil
}

// the many side of many:one relationships is done with nested keys

func (h *Hypervisor) subnetKey(s *Subnet) string {
	var key string
	if s != nil {
		key = s.ID
	}
	return filepath.Join(HypervisorPath, h.ID, "subnets", key)
}

// AddSubnet adds a subnet to a Hypervisor.
func (h *Hypervisor) AddSubnet(s *Subnet, bridge string) error {
	_, err := h.context.etcd.Set(filepath.Join(h.subnetKey(s)), bridge, 0)
	if err == nil {
		h.subnets[s.ID] = bridge
	}
	return err
}

// Subnets returns the subnet/bridge mappings for a Hypervisor.
func (h *Hypervisor) Subnets() map[string]string {
	return h.subnets
}

// heartbeatKey is a helper for generating a key for config store.
func (h *Hypervisor) heartbeatKey() string {
	return filepath.Join(HypervisorPath, h.ID, "heartbeat")
}

// Heartbeat announces the avilibility of a hypervisor.  In general, this is useful for
// service announcement/discovery. Should be ran from the hypervisor, or something monitoring it.
func (h *Hypervisor) Heartbeat(ttl time.Duration) error {
	if err := h.verifyOnHV(); err != nil {
		return err
	}

	v := time.Now().String()
	_, err := h.context.etcd.Set(h.heartbeatKey(), v, uint64(ttl.Seconds()))
	return err
}

// IsAlive returns true if the heartbeat is present.
func (h *Hypervisor) IsAlive() bool {
	return h.alive
}

// guestKey for generating a key for config store.
func (h *Hypervisor) guestKey(g *Guest) string {
	var key string
	if g != nil {
		key = g.ID
	}
	return filepath.Join(HypervisorPath, h.ID, "guests", key)
}

// AddGuest adds a Guest to the Hypervisor. It reserves an IPaddress for the Guest.
/// It also updates the Guest.
func (h *Hypervisor) AddGuest(g *Guest) error {

	// make sure we have subnet guest wants.  we should have this figured out
	// when we selected this hypervisor, so this is sort of silly to do again
	// we need to rethink how we do this

	n, err := h.context.Network(g.NetworkID)
	if err != nil {
		return err
	}
	var s *Subnet
	var bridge string
LOOP:
	for _, k := range n.Subnets() {

		for id, br := range h.subnets {
			if id == k {
				subnet, err := h.context.Subnet(id)
				if err != nil {
					return err
				}
				avail := subnet.AvailibleAddresses()
				if len(avail) > 0 {
					s = subnet
					bridge = br
					break LOOP
				}
			}
		}
	}

	if s == nil {
		return errors.New("no suitable subnet found")
	}

	ip, err := s.ReserveAddress(g.ID)

	if err != nil {
		return err
	}

	// an instance where transactions would be cool...
	g.HypervisorID = h.ID
	g.IP = ip
	g.SubnetID = s.ID
	g.Bridge = bridge

	_, err = h.context.etcd.Set(filepath.Join(h.guestKey(g)), g.ID, 0)

	if err != nil {
		return err
	}

	return g.Save()
}

// Guests returns a slice of Guests assigned to the Hypervisor.
func (h *Hypervisor) Guests() []string {
	return h.guests
}

// ForEachHypervisor will run f on each Hypervisor. It will stop iteration if f returns an error.
func (c *Context) ForEachHypervisor(f func(*Hypervisor) error) error {
	// should we condense this to a single etcd call?
	// We would need to rework how we "load" hypervisor a bit
	resp, err := c.etcd.Get(HypervisorPath, false, false)
	if err != nil {
		return err
	}
	for _, n := range resp.Node.Nodes {
		h, err := c.Hypervisor(filepath.Base(n.Key))
		if err != nil {
			return err
		}

		if err := f(h); err != nil {
			return err
		}
	}
	return nil
}
