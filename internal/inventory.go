package internal

import (
	"context"
	"fmt"
	"strings"
)

// Inventory is the hardware inventory read from a device's CIM classes.
//
// The types are this package's own rather than bmc-toolbox/common: an AMT
// library should not require its callers to adopt one BMC ecosystem's model.
// Mapping to common.Device, redfish, or anything else is a few lines in the
// caller.
type Inventory struct {
	Manufacturer string
	Model        string
	SerialNumber string

	Baseboard Baseboard
	BIOS      BIOS
	CPUs      []CPU
	Memory    []MemoryModule
	NICs      []NIC
	Drives    []Drive
}

// MemoryBytes is the total installed memory.
func (i Inventory) MemoryBytes() int64 {
	var total int64
	for _, m := range i.Memory {
		total += m.CapacityBytes
	}
	return total
}

// Baseboard is the system board.
type Baseboard struct {
	Manufacturer string
	Model        string
	SerialNumber string
	Version      string
}

// BIOS is the system firmware.
type BIOS struct {
	Vendor      string
	Version     string
	ReleaseDate string
}

// CPU is one processor package.
type CPU struct {
	ID              string
	Name            string
	MaxClockMHz     int
	CurrentClockMHz int
	Family          int
	Stepping        string
}

// MemoryModule is one installed DIMM.
type MemoryModule struct {
	BankLabel     string
	Manufacturer  string
	PartNumber    string
	SerialNumber  string
	CapacityBytes int64
	ClockMHz      int
}

// NIC is one host network interface.
type NIC struct {
	MACAddress string
	Name       string
}

// Drive is one storage device.
//
// AMT's CIM_MediaAccessDevice reports no model or serial number -- only an
// identifier, a generic element name and a capacity -- so those are the only
// fields offered here rather than inventing ones the device cannot fill.
type Drive struct {
	// ID is the device identifier, e.g. "MEDIA DEV 0".
	ID string
	// MaxMediaSizeKB is the capacity as reported by CIM, in kilobytes. Zero
	// when the device does not report it.
	MaxMediaSizeKB uint64
}

// Inventory reads the device's hardware inventory.
//
// Every class is read independently and a failure in one is tolerated: AMT
// populates these unevenly across platforms and firmware versions, and a
// partial inventory is materially more useful than none. Only a failure to
// read the chassis -- which carries the identifying model and serial -- is
// reported as an error.
func (c *Client) Inventory(ctx context.Context) (*Inventory, error) {
	chassis, err := c.chassis(ctx)
	if err != nil {
		return nil, err
	}

	inv := &Inventory{
		Manufacturer: chassis.Manufacturer,
		Model:        chassis.Model,
		SerialNumber: chassis.SerialNumber,
		Baseboard:    c.baseboard(ctx),
		BIOS:         c.bios(ctx),
		CPUs:         c.cpus(ctx),
		Memory:       c.memory(ctx),
		NICs:         c.nics(ctx),
		Drives:       c.drives(ctx),
	}
	return inv, nil
}

type chassisInfo struct {
	Manufacturer string
	Model        string
	SerialNumber string
}

func (c *Client) chassis(_ context.Context) (chassisInfo, error) {
	enum, err := c.Msg.CIM.Chassis.Enumerate()
	if err != nil {
		return chassisInfo{}, fmt.Errorf("iamt: enumerating chassis: %w", err)
	}
	pull, err := c.Msg.CIM.Chassis.Pull(enum.Body.EnumerateResponse.EnumerationContext)
	if err != nil {
		return chassisInfo{}, fmt.Errorf("iamt: reading chassis: %w", err)
	}
	items := pull.Body.PullResponse.PackageItems
	if len(items) == 0 {
		return chassisInfo{}, fmt.Errorf("iamt: device reported no chassis")
	}
	return chassisInfo{
		Manufacturer: strings.TrimSpace(items[0].Manufacturer),
		Model:        strings.TrimSpace(items[0].Model),
		SerialNumber: strings.TrimSpace(items[0].SerialNumber),
	}, nil
}

func (c *Client) baseboard(_ context.Context) Baseboard {
	enum, err := c.Msg.CIM.Card.Enumerate()
	if err != nil {
		return Baseboard{}
	}
	pull, err := c.Msg.CIM.Card.Pull(enum.Body.EnumerateResponse.EnumerationContext)
	if err != nil || len(pull.Body.PullResponse.CardItems) == 0 {
		return Baseboard{}
	}
	card := pull.Body.PullResponse.CardItems[0]
	return Baseboard{
		Manufacturer: strings.TrimSpace(card.Manufacturer),
		Model:        strings.TrimSpace(card.Model),
		SerialNumber: strings.TrimSpace(card.SerialNumber),
		Version:      strings.TrimSpace(card.Version),
	}
}

