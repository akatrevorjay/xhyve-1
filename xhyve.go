// +build darwin

package main

// #cgo CFLAGS: -I${SRCDIR}/vendor/xhyve/include
// #cgo LDFLAGS: -L${SRCDIR} -lxhyve -arch x86_64 -framework Hypervisor -framework vmnet
// #include "helper.h"
import "C"

import (
	"errors"
	"strconv"

	"github.com/satori/go.uuid"
)

var (
	// ErrPCISlots is returned when an error was found parsing PCI slots.
	ErrPCISlots = errors.New("Error parsing PCI slots")
	// ErrLPCDevice is returned when an error was found parsing LPC device options.
	ErrLPCDevice = errors.New("Error parsing LPC devices")
	// ErrInvalidMemsize is returned if memorize size is invalid.
	ErrInvalidMemsize = errors.New("Invalid memory size.")
	// ErrInvalidBootParams is returne when kexec or fbsd params are invalid.
	ErrInvalidBootParams = errors.New("Boot parameters are invalid.")
	// ErrCreatingVM is returned when xhyve was unable to create the virtual machine.
	ErrCreatingVM = errors.New("Unable to create VM.")
	// ErrMaxNumVCPUExceeded is returned when the number of vcpus requested for the guest
	// exceeds the limit imposed by xhyve.
	ErrMaxNumVCPUExceeded = errors.New("Maximum number of vcpus requested is too high")
	// ErrSettingUpMemory is returned when an error was returned by xhyve when trying
	// to setup guest memory.
	ErrSettingUpMemory = errors.New("Unable to setup memory for guest vm")
)

// XHyveParams defines parameters needed by xhyve to boot up virtual machines.
type XHyveParams struct {
	// Number of CPUs to assigned to the guest vm.
	Nvcpus int
	// Memory in megabytes to assign to guest vm.
	Memory string
	// PCI Slots to attach to the guest vm.
	PCISlots []string // 2:0,virtio-net or
	// LPC devices to attach to the guest vm.
	LPCDevs []string // -l com1,stdio
	// Whether to create ACPI tables or not.
	ACPI bool
	// Universal identifier for the guest vm.
	UUID string
	// Whether to use UTC offset or localtime
	UTC bool
	// Either kexec or fbsd params. Format:
	// kexec,kernel image,initrd,"cmdline"
	// fbsd,userboot,boot volume,"kernel env"
	BootParams string
}

func setDefaults(p *XHyveParams) {
	if p.Nvcpus < 1 {
		p.Nvcpus = 1
	}

	memsize, err := strconv.Atoi(p.Memory)
	if memsize < 256 || err != nil {
		p.Memory = "256"
	}

	if len(p.PCISlots) == 0 {
		p.PCISlots = []string{
			"2:0,virtio-net",
			"0:0,hostbridge",
			"31,lpc",
		}
	}

	if len(p.LPCDevs) == 0 {
		p.LPCDevs = []string{
			"com1",
			"stdio",
		}
	}

	if p.UUID == "" {
		p.UUID = uuid.NewV4().String()
	}
}

// RunXHyve runs xhyve hypervisor with the given parameters.
func RunXHyve(p XHyveParams) error {
	setDefaults(&p)

	for _, s := range p.PCISlots {
		if err := C.pci_parse_slot(C.CString(s)); err != 0 {
			return ErrPCISlots
		}
	}

	for _, l := range p.LPCDevs {
		if err := C.lpc_device_parse(C.CString(l)); err != 0 {
			return ErrLPCDevice
		}
	}

	var memsize C.size_t
	if err := C.parse_memsize(C.CString(p.Memory), &memsize); err != 0 {
		return ErrInvalidMemsize
	}

	if err := C.firmware_parse(C.CString(p.BootParams)); err != 0 {
		return ErrInvalidBootParams
	}

	if err := C.xh_vm_create(); err != 0 {
		return ErrCreatingVM
	}

	maxVCPUs := C.num_vcpus_allowed()
	if C.int(p.Nvcpus) > maxVCPUs {
		return ErrMaxNumVCPUExceeded
	}

	if err := C.xh_vm_setup_memory(memsize, C.VM_MMAP_ALL); err != 0 {
		return ErrSettingUpMemory
	}

	if err := C.init_msr(); err != 0 {
		return ErrInitializingMSR
	}

	C.init_mem()
	C.init_inout()
	C.pci_irq_init()
	C.ioapic_init()

	C.rtc_init(C.int(0))
	C.sci_init()

	if err := C.init_pci(); err != 0 {
		return ErrInitializingPCI
	}

	if p.BVMConsole {
		C.init_bvmcons()
	}

	if p.MPTGen {
		if err := C.mptable_build(C.int(p.Nvcpus)); err != 0 {
			return ErrBuildingMPTTable
		}
	}

	if err := C.smbios_build(); err != 0 {
		return ErrBuildingSMBIOS
	}

	if p.ACPI {
		if err := C.acpi_build(C.int(p.Nvcpus)); err != 0 {
			return ErrBuildingACPI
		}
	}

	var bsp C.int
	var rip C.uint64_t
	C.vcpu_add(bsp, bsp, rip)

	C.mevent_dispatch()

	return nil
}