func (c *Client) bios(_ context.Context) BIOS {
	resp, err := c.Msg.CIM.BIOSElement.Get()
	if err != nil {
		return BIOS{}
	}
	b := resp.Body.BIOSElementGetResponse
	return BIOS{
		Vendor:      strings.TrimSpace(b.Manufacturer),
		Version:     strings.TrimSpace(b.Version),
		ReleaseDate: b.ReleaseDate.DateTime,
	}
}

func (c *Client) cpus(_ context.Context) []CPU {
	enum, err := c.Msg.CIM.Processor.Enumerate()
	if err != nil {
		return nil
	}
	pull, err := c.Msg.CIM.Processor.Pull(enum.Body.EnumerateResponse.EnumerationContext)
	if err != nil {
		return nil
	}

	out := make([]CPU, 0, len(pull.Body.PullResponse.PackageItems))
	for _, p := range pull.Body.PullResponse.PackageItems {
		out = append(out, CPU{
			ID:              strings.TrimSpace(p.DeviceID),
			Name:            strings.TrimSpace(p.ElementName),
			MaxClockMHz:     p.MaxClockSpeed,
			CurrentClockMHz: p.CurrentClockSpeed,
			Family:          p.Family,
			Stepping:        strings.TrimSpace(p.Stepping),
		})
	}
	return out
}

func (c *Client) memory(_ context.Context) []MemoryModule {
	enum, err := c.Msg.CIM.PhysicalMemory.Enumerate()
	if err != nil {
		return nil
	}
	pull, err := c.Msg.CIM.PhysicalMemory.Pull(enum.Body.EnumerateResponse.EnumerationContext)
	if err != nil {
		return nil
	}

	var out []MemoryModule
	for _, m := range pull.Body.PullResponse.MemoryItems {
		if m.Capacity == 0 {
			continue
		}
		out = append(out, MemoryModule{
			BankLabel:     strings.TrimSpace(m.BankLabel),
			Manufacturer:  strings.TrimSpace(m.Manufacturer),
			PartNumber:    strings.TrimSpace(m.PartNumber),
			SerialNumber:  strings.TrimSpace(m.SerialNumber),
			CapacityBytes: int64(m.Capacity),
			ClockMHz:      m.ConfiguredMemoryClockSpeed,
		})
	}
	return out
}

// nics reads CIM_EthernetPort.
//
// The MAC is not where CIM says it should be: AMT leaves PermanentAddress
// empty and reports the address in NetworkAddresses instead, without
// separators. Both are checked, and the result is normalised to the
// colon-separated lower-case form.
func (c *Client) nics(_ context.Context) []NIC {
	enum, err := c.Msg.CIM.EthernetPort.Enumerate()
	if err != nil {
		return nil
	}
	pull, err := c.Msg.CIM.EthernetPort.Pull(enum.Body.EnumerateResponse.EnumerationContext)
	if err != nil {
		return nil
	}

	var out []NIC
	for _, p := range pull.Body.PullResponse.EthernetPortItems {
		mac := NormalizeMAC(p.PermanentAddress)
		for _, addr := range p.NetworkAddresses {
			if mac != "" {
				break
			}
			mac = NormalizeMAC(addr)
		}
		// An unpopulated port -- a disabled wireless radio, typically --
		// reports an all-zero address. Surfacing it would put a bogus
		// interface on every device that has one, all of them identical.
		if mac == "" || isZeroMAC(mac) {
			continue
		}
		name := strings.TrimSpace(p.Description)
		if name == "" {
			name = strings.TrimSpace(p.ElementName)
		}
		out = append(out, NIC{MACAddress: mac, Name: name})
	}
	return out
}

func (c *Client) drives(_ context.Context) []Drive {
	enum, err := c.Msg.CIM.MediaAccessDevice.Enumerate()
	if err != nil {
		return nil
	}
	pull, err := c.Msg.CIM.MediaAccessDevice.Pull(enum.Body.EnumerateResponse.EnumerationContext)
	if err != nil {
		return nil
	}

	var out []Drive
	for _, d := range pull.Body.PullResponse.MediaAccessDevices {
		id := strings.TrimSpace(d.DeviceID)
		if id == "" {
			continue
		}
		out = append(out, Drive{ID: id, MaxMediaSizeKB: d.MaxMediaSize})
	}
	return out
}

// isZeroMAC reports whether a normalised MAC is all zeroes.
func isZeroMAC(mac string) bool { return mac == "00:00:00:00:00:00" }

// NormalizeMAC renders a MAC address as lower-case colon-separated octets.
// It accepts the bare 12-hex-digit form AMT reports as well as addresses that
// already carry ':' or '-' separators, and returns "" for anything else.
func NormalizeMAC(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(":", "", "-", "", ".", "").Replace(s)
	if len(s) != 12 {
		return ""
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	var b strings.Builder
	for i := 0; i < 12; i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(s[i : i+2])
	}
	return b.String()
}
